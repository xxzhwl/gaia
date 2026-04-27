// Package nacos 对 Nacos 官方 Go SDK 做轻量封装，提供配置中心能力。
//
// 典型用法：
//
//	cli, err := nacos.NewClient(nacos.Config{
//	    ServerAddrs: []string{"127.0.0.1:8848"},
//	    Namespace:   "public",
//	    Group:       "DEFAULT_GROUP",
//	    DataId:      "app.yaml",
//	})
//	val, ok, err := cli.GetConfig("Server.Port")
//	_ = cli.WatchConfig(func(content string) { ... })
//
// Nacos 的配置粒度是 (namespace, group, dataId) 三元组，对应一个完整的配置文件内容。
// 本封装将其解析为 map[string]any，使用方可以按 key-path（如 `Server.Port`）读取。
//
// @author wanlizhan
// @created 2026-04-24
package nacos

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
	"gopkg.in/yaml.v3"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/dic"
)

// Config Nacos 客户端配置
type Config struct {
	// ServerAddrs Nacos server 地址列表，形如 "host:port"；必填
	ServerAddrs []string
	// Namespace 命名空间 ID（非中文名），可空
	Namespace string
	// Group 配置分组，空则使用 "DEFAULT_GROUP"
	Group string
	// DataId 配置 ID，即一个完整配置文件的名字，必填
	DataId string
	// Username/Password Nacos 开启鉴权时必填
	Username string
	Password string
	// TimeoutMs 请求超时（毫秒），默认 5000
	TimeoutMs uint64
	// LogDir / CacheDir SDK 本地落盘路径，空则使用默认
	LogDir   string
	CacheDir string
	// Format 配置内容格式：yaml | json；默认按 DataId 后缀判定，兜底 yaml
	Format string
}

// Client Nacos 客户端
type Client struct {
	cfg    Config
	cli    config_client.IConfigClient
	mu     sync.RWMutex
	cached map[string]any // 最近一次成功解析的完整配置树
}

// NewClient 创建 Nacos 配置客户端
func NewClient(cfg Config) (*Client, error) {
	if len(cfg.ServerAddrs) == 0 {
		return nil, fmt.Errorf("ServerAddrs 必填")
	}
	if cfg.DataId == "" {
		return nil, fmt.Errorf("DataId 必填")
	}
	if cfg.Group == "" {
		cfg.Group = "DEFAULT_GROUP"
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = 5000
	}
	if cfg.Format == "" {
		switch {
		case strings.HasSuffix(cfg.DataId, ".json"):
			cfg.Format = "json"
		case strings.HasSuffix(cfg.DataId, ".yml"), strings.HasSuffix(cfg.DataId, ".yaml"):
			cfg.Format = "yaml"
		default:
			cfg.Format = "yaml"
		}
	}

	serverConfigs := make([]constant.ServerConfig, 0, len(cfg.ServerAddrs))
	for _, addr := range cfg.ServerAddrs {
		host, port, err := splitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("解析 ServerAddr[%s] 失败: %w", addr, err)
		}
		serverConfigs = append(serverConfigs, *constant.NewServerConfig(host, port))
	}

	clientConfig := *constant.NewClientConfig(
		constant.WithNamespaceId(cfg.Namespace),
		constant.WithUsername(cfg.Username),
		constant.WithPassword(cfg.Password),
		constant.WithTimeoutMs(cfg.TimeoutMs),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir(cfg.LogDir),
		constant.WithCacheDir(cfg.CacheDir),
		constant.WithLogLevel("warn"),
	)

	cli, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &clientConfig,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 配置客户端失败: %w", err)
	}

	return &Client{cfg: cfg, cli: cli}, nil
}

// NewClientWithSchema 从 gaia 配置中读取
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClient(Config{
		ServerAddrs: gaia.GetSafeConfStringSliceFromString(schema + ".ServerAddrs"),
		Namespace:   gaia.GetSafeConfString(schema + ".Namespace"),
		Group:       gaia.GetSafeConfString(schema + ".Group"),
		DataId:      gaia.GetSafeConfString(schema + ".DataId"),
		Username:    gaia.GetSafeConfString(schema + ".Username"),
		Password:    gaia.GetSafeConfString(schema + ".Password"),
		TimeoutMs:   uint64(gaia.GetSafeConfInt64(schema + ".TimeoutMs")),
		Format:      gaia.GetSafeConfString(schema + ".Format"),
		LogDir:      gaia.GetSafeConfString(schema + ".LogDir"),
		CacheDir:    gaia.GetSafeConfString(schema + ".CacheDir"),
	})
}

// NewFrameworkClient 使用 RemoteConfig.Nacos 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("RemoteConfig.Nacos")
}

// GetCli 返回底层 SDK client（需要调用高级能力时使用）
func (c *Client) GetCli() config_client.IConfigClient {
	return c.cli
}

// PullRaw 拉取 DataId 当前的完整原始字符串内容
func (c *Client) PullRaw() (string, error) {
	return c.cli.GetConfig(vo.ConfigParam{
		DataId: c.cfg.DataId,
		Group:  c.cfg.Group,
	})
}

// PullMap 拉取 DataId 并解析为 map[string]any
func (c *Client) PullMap() (m map[string]any, err error) {
	content, err := c.PullRaw()
	if err != nil {
		return nil, fmt.Errorf("拉取配置失败: %w", err)
	}
	m, err = parseContent(content, c.cfg.Format)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cached = m
	c.mu.Unlock()
	return m, nil
}

// GetConfig 按 key-path 读取配置（每次调用都会重新拉取，供上层自己决定缓存策略）
//
//	value   : 读取到的值
//	existed : 配置是否存在
//	err     : 错误
func (c *Client) GetConfig(key string) (any, bool, error) {
	m, err := c.PullMap()
	if err != nil {
		return nil, false, err
	}
	val, err := dic.GetValueByMapPath(m, key)
	if err != nil {
		return nil, false, nil
	}
	return val, val != nil, nil
}

// WatchConfig 监听 DataId 整体变化；callback 收到的是新的原始字符串
//
// 注意：Nacos 的监听粒度是 DataId，不能只监听某个子 key；
// 上层 Provider 会基于 DataId 变化做 key 级别的 diff。
func (c *Client) WatchConfig(callback func(content string)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	return c.cli.ListenConfig(vo.ConfigParam{
		DataId: c.cfg.DataId,
		Group:  c.cfg.Group,
		OnChange: func(namespace, group, dataId, data string) {
			callback(data)
		},
	})
}

// CancelWatch 取消对 DataId 的监听
func (c *Client) CancelWatch() error {
	return c.cli.CancelListenConfig(vo.ConfigParam{
		DataId: c.cfg.DataId,
		Group:  c.cfg.Group,
	})
}

// Close 关闭客户端
func (c *Client) Close() error {
	c.cli.CloseClient()
	return nil
}

// ================================ 工具函数 ================================

// parseContent 按格式把字符串解析为 map
func parseContent(content, format string) (map[string]any, error) {
	if strings.TrimSpace(content) == "" {
		return map[string]any{}, nil
	}
	m := map[string]any{}
	switch strings.ToLower(format) {
	case "json":
		if err := json.Unmarshal([]byte(content), &m); err != nil {
			return nil, fmt.Errorf("解析 JSON 配置失败: %w", err)
		}
	case "yaml", "yml":
		if err := yaml.Unmarshal([]byte(content), &m); err != nil {
			return nil, fmt.Errorf("解析 YAML 配置失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("不支持的格式: %s", format)
	}
	return m, nil
}

// splitHostPort 解析 "host:port" 字符串
func splitHostPort(addr string) (string, uint64, error) {
	idx := strings.LastIndex(addr, ":")
	if idx <= 0 || idx == len(addr)-1 {
		return "", 0, fmt.Errorf("地址格式应为 host:port")
	}
	host := addr[:idx]
	var port uint64
	if _, err := fmt.Sscanf(addr[idx+1:], "%d", &port); err != nil {
		return "", 0, fmt.Errorf("端口解析失败: %w", err)
	}
	return host, port, nil
}

// truncate 截断字符串，用于错误日志
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
