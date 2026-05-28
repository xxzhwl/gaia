// Package nacos 对 Nacos 官方 Go SDK 做轻量封装，提供配置中心能力。
//
// 典型用法：
//
//	cli, err := nacos.NewClient(nacos.Config{
//	    ServerAddrs: []string{"127.0.0.1:8848"},
//	    Group:       "DEFAULT_GROUP",        // 所有 DataIds 共用此 group
//	    DataIds:     []string{"app.yaml"},   // 单份配置
//	})
//
// 多 DataId 用法：
//
//	cli, err := nacos.NewClient(nacos.Config{
//	    ServerAddrs: []string{"127.0.0.1:8848"},
//	    Group:       "DEFAULT_GROUP",
//	    DataIds: []string{
//	        "common.yaml",      // 优先级最低（base）
//	        "biz.yaml",         // 中
//	        "overrides.yaml",   // 优先级最高（最后定义覆盖）
//	    },
//	})
//	val, ok, err := cli.GetConfig("Server.Port")
//	_ = cli.WatchMapConfig(func(merged map[string]any, raw string) { ... })
//
// 多 DataId 合并语义：
//   - **所有 DataId 共用同一 Group**（即 Config.Group）
//   - 数组顺序 = 优先级，**后定义的覆盖先定义的**
//   - 顶层 deep-merge：子 map 递归合并、叶子值后者覆盖前者
//   - 任一 DataId 推送变更 → 重建合并视图 → 触发 callback（cb 收到 merged + 触发本次的原始内容）
//   - **不支持** per-spec 的 Group / Format / Prefix 隔离；如有此类需求请使用多个 Client 实例
//
// Format 决议规则（每个 DataId 独立判定）：
//  1. 若 DataId 后缀为 `.json` → json
//  2. 若 DataId 后缀为 `.yaml` / `.yml` → yaml
//  3. 否则 → 使用 Config.Format（顶层兜底，空则 yaml）
//
// 缓存语义：
//   - GetConfig(key)         → cached-first：先看本地 merged 快照，未命中再 PullMap
//   - GetConfigFromRemote(k) → 强制远程拉取所有 DataId 后取 key（绕过 cached）
//   - GetCachedConfig(k)     → 仅读 merged，不发起任何远程请求
//
// @author wanlizhan
// @created 2026-04-24
// @refactored 2026-06-05
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
	// ===== 必填核心 =====

	// ServerAddrs Nacos server 地址列表，形如 "host:port"；与 Endpoint 二选一
	ServerAddrs []string

	// DataIds 一个或多个 DataId 名称（必填，至少一个）
	//
	// 数组顺序 = 优先级，后定义的覆盖先定义的（顶层 deep-merge）。
	// 所有 DataId 共用 Group 字段。
	DataIds []string

	// ===== 寻址 / 鉴权 =====

	// Namespace 命名空间 ID（非中文名），可空
	Namespace string
	// Group 所有 DataIds 共用的分组；空则使用 "DEFAULT_GROUP"
	Group string
	// Username/Password Nacos 开启账号鉴权时必填
	Username string
	Password string
	// AccessKey/SecretKey 阿里云 MSE / 公有云 Nacos 鉴权
	AccessKey string
	SecretKey string

	// ===== 服务端寻址扩展 =====

	// Endpoint 服务端发现端点（阿里云 MSE / 公网 Nacos）；与 ServerAddrs 二选一
	Endpoint string
	// Scheme 服务端协议：http / https；空走 SDK 默认（http）
	Scheme string
	// ContextPath 非默认部署路径，例如 "/nacos"
	ContextPath string

	// ===== 客户端行为 =====

	// TimeoutMs 请求超时（毫秒），默认 5000
	TimeoutMs uint64
	// AppName 应用名，便于 Nacos 控制台识别来源
	AppName string
	// LogLevel SDK 日志级别：debug / info / warn / error；默认 warn
	LogLevel string
	// LogDir / CacheDir SDK 本地落盘路径，空则使用默认
	LogDir   string
	CacheDir string
	// Format 默认配置内容格式：yaml | json
	//
	//   - 每个 DataId 优先按文件名后缀判定 format（.json / .yaml / .yml）
	//   - 后缀无法判定时回退到本字段（空则兜底 yaml）
	Format string

	// ===== 容灾选项 =====

	// DisableUseSnapshot 远端不可达时禁止读 SDK 本地 snapshot；默认 false
	DisableUseSnapshot bool
	// NotLoadCacheAtStart 启动时是否跳过加载本地缓存；默认 true
	NotLoadCacheAtStart *bool

	// ===== KMS（阿里云 KMS 加密配置） =====

	OpenKMS    bool
	RegionId   string
	KMSVersion string
}

// resolvedSpec 内部解析后的 DataId 规约（Group 由 client 统一持有）
type resolvedSpec struct {
	DataId string
	Format string
}

// Client Nacos 客户端
type Client struct {
	cfg   Config
	specs []resolvedSpec // 标准化后的 spec 列表，保证 len>=1
	cli   config_client.IConfigClient

	mu      sync.RWMutex
	perSpec map[string]map[string]any // dataId -> 该 DataId 解析后的 raw map
	merged  map[string]any            // 按 specs 顺序 deep-merge 后的合并视图
}

// NewClient 创建 Nacos 配置客户端
func NewClient(cfg Config) (*Client, error) {
	if len(cfg.ServerAddrs) == 0 && cfg.Endpoint == "" {
		return nil, fmt.Errorf("ServerAddrs 与 Endpoint 至少需配置一个")
	}

	specs, err := normalizeSpecs(&cfg)
	if err != nil {
		return nil, err
	}

	if cfg.Group == "" {
		cfg.Group = "DEFAULT_GROUP"
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = 5000
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "warn"
	}
	notLoadCacheAtStart := true
	if cfg.NotLoadCacheAtStart != nil {
		notLoadCacheAtStart = *cfg.NotLoadCacheAtStart
	}

	// ----- 服务端配置 -----
	serverConfigs := make([]constant.ServerConfig, 0, len(cfg.ServerAddrs))
	for _, addr := range cfg.ServerAddrs {
		host, port, err := splitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("解析 ServerAddr[%s] 失败: %w", addr, err)
		}
		opts := []constant.ServerOption{}
		if cfg.Scheme != "" {
			opts = append(opts, constant.WithScheme(cfg.Scheme))
		}
		if cfg.ContextPath != "" {
			opts = append(opts, constant.WithContextPath(cfg.ContextPath))
		}
		serverConfigs = append(serverConfigs, *constant.NewServerConfig(host, port, opts...))
	}

	// ----- 客户端配置 -----
	clientOpts := []constant.ClientOption{
		constant.WithNamespaceId(cfg.Namespace),
		constant.WithUsername(cfg.Username),
		constant.WithPassword(cfg.Password),
		constant.WithTimeoutMs(cfg.TimeoutMs),
		constant.WithNotLoadCacheAtStart(notLoadCacheAtStart),
		constant.WithLogDir(cfg.LogDir),
		constant.WithCacheDir(cfg.CacheDir),
		constant.WithLogLevel(cfg.LogLevel),
	}
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, constant.WithEndpoint(cfg.Endpoint))
	}
	if cfg.AccessKey != "" {
		clientOpts = append(clientOpts, constant.WithAccessKey(cfg.AccessKey))
	}
	if cfg.SecretKey != "" {
		clientOpts = append(clientOpts, constant.WithSecretKey(cfg.SecretKey))
	}
	if cfg.AppName != "" {
		clientOpts = append(clientOpts, constant.WithAppName(cfg.AppName))
	}
	if cfg.DisableUseSnapshot {
		clientOpts = append(clientOpts, constant.WithDisableUseSnapShot(true))
	}
	if cfg.OpenKMS {
		clientOpts = append(clientOpts, constant.WithOpenKMS(true))
		if cfg.RegionId != "" {
			clientOpts = append(clientOpts, constant.WithRegionId(cfg.RegionId))
		}
		if cfg.KMSVersion != "" {
			clientOpts = append(clientOpts, constant.WithKMSVersion(constant.KMSVersion(cfg.KMSVersion)))
		}
	}
	clientConfig := *constant.NewClientConfig(clientOpts...)

	cli, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &clientConfig,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos 配置客户端失败: %w", err)
	}

	return &Client{cfg: cfg, specs: specs, cli: cli}, nil
}

// normalizeSpecs 把 Config.DataIds 标准化为内部 spec 列表，并完成 Format 推导
func normalizeSpecs(cfg *Config) ([]resolvedSpec, error) {
	if len(cfg.DataIds) == 0 {
		return nil, fmt.Errorf("DataIds 至少需配置一个")
	}
	out := make([]resolvedSpec, 0, len(cfg.DataIds))
	for i, raw := range cfg.DataIds {
		id := strings.TrimSpace(raw)
		if id == "" {
			return nil, fmt.Errorf("DataIds[%d] 不能为空", i)
		}
		out = append(out, resolvedSpec{
			DataId: id,
			Format: guessFormatByDataId(id, cfg.Format),
		})
	}
	return out, nil
}

// guessFormatByDataId 按 DataId 后缀判断 yaml / json，未知时使用 fallback（空则兜底 yaml）
func guessFormatByDataId(dataId, fallback string) string {
	switch {
	case strings.HasSuffix(dataId, ".json"):
		return "json"
	case strings.HasSuffix(dataId, ".yml"), strings.HasSuffix(dataId, ".yaml"):
		return "yaml"
	}
	if fallback != "" {
		return fallback
	}
	return "yaml"
}

// NewClientWithSchema 从 gaia 配置中读取
//
// 读取 schema.DataIds（字符串数组）；其余字段同上。
func NewClientWithSchema(schema string) (*Client, error) {
	cfg := Config{
		ServerAddrs:        gaia.GetSafeConfStringSliceFromString(schema + ".ServerAddrs"),
		Namespace:          gaia.GetSafeConfString(schema + ".Namespace"),
		Group:              gaia.GetSafeConfString(schema + ".Group"),
		Username:           gaia.GetSafeConfString(schema + ".Username"),
		Password:           gaia.GetSafeConfString(schema + ".Password"),
		TimeoutMs:          uint64(gaia.GetSafeConfInt64(schema + ".TimeoutMs")),
		Format:             gaia.GetSafeConfString(schema + ".Format"),
		LogDir:             gaia.GetSafeConfString(schema + ".LogDir"),
		CacheDir:           gaia.GetSafeConfString(schema + ".CacheDir"),
		AccessKey:          gaia.GetSafeConfString(schema + ".AccessKey"),
		SecretKey:          gaia.GetSafeConfString(schema + ".SecretKey"),
		Endpoint:           gaia.GetSafeConfString(schema + ".Endpoint"),
		Scheme:             gaia.GetSafeConfString(schema + ".Scheme"),
		ContextPath:        gaia.GetSafeConfString(schema + ".ContextPath"),
		AppName:            gaia.GetSafeConfString(schema + ".AppName"),
		LogLevel:           gaia.GetSafeConfString(schema + ".LogLevel"),
		DisableUseSnapshot: gaia.GetSafeConfBool(schema + ".DisableUseSnapshot"),
		OpenKMS:            gaia.GetSafeConfBool(schema + ".OpenKMS"),
		RegionId:           gaia.GetSafeConfString(schema + ".RegionId"),
		KMSVersion:         gaia.GetSafeConfString(schema + ".KMSVersion"),
	}
	if v, err := gaia.GetConf(schema + ".DataIds"); err == nil && v != nil {
		cfg.DataIds = parseDataIdsAny(v)
	}
	return NewClient(cfg)
}

// NewFrameworkClient 使用 RemoteConfig.Nacos 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("RemoteConfig.Nacos")
}

// parseDataIdsAny 把 any 解析为 []string；支持 []any of string、[]string、逗号分隔字符串
func parseDataIdsAny(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			if s, ok := it.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					out = append(out, t)
				}
			}
		}
		return out
	case string:
		parts := strings.Split(x, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	return nil
}

// GetCli 返回底层 SDK client（需要调用高级能力时使用）
func (c *Client) GetCli() config_client.IConfigClient {
	return c.cli
}

// DataIds 返回当前生效的 DataId 列表（只读副本）
func (c *Client) DataIds() []string {
	out := make([]string, len(c.specs))
	for i, s := range c.specs {
		out[i] = s.DataId
	}
	return out
}

// Group 返回所有 DataIds 共用的 group
func (c *Client) Group() string { return c.cfg.Group }

// Namespace 返回当前命名空间 ID（空则表示 Nacos 默认 public）
func (c *Client) Namespace() string { return c.cfg.Namespace }

// ServerAddrs 返回当前 server 地址列表（只读副本）
func (c *Client) ServerAddrs() []string {
	out := make([]string, len(c.cfg.ServerAddrs))
	copy(out, c.cfg.ServerAddrs)
	return out
}

// PullRaw 拉取 *第一个 DataId* 的完整原始字符串内容
//
// 多 DataId 场景下若需获取每份原始内容，请使用 PullRawAll。
func (c *Client) PullRaw() (string, error) {
	s := c.specs[0]
	return c.cli.GetConfig(vo.ConfigParam{DataId: s.DataId, Group: c.cfg.Group})
}

// GetConfigByDataId 按 DataId 名称拉取原始内容（不走缓存，直接从 Nacos 拉取）
func (c *Client) GetConfigByDataId(dataId string) (string, error) {
	return c.cli.GetConfig(vo.ConfigParam{DataId: dataId, Group: c.cfg.Group})
}

// PullRawAll 拉取所有 DataId 的原始内容；返回 map 的 key 为 DataId
func (c *Client) PullRawAll() (map[string]string, error) {
	out := make(map[string]string, len(c.specs))
	for _, s := range c.specs {
		raw, err := c.cli.GetConfig(vo.ConfigParam{DataId: s.DataId, Group: c.cfg.Group})
		if err != nil {
			return nil, fmt.Errorf("拉取 %s@%s 失败: %w", s.DataId, c.cfg.Group, err)
		}
		out[s.DataId] = raw
	}
	return out, nil
}

// PullMap 拉取所有 DataId 并合并为 map[string]any，同时刷新本地 perSpec / merged
func (c *Client) PullMap() (map[string]any, error) {
	perSpec := make(map[string]map[string]any, len(c.specs))
	for _, s := range c.specs {
		raw, err := c.cli.GetConfig(vo.ConfigParam{DataId: s.DataId, Group: c.cfg.Group})
		if err != nil {
			return nil, fmt.Errorf("拉取 %s@%s 失败: %w", s.DataId, c.cfg.Group, err)
		}
		m, err := parseContent(raw, s.Format)
		if err != nil {
			return nil, fmt.Errorf("解析 %s@%s 失败: %w", s.DataId, c.cfg.Group, err)
		}
		perSpec[s.DataId] = m
	}
	merged := buildMerged(c.specs, perSpec)

	c.mu.Lock()
	c.perSpec = perSpec
	c.merged = merged
	c.mu.Unlock()
	return merged, nil
}

// ParseContent 按当前 client 的默认格式解析任意原始内容为 map（不修改 cached）
//
// 多 DataId 场景下，使用第一个 DataId 的格式作为兜底；如需指定格式请用 ParseContentAs。
func (c *Client) ParseContent(content string) (map[string]any, error) {
	return parseContent(content, c.specs[0].Format)
}

// ParseContentAs 按指定格式解析（yaml | json）
func (c *Client) ParseContentAs(content, format string) (map[string]any, error) {
	return parseContent(content, format)
}

// GetConfig 按 key-path 读取配置（cached-first）
//
//   - 先查本地 merged 快照；
//   - merged 为空则触发一次 PullMap（拉取所有 DataId 并合并）；
//   - 不希望走 cached 的高敏感场景请使用 GetConfigFromRemote
func (c *Client) GetConfig(key string) (any, bool, error) {
	if v, ok := c.GetCachedConfig(key); ok {
		return v, true, nil
	}
	c.mu.RLock()
	hasCached := c.merged != nil
	c.mu.RUnlock()
	if !hasCached {
		if _, err := c.PullMap(); err != nil {
			return nil, false, err
		}
	}
	c.mu.RLock()
	m := c.merged
	c.mu.RUnlock()
	if m == nil {
		return nil, false, nil
	}
	val, err := dic.GetValueByMapPath(m, key)
	if err != nil {
		return nil, false, nil
	}
	return val, val != nil, nil
}

// GetConfigFromRemote 强制远程拉取所有 DataId 后取 key（绕过 cached），同时会刷新 cached
func (c *Client) GetConfigFromRemote(key string) (any, bool, error) {
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

// GetCachedConfig 仅读本地 merged（不发起任何远程请求）；需先调用过 PullMap 或 WatchMapConfig
func (c *Client) GetCachedConfig(key string) (any, bool) {
	c.mu.RLock()
	m := c.merged
	c.mu.RUnlock()
	if m == nil {
		return nil, false
	}
	val, err := dic.GetValueByMapPath(m, key)
	if err != nil || val == nil {
		return nil, false
	}
	return val, true
}

// WarmCache 主动拉取一次并刷新 merged；调用后可以使用 GetCachedConfig
func (c *Client) WarmCache() error {
	_, err := c.PullMap()
	return err
}

// WatchConfig 监听 *第一个 DataId* 的整体变化；callback 收到的是新的原始字符串。
//
// 多 DataId 场景下推荐使用 WatchMapConfig。
func (c *Client) WatchConfig(callback func(content string)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	s := c.specs[0]
	return c.cli.ListenConfig(vo.ConfigParam{
		DataId: s.DataId,
		Group:  c.cfg.Group,
		OnChange: func(namespace, group, dataId, data string) {
			c.applyPushedSpec(s, data)
			callback(data)
		},
	})
}

// WatchMapConfig 监听所有 DataId 的变化，回调直接拿到合并后的 map。
//
//   - merged : 重建后的合并视图（任一 DataId 变更后会同步刷新）
//   - raw    : 触发本次回调的那个 DataId 的原始字符串内容（多 DataId 场景下方便审计）
func (c *Client) WatchMapConfig(callback func(merged map[string]any, raw string)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	for _, s := range c.specs {
		spec := s // capture
		err := c.cli.ListenConfig(vo.ConfigParam{
			DataId: spec.DataId,
			Group:  c.cfg.Group,
			OnChange: func(namespace, group, dataId, data string) {
				merged := c.applyPushedSpec(spec, data)
				callback(merged, data)
			},
		})
		if err != nil {
			return fmt.Errorf("注册 %s@%s 监听失败: %w", spec.DataId, c.cfg.Group, err)
		}
	}
	return nil
}

// applyPushedSpec 收到推送时刷新 perSpec[DataId] 并重建 merged，返回新的 merged
func (c *Client) applyPushedSpec(spec resolvedSpec, data string) map[string]any {
	m, perr := parseContent(data, spec.Format)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.perSpec == nil {
		c.perSpec = make(map[string]map[string]any, len(c.specs))
	}
	if perr == nil && m != nil {
		c.perSpec[spec.DataId] = m
	}
	c.merged = buildMerged(c.specs, c.perSpec)
	return c.merged
}

// CancelWatch 取消对所有 DataId 的监听
func (c *Client) CancelWatch() error {
	var firstErr error
	for _, s := range c.specs {
		if err := c.cli.CancelListenConfig(vo.ConfigParam{
			DataId: s.DataId,
			Group:  c.cfg.Group,
		}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// PublishConfigAt 显式指定 DataId / Group 推送内容（管理后台场景）
//
// 注意：本方法不会刷新 client 本地 cached，调用方如需立即生效请等待 push 回调或显式 PullMap。
func (c *Client) PublishConfigAt(dataId, group, content string) (bool, error) {
	if dataId == "" {
		return false, fmt.Errorf("dataId 不能为空")
	}
	if group == "" {
		group = c.cfg.Group
	}
	return c.cli.PublishConfig(vo.ConfigParam{
		DataId:  dataId,
		Group:   group,
		Content: content,
	})
}

// DeleteConfigAt 显式指定 DataId / Group 删除（管理后台场景）
func (c *Client) DeleteConfigAt(dataId, group string) (bool, error) {
	if dataId == "" {
		return false, fmt.Errorf("dataId 不能为空")
	}
	if group == "" {
		group = c.cfg.Group
	}
	return c.cli.DeleteConfig(vo.ConfigParam{
		DataId: dataId,
		Group:  group,
	})
}

// Close 关闭客户端
func (c *Client) Close() error {
	c.cli.CloseClient()
	return nil
}

// ================================ 合并 / 解析工具 ================================

// buildMerged 按 specs 顺序重建合并视图：
//   - 顺序遍历，逐份 deep-merge 到累积器；
//   - 末尾的 spec 覆盖前面 spec 的同名叶子。
func buildMerged(specs []resolvedSpec, perSpec map[string]map[string]any) map[string]any {
	out := map[string]any{}
	for _, s := range specs {
		m := perSpec[s.DataId]
		if m == nil {
			continue
		}
		deepMergeInto(out, m)
	}
	return out
}

// deepMergeInto 把 src 递归合并到 dst（叶子值后者覆盖前者）
//
// 规则：
//   - dst[k] 与 src[k] 都是 map → 递归 merge
//   - 否则 → src[k] 覆盖 dst[k]
func deepMergeInto(dst, src map[string]any) {
	for k, v := range src {
		if vm, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				deepMergeInto(dv, vm)
				continue
			}
			// dst 这一格不是 map → 直接用 src 的 map（不需要再深拷贝，src 是新解析的）
			dst[k] = vm
			continue
		}
		dst[k] = v
	}
}

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
