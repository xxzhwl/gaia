// Package etcd 对 etcd 官方 Go SDK（clientv3）做轻量封装，提供配置中心能力。
//
// 典型用法：
//
//	cli, err := etcd.NewClient(etcd.Config{
//	    Endpoints: []string{"127.0.0.1:2379"},
//	    Prefix:    "/gaia/app",
//	})
//	val, ok, err := cli.GetConfig("Server.Port")
//	cli.WatchKey("Server.Port", func(val any) { ... })
//
// 存储约定：
//   - Prefix + "/" + key 对应一个 etcd key
//   - value 为 JSON 字符串，使用方写入时应保持 JSON 格式（便于跨语言消费）
//   - 如果 value 不是合法 JSON，会原样作为 string 返回
//
// 特点：etcd 原生支持 Watch，响应实时；上层 Provider 不需要再做轮询。
//
// @author wanlizhan
// @created 2026-04-24
package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/xxzhwl/gaia"
)

// Config etcd 客户端配置
type Config struct {
	// Endpoints etcd 节点列表，形如 "host:port"；必填
	Endpoints []string
	// Prefix 配置 key 的统一前缀（末尾无需带 `/`），必填
	Prefix string
	// Username/Password 开启 RBAC 时必填
	Username string
	Password string
	// DialTimeout 建连超时；0 则默认 5 秒
	DialTimeout time.Duration
	// RequestTimeout 单次请求超时；0 则默认 3 秒
	RequestTimeout time.Duration
}

// Client etcd 客户端
type Client struct {
	cfg      Config
	cli      *clientv3.Client
	watchers map[string]context.CancelFunc
	mu       sync.Mutex
}

// NewClient 创建 etcd 客户端
func NewClient(cfg Config) (*Client, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("Endpoints 必填")
	}
	if cfg.Prefix == "" {
		return nil, fmt.Errorf("Prefix 必填")
	}
	cfg.Prefix = strings.TrimRight(cfg.Prefix, "/")
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 3 * time.Second
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
		Username:    cfg.Username,
		Password:    cfg.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 etcd 客户端失败: %w", err)
	}

	return &Client{
		cfg:      cfg,
		cli:      cli,
		watchers: map[string]context.CancelFunc{},
	}, nil
}

// NewClientWithSchema 从 gaia 配置中读取
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClient(Config{
		Endpoints:      gaia.GetSafeConfStringSliceFromString(schema + ".Endpoints"),
		Prefix:         gaia.GetSafeConfString(schema + ".Prefix"),
		Username:       gaia.GetSafeConfString(schema + ".Username"),
		Password:       gaia.GetSafeConfString(schema + ".Password"),
		DialTimeout:    time.Duration(gaia.GetSafeConfInt64(schema + ".DialTimeoutMs")) * time.Millisecond,
		RequestTimeout: time.Duration(gaia.GetSafeConfInt64(schema + ".RequestTimeoutMs")) * time.Millisecond,
	})
}

// NewFrameworkClient 使用 RemoteConfig.Etcd 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("RemoteConfig.Etcd")
}

// GetCli 返回底层 etcd client
func (c *Client) GetCli() *clientv3.Client {
	return c.cli
}

// fullKey 拼接完整 key
func (c *Client) fullKey(key string) string {
	if key == "" {
		return c.cfg.Prefix
	}
	return c.cfg.Prefix + "/" + key
}

// GetConfig 读取 key 的值；value 优先按 JSON 解析，不是 JSON 则原样字符串返回
//
//	value   : JSON 反解后的任意类型，或原始 string
//	existed : key 是否存在
//	err     : 网络/权限错误
func (c *Client) GetConfig(key string) (any, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.RequestTimeout)
	defer cancel()

	resp, err := c.cli.Get(ctx, c.fullKey(key))
	if err != nil {
		return nil, false, fmt.Errorf("etcd get 失败: %w", err)
	}
	if len(resp.Kvs) == 0 {
		return nil, false, nil
	}
	raw := resp.Kvs[0].Value
	return decodeValue(raw), true, nil
}

// Put 写入配置；value 会被 JSON 编码后写入 etcd
func (c *Client) Put(key string, value any) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.RequestTimeout)
	defer cancel()

	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("value 编码失败: %w", err)
		}
		bytes = b
	}
	_, err := c.cli.Put(ctx, c.fullKey(key), string(bytes))
	if err != nil {
		return fmt.Errorf("etcd put 失败: %w", err)
	}
	return nil
}

// Delete 删除 key
func (c *Client) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.RequestTimeout)
	defer cancel()

	_, err := c.cli.Delete(ctx, c.fullKey(key))
	if err != nil {
		return fmt.Errorf("etcd delete 失败: %w", err)
	}
	return nil
}

// ListByPrefix 列出 prefix 下所有 key
func (c *Client) ListByPrefix(prefix string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.RequestTimeout)
	defer cancel()

	full := c.fullKey(prefix)
	resp, err := c.cli.Get(ctx, full, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("etcd list 失败: %w", err)
	}
	result := map[string]any{}
	for _, kv := range resp.Kvs {
		// 去掉前缀
		k := strings.TrimPrefix(string(kv.Key), c.cfg.Prefix+"/")
		result[k] = decodeValue(kv.Value)
	}
	return result, nil
}

// WatchKey 监听单个 key 的变化
// callback 接受新值（nil 表示 key 被删除）
func (c *Client) WatchKey(key string, callback func(any)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.watchers[key]; exists {
		return fmt.Errorf("watcher for key '%s' already exists", key)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.watchers[key] = cancel

	go func() {
		ch := c.cli.Watch(ctx, c.fullKey(key))
		for resp := range ch {
			for _, ev := range resp.Events {
				switch ev.Type {
				case clientv3.EventTypePut:
					callback(decodeValue(ev.Kv.Value))
				case clientv3.EventTypeDelete:
					callback(nil)
				}
			}
		}
	}()
	return nil
}

// StopWatch 停止单个 key 的监听
func (c *Client) StopWatch(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel, ok := c.watchers[key]; ok {
		cancel()
		delete(c.watchers, key)
	}
}

// Close 关闭客户端
func (c *Client) Close() error {
	c.mu.Lock()
	for _, cancel := range c.watchers {
		cancel()
	}
	c.watchers = map[string]context.CancelFunc{}
	c.mu.Unlock()
	return c.cli.Close()
}

// ================================ 工具函数 ================================

// decodeValue 优先尝试 JSON 解码，失败则原样返回字符串
func decodeValue(raw []byte) any {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}
	return string(raw)
}
