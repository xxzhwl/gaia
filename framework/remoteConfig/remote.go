// Package remoteConfig 远程配置中心框架
//
// 本包提供了可扩展的远程配置中心抽象：
//   - ConfigCenter      : 配置中心接口，所有 Provider 需实现
//   - BaseCenter        : 通用 KV 缓存层，供 Provider 复用（仅缓存，不做监听）
//   - Provider 注册表   : 按类型名（如 nacos / configmap）注册工厂函数
//   - InitFromConfig    : 框架启动时按 RemoteConfig.Type 条件化装配
//
// 上游可以通过三种方式接入自己的配置中心：
//
//  1. 直接实现 ConfigCenter 接口后调用 RegisterConfigCenter(myCenter)
//  2. 实现 ConfigCenter 并注册 Provider，通过配置 RemoteConfig.Type=xxx 自动装配
//  3. 嵌入 BaseCenter，仅实现 fetchFunc 即可免费获得 GetConfig 二级缓存能力；
//     WatchConfig 需要 Provider 自行实现（通常基于后端的原生 push 机制）
//
// 设计取舍：
//   - 历史上 BaseCenter 还附带"按 key 轮询"的 watcher，主要服务 Consul 这类 KV 模型。
//     从 2026-06 起，框架放弃 consul / etcd 作为远程配置中心，
//     主推 Nacos（DataId 整份文件 + SDK 原生 push）和 K8s ConfigMap（挂载文件 + fsnotify），
//     这两种 Provider 都自己实现 WatchConfig，不再需要轮询。BaseCenter 仅保留缓存能力。
//
// @author wanlizhan
// @created 2024-12-05
// @refactored 2026-06-05
package remoteConfig

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
)

// ================================ 接口定义 ================================

// ConfigCenter 配置中心接口
//
// 所有远程配置中心实现都必须满足这个接口。框架通过 GetConfFromRemote 变量把
// GetConfig 暴露给 gaia.GetConf 系列函数使用。
type ConfigCenter interface {
	// GetConfig 读取指定 key 的配置
	//   - value   : 读取到的配置值（JSON 可解析的任意类型）
	//   - existed : 配置是否存在
	//   - err     : 网络/序列化/鉴权等错误
	GetConfig(key string) (value any, existed bool, err error)

	// WatchConfig 监听 key 的配置变化
	//   - callback 会在配置发生变化时被异步调用
	//   - 实现应当基于后端原生 push（Nacos ListenConfig / fsnotify 等），不要再走轮询
	//   - 同一 key 重复注册应返回 error
	WatchConfig(key string, callback func(any)) error
}

// Closer 可选接口：若 Provider 持有底层长连接 / watcher，建议实现以便优雅退出
type Closer interface {
	Close() error
}

// ConfigDumper 可选接口：支持全量配置导出的配置中心。
// API 管理界面可以通过该接口展示远程配置的完整内容。
type ConfigDumper interface {
	DumpConfig() (map[string]any, error)
}

// KeyWatchStopper 可选接口：支持取消单个 key 的监听
type KeyWatchStopper interface {
	StopWatch(key string) error
}

// Prober 可选接口：Provider 暴露一个轻量的可达性探活方法
//
// 框架启动 Init 阶段在 component_check 里会调用本方法；耗时受
// Gaia.ProbeTimeout（秒，默认 3）约束。建议实现方：
//   - Nacos：触发一次 GetConfig（首个 DataId）验证服务端可达 + 鉴权通过
//   - K8s ConfigMap：os.Stat 检查挂载文件可读
//   - 其它：自行实现轻量探活，避免 panic 与长时间阻塞
type Prober interface {
	Probe(ctx context.Context) error
}

// ================================ Provider 注册表 ================================

// Provider 配置中心工厂函数
//
//	cfg 是当前 RemoteConfig.* 的完整配置树（map 形式），
//	Provider 自行从中按需读取自己关心的字段，避免重复回头调 gaia.GetConf。
type Provider func(cfg map[string]any) (ConfigCenter, error)

var (
	providerRegistry   = map[string]Provider{}
	providerRegistryMu sync.RWMutex
)

// RegisterProvider 注册一个命名 Provider；重复注册会覆盖旧值。
//
// Provider 子包通常在 init() 中调用本函数自我注册，例如：
//
//	func init() {
//	    remoteConfig.RegisterProvider("nacos", newNacosProvider)
//	}
func RegisterProvider(name string, p Provider) {
	if name == "" || p == nil {
		return
	}
	providerRegistryMu.Lock()
	providerRegistry[name] = p
	providerRegistryMu.Unlock()
}

// GetProvider 获取指定名字的 Provider
func GetProvider(name string) (Provider, bool) {
	providerRegistryMu.RLock()
	defer providerRegistryMu.RUnlock()
	p, ok := providerRegistry[name]
	return p, ok
}

// ListProviders 列出所有已注册的 Provider 名字（用于日志/诊断）
func ListProviders() []string {
	providerRegistryMu.RLock()
	defer providerRegistryMu.RUnlock()
	names := make([]string, 0, len(providerRegistry))
	for k := range providerRegistry {
		names = append(names, k)
	}
	return names
}

// ================================ 自动装配入口 ================================

// activeCenter 当前激活的配置中心（用于框架优雅退出时 Close）
var (
	activeCenter   ConfigCenter
	activeCenterMu sync.RWMutex
)

// InitFromConfig 根据配置 RemoteConfig.Type 选择 Provider 并装配。
//
// 配置约定：
//
//	RemoteConfig:
//	  Type: nacos          # nacos | configmap | "" (不启用)
//	  # 各 Provider 私有字段放在对应命名空间下，详见各 Provider 文档
//
// 特殊行为：
//   - Type 为空或 "none"：什么都不做（允许上游手动 RegisterConfigCenter）
//   - Type 未在 Provider 注册表中：warn 一次后退出，不 panic
//
// 返回值：
//   - 当前激活的配置中心（若未装配则为 nil）
//   - 错误（仅当 Provider 构造过程中返回错误时）
func InitFromConfig() (ConfigCenter, error) {
	// 1. 已有上游手动注入的中心，直接复用
	activeCenterMu.RLock()
	existing := activeCenter
	activeCenterMu.RUnlock()
	if existing != nil {
		gaia.Info("[RemoteConfig] 已存在自定义配置中心，跳过自动装配")
		return existing, nil
	}

	// 2. 读取 Type
	typ := gaia.GetSafeConfString("RemoteConfig.Type")
	if typ == "" || typ == "none" {
		gaia.Info("[RemoteConfig] 未配置远程配置中心，跳过装配")
		return nil, nil
	}

	// 3. 查询 Provider
	p, ok := GetProvider(typ)
	if !ok {
		gaia.WarnF("[RemoteConfig] 未找到 Provider[%s]，已注册 Provider: %v", typ, ListProviders())
		return nil, nil
	}

	// 4. 组装原始配置树，交给 Provider 自行解析
	raw, _ := gaia.GetConf("RemoteConfig")
	cfgMap, _ := raw.(map[string]any)
	if cfgMap == nil {
		cfgMap = map[string]any{}
	}

	center, err := p(cfgMap)
	if err != nil {
		gaia.ErrorF("[RemoteConfig] 创建 Provider[%s] 失败: %v", typ, err)
		return nil, fmt.Errorf("创建 Provider[%s] 失败: %w", typ, err)
	}

	RegisterConfigCenter(center)
	gaia.InfoF("[RemoteConfig] 已装配 Provider[%s]", typ)
	return center, nil
}

// RegisterConfigCenter 注册/替换当前激活的配置中心
//
// 上游可以在 framework.Init() 之前调用它手动装配自定义中心；
// 之后再调 framework.Init()，InitFromConfig 会检测到并跳过默认装配。
func RegisterConfigCenter(center ConfigCenter) {
	if center == nil {
		return
	}
	activeCenterMu.Lock()
	// 若已有旧中心且实现了 Closer，优雅关闭
	if old := activeCenter; old != nil {
		if c, ok := old.(Closer); ok {
			_ = c.Close()
		}
	}
	activeCenter = center
	activeCenterMu.Unlock()

	gaia.GetConfFromRemote = func(key string) (any, bool, error) {
		return center.GetConfig(key)
	}
}

// ActiveCenter 获取当前激活的配置中心（可能为 nil）
func ActiveCenter() ConfigCenter {
	activeCenterMu.RLock()
	defer activeCenterMu.RUnlock()
	return activeCenter
}

// CloseActiveCenter 关闭当前激活的配置中心（若实现了 Closer）
func CloseActiveCenter() error {
	activeCenterMu.Lock()
	defer activeCenterMu.Unlock()
	if activeCenter == nil {
		return nil
	}
	if c, ok := activeCenter.(Closer); ok {
		return c.Close()
	}
	return nil
}

// ================================ BaseCenter（缓存基类） ================================

// FetchFunc 由具体 Provider 提供的读取函数；返回值语义同 ConfigCenter.GetConfig
type FetchFunc func(key string) (value any, existed bool, err error)

// BaseCenter 提供"带 TTL 的内存缓存 + GetConfig 包装"两件事，
// 让接入新后端的 Provider 不必每次重写缓存逻辑。
//
// 注意：BaseCenter 不再提供 WatchConfig 默认实现 —— Provider 必须自己实现它，
// 否则编译时会因为接口不满足而失败。这是有意为之：Nacos / ConfigMap 这类
// "整份快照 + 原生 push" 模型的后端不应该退化为 per-key 轮询。
type BaseCenter struct {
	fetch    FetchFunc
	cache    map[string]*cacheEntry
	mu       sync.RWMutex
	cacheTTL time.Duration
}

type cacheEntry struct {
	value     any
	existed   bool
	timestamp time.Time
}

// BaseOption BaseCenter 构造选项
type BaseOption struct {
	// CacheTTL 缓存时长；<=0 则使用默认 5 分钟
	CacheTTL time.Duration
}

// NewBaseCenter 基于 FetchFunc 构造缓存基类
func NewBaseCenter(fetch FetchFunc, opt BaseOption) *BaseCenter {
	if opt.CacheTTL <= 0 {
		opt.CacheTTL = 5 * time.Minute
	}
	return &BaseCenter{
		fetch:    fetch,
		cache:    make(map[string]*cacheEntry),
		cacheTTL: opt.CacheTTL,
	}
}

// GetConfig 实现 ConfigCenter 接口的读路径：带缓存的读取
func (b *BaseCenter) GetConfig(key string) (any, bool, error) {
	if entry, hit := b.readCache(key); hit {
		return entry.value, entry.existed, nil
	}
	val, existed, err := b.fetch(key)
	if err != nil {
		return nil, false, err
	}
	// 仅缓存"存在"的结果，不写负缓存：
	//   - 远端新增 key 在 cacheTTL 时间内会不可见；
	//   - 本地"刚被删除"的兜底文件场景下，早期请求的 key 长期不可读。
	if existed {
		b.writeCache(key, val, existed)
	}
	return val, existed, nil
}

// Close 默认实现：BaseCenter 自身没有需要释放的资源
func (b *BaseCenter) Close() error { return nil }

// ----- 缓存失效相关（供 Provider 在收到推送/检测到 diff 时调用）-----

// InvalidateCache 失效指定 key 的内存缓存条目
func (b *BaseCenter) InvalidateCache(key string) {
	if key == "" {
		return
	}
	b.mu.Lock()
	delete(b.cache, key)
	b.mu.Unlock()
}

// InvalidateCacheBatch 批量失效内存缓存
func (b *BaseCenter) InvalidateCacheBatch(keys []string) {
	if len(keys) == 0 {
		return
	}
	b.mu.Lock()
	for _, k := range keys {
		delete(b.cache, k)
	}
	b.mu.Unlock()
}

// InvalidateAllCache 失效全部内存缓存
// 适用于"远端整份配置变更"这类无法精确 diff 的场景
func (b *BaseCenter) InvalidateAllCache() {
	b.mu.Lock()
	b.cache = make(map[string]*cacheEntry)
	b.mu.Unlock()
}

// CachedKeys 列出当前已缓存的 key（用于 diff/调试）
func (b *BaseCenter) CachedKeys() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	keys := make([]string, 0, len(b.cache))
	for k := range b.cache {
		keys = append(keys, k)
	}
	return keys
}

// writeCache 直接写入一条缓存（供 Provider 在收到原生 push 时主动 warm-up）
func (b *BaseCenter) writeCache(key string, val any, existed bool) {
	b.mu.Lock()
	b.cache[key] = &cacheEntry{value: val, existed: existed, timestamp: time.Now()}
	b.mu.Unlock()
}

// ----- 内部方法 -----

func (b *BaseCenter) readCache(key string) (*cacheEntry, bool) {
	b.mu.RLock()
	entry, ok := b.cache[key]
	b.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(entry.timestamp) >= b.cacheTTL {
		return nil, false
	}
	return entry, true
}

// ================================ 辅助函数（公共） ================================

// toDuration 把 map 里读出的 interface{} 统一转成 time.Duration（按秒数解释）
// 支持 int / int64 / float64 / string；失败返回 fallback
func toDuration(raw any, fallback time.Duration) time.Duration {
	switch v := raw.(type) {
	case int:
		return time.Duration(v)
	case int64:
		return time.Duration(v)
	case float64:
		return time.Duration(int64(v))
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d / time.Second
		}
	}
	return fallback
}
