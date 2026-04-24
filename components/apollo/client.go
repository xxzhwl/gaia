// Package apollo 对携程 agollo v4 做轻量封装，提供 Apollo 配置中心能力。
//
// 典型用法：
//
//	cli, err := apollo.NewClient(apollo.Config{
//	    AppID:       "gaia-server",
//	    Cluster:     "default",
//	    MetaAddr:    "http://127.0.0.1:8080",
//	    Namespaces:  []string{"application"},
//	    Secret:      "xxxx",
//	})
//	val, ok, err := cli.GetConfig("Server.Port")
//	cli.AddChangeListener(func(ns string, changes map[string]*ConfigChange) { ... })
//
// Apollo 按 AppID + Cluster + Namespace 三级定位一组配置；一个客户端可以订阅多个
// namespace。本封装默认操作第一个 namespace（一般就是 application）。
//
// @author wanlizhan
// @created 2026-04-24
package apollo

import (
	"fmt"
	"strings"
	"sync"

	"github.com/apolloconfig/agollo/v4"
	agolloConfig "github.com/apolloconfig/agollo/v4/env/config"
	"github.com/apolloconfig/agollo/v4/storage"

	"github.com/xxzhwl/gaia/dic"
)

// Config Apollo 客户端配置
type Config struct {
	// AppID 应用 ID；必填
	AppID string
	// Cluster 集群名，空则使用 "default"
	Cluster string
	// MetaAddr Apollo Meta 服务地址，支持多个用逗号分隔；必填
	// 例如 "http://apollo-meta-dev:8080"
	MetaAddr string
	// Namespaces 订阅的 namespace 列表，空则使用 []string{"application"}
	Namespaces []string
	// Secret 访问密钥；Apollo 开启访问秘钥校验时必填
	Secret string
	// IsBackupConfig 是否启用本地磁盘缓存；默认 true
	IsBackupConfig *bool
	// BackupConfigPath 本地缓存目录；空走 SDK 默认
	BackupConfigPath string
}

// Client Apollo 客户端
type Client struct {
	cfg    Config
	cli    agollo.Client
	mu     sync.RWMutex
	closed bool
}

// ConfigChange 配置变化事件（转发 agollo 的 ConfigChange）
type ConfigChange = storage.ConfigChange

// ChangeEvent Namespace 级别的配置变化事件
type ChangeEvent = storage.ChangeEvent

// ChangeListener 监听器类型
type ChangeListener func(namespace string, changes map[string]*ConfigChange)

// NewClient 创建 Apollo 配置客户端
func NewClient(cfg Config) (*Client, error) {
	if cfg.AppID == "" {
		return nil, fmt.Errorf("AppID 必填")
	}
	if cfg.MetaAddr == "" {
		return nil, fmt.Errorf("MetaAddr 必填")
	}
	if cfg.Cluster == "" {
		cfg.Cluster = "default"
	}
	if len(cfg.Namespaces) == 0 {
		cfg.Namespaces = []string{"application"}
	}

	backup := true
	if cfg.IsBackupConfig != nil {
		backup = *cfg.IsBackupConfig
	}

	ac := &agolloConfig.AppConfig{
		AppID:            cfg.AppID,
		Cluster:          cfg.Cluster,
		IP:               cfg.MetaAddr,
		NamespaceName:    strings.Join(cfg.Namespaces, ","),
		IsBackupConfig:   backup,
		BackupConfigPath: cfg.BackupConfigPath,
		Secret:           cfg.Secret,
	}

	cli, err := agollo.StartWithConfig(func() (*agolloConfig.AppConfig, error) {
		return ac, nil
	})
	if err != nil {
		return nil, fmt.Errorf("启动 Apollo 客户端失败: %w", err)
	}

	return &Client{cfg: cfg, cli: cli}, nil
}

// GetCli 返回底层 agollo client
func (c *Client) GetCli() agollo.Client {
	return c.cli
}

// DefaultNamespace 默认操作的 namespace
func (c *Client) DefaultNamespace() string {
	return c.cfg.Namespaces[0]
}

// GetString 读取指定 namespace 下的 key（string 类型）
func (c *Client) GetString(namespace, key string) (string, bool) {
	cache := c.cli.GetConfigCache(namespace)
	if cache == nil {
		return "", false
	}
	v, err := cache.Get(key)
	if err != nil || v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// GetConfig 按 key 读取配置；自动在所有已订阅 namespace 里找
//
//   - 先在 DefaultNamespace 查；命中直接返回
//   - 否则逐个 namespace 兜底查找
//   - 支持 dotted path（例如 `Server.Port`）——此时会将 namespace 的所有 key 装成 map
//     再按 path 取值（适合 yaml/json 整体托管在单 key 的场景）
func (c *Client) GetConfig(key string) (any, bool, error) {
	for _, ns := range c.cfg.Namespaces {
		if v, ok := c.getFromNamespace(ns, key); ok {
			return v, true, nil
		}
	}
	return nil, false, nil
}

// getFromNamespace 在单个 namespace 中按 key 取值
func (c *Client) getFromNamespace(ns, key string) (any, bool) {
	cache := c.cli.GetConfigCache(ns)
	if cache == nil {
		return nil, false
	}

	// 1. 直接 key 命中
	if v, err := cache.Get(key); err == nil && v != nil {
		return v, true
	}

	// 2. 若 key 包含 `.` —— 尝试从完整 map 按路径取
	if strings.Contains(key, ".") {
		m := c.DumpNamespace(ns)
		if val, err := dic.GetValueByMapPath(m, key); err == nil && val != nil {
			return val, true
		}
	}
	return nil, false
}

// DumpNamespace 把一个 namespace 的所有 key-value 导出为 map
func (c *Client) DumpNamespace(ns string) map[string]any {
	m := map[string]any{}
	cache := c.cli.GetConfigCache(ns)
	if cache == nil {
		return m
	}
	cache.Range(func(key, value any) bool {
		if k, ok := key.(string); ok {
			m[k] = value
		}
		return true
	})
	return m
}

// AddChangeListener 注册配置变化监听器
func (c *Client) AddChangeListener(listener ChangeListener) {
	c.cli.AddChangeListener(&apolloListener{fn: listener})
}

// Close 停止客户端并释放资源
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.cli.Close()
	return nil
}

// ================================ 内部适配 ================================

// apolloListener 适配 agollo 的 ChangeListener 接口
type apolloListener struct {
	fn ChangeListener
}

func (l *apolloListener) OnChange(event *ChangeEvent) {
	if l.fn != nil && event != nil {
		l.fn(event.Namespace, event.Changes)
	}
}

// OnNewestChange 订阅最新快照变更（本封装不关心，保持空实现）
func (l *apolloListener) OnNewestChange(event *storage.FullChangeEvent) {}
