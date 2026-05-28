// Package consul 对 hashicorp/consul Go SDK 做轻量封装，提供配置中心能力。
//
// 典型用法：
//
//	cli, err := consul.NewClient(consul.Config{Address: "127.0.0.1:8500"})
//	m, _ := cli.GetConfigs("gaia/app")
//	cli.Put("gaia/app/Server.Port", 8080)
//	cli.WatchKey("gaia/app/Server.Port", func(v any) { ... })
//
// @author wanlizhan
// @created 2024
// @updated 2026-05-28  补齐 Config 结构、超时、Token、Put/Delete/Watch/Close
package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/xxzhwl/gaia"
)

// Config Consul 客户端配置
type Config struct {
	// Address Consul Agent 地址，必填，如 "127.0.0.1:8500"
	Address string
	// Scheme http / https；默认 http
	Scheme string
	// Token ACL Token，开启 ACL 时必填
	Token string
	// Datacenter 数据中心名；空则使用 agent 默认
	Datacenter string
	// WaitTime KV blocking query 的最长等待时间；0 默认 5 分钟
	WaitTime time.Duration
}

// Client Consul KV 客户端
type Client struct {
	cfg      Config
	c        *api.Client
	watchers map[string]context.CancelFunc
	mu       sync.Mutex
}

// NewClient 创建客户端（兼容旧签名：直接传 endPoint）
func NewClient(endPoint string) (*Client, error) {
	return NewClientWithConfig(Config{Address: endPoint})
}

// NewClientWithConfig 使用完整配置创建客户端
func NewClientWithConfig(cfg Config) (*Client, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("Address 必填")
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}

	apiCfg := api.DefaultConfig()
	apiCfg.Address = cfg.Address
	apiCfg.Scheme = cfg.Scheme
	apiCfg.Token = cfg.Token
	apiCfg.Datacenter = cfg.Datacenter
	if cfg.WaitTime > 0 {
		apiCfg.WaitTime = cfg.WaitTime
	}

	cli, err := api.NewClient(apiCfg)
	if err != nil {
		return nil, fmt.Errorf("创建 consul 客户端失败: %w", err)
	}
	return &Client{cfg: cfg, c: cli, watchers: map[string]context.CancelFunc{}}, nil
}

// NewClientWithSchema 从 gaia 配置中读取
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClientWithConfig(Config{
		Address:    gaia.GetSafeConfString(schema + ".Address"),
		Scheme:     gaia.GetSafeConfString(schema + ".Scheme"),
		Token:      gaia.GetSafeConfString(schema + ".Token"),
		Datacenter: gaia.GetSafeConfString(schema + ".Datacenter"),
		WaitTime:   time.Duration(gaia.GetSafeConfInt64(schema+".WaitTimeMs")) * time.Millisecond,
	})
}

// GetCli 返回底层 SDK client
func (c *Client) GetCli() *api.Client {
	return c.c
}

// GetConfigs 读取一个 key（默认按 JSON 解析为 map[string]any，与历史行为兼容）
func (c *Client) GetConfigs(path string) (map[string]any, error) {
	get, _, err := c.c.KV().Get(path, nil)
	if err != nil {
		return nil, err
	}
	res := map[string]any{}
	if get == nil {
		return res, nil
	}
	if len(get.Value) == 0 {
		return res, nil
	}
	if err := json.Unmarshal(get.Value, &res); err != nil {
		return nil, fmt.Errorf("consul value 不是合法 JSON: %w", err)
	}
	return res, nil
}

// GetRaw 读取一个 key 的原始字节
func (c *Client) GetRaw(path string) ([]byte, bool, error) {
	pair, _, err := c.c.KV().Get(path, nil)
	if err != nil {
		return nil, false, err
	}
	if pair == nil {
		return nil, false, nil
	}
	return pair.Value, true, nil
}

// ListByPrefix 列出 prefix 下所有 key（value 优先按 JSON 解码，失败则原样字符串）
func (c *Client) ListByPrefix(prefix string) (map[string]any, error) {
	pairs, _, err := c.c.KV().List(prefix, nil)
	if err != nil {
		return nil, err
	}
	res := make(map[string]any, len(pairs))
	for _, p := range pairs {
		k := strings.TrimPrefix(p.Key, prefix)
		k = strings.TrimPrefix(k, "/")
		var v any
		if err := json.Unmarshal(p.Value, &v); err != nil {
			v = string(p.Value)
		}
		res[k] = v
	}
	return res, nil
}

// Put 写入 key；value 会被 JSON 编码后写入
func (c *Client) Put(key string, value any) error {
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
	_, err := c.c.KV().Put(&api.KVPair{Key: key, Value: bytes}, nil)
	return err
}

// Delete 删除 key
func (c *Client) Delete(key string) error {
	_, err := c.c.KV().Delete(key, nil)
	return err
}

// WatchKey 通过 blocking query 监听单个 key 变化（callback 收到新值；删除时为 nil）
//
// 实现方式：使用 modifyIndex + WaitIndex 阻塞查询；ctx 取消时停止监听。
func (c *Client) WatchKey(key string, callback func(any)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	c.mu.Lock()
	if _, exists := c.watchers[key]; exists {
		c.mu.Unlock()
		return fmt.Errorf("watcher for key '%s' already exists", key)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.watchers[key] = cancel
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.watchers, key)
			c.mu.Unlock()
		}()

		var lastIndex uint64
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			pair, meta, err := c.c.KV().Get(key, (&api.QueryOptions{WaitIndex: lastIndex}).WithContext(ctx))
			if err != nil {
				// 网络错误睡 1s 重试，避免 hot loop
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				continue
			}
			if meta == nil {
				continue
			}
			if meta.LastIndex == lastIndex {
				// 没有变化，直接进入下一轮 blocking query
				continue
			}
			lastIndex = meta.LastIndex

			if pair == nil {
				callback(nil)
				continue
			}
			var v any
			if err := json.Unmarshal(pair.Value, &v); err != nil {
				v = string(pair.Value)
			}
			callback(v)
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

// Close 停止所有 watcher（consul SDK client 本身无需关闭）
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cancel := range c.watchers {
		cancel()
	}
	c.watchers = map[string]context.CancelFunc{}
	return nil
}