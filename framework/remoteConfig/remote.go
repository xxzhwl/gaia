// Package remoteConfig 远程配置中心框架
//
// 本包提供了可扩展的远程配置中心抽象：
//   - ConfigCenter  : 配置中心接口，所有 Provider 需实现
//   - BaseCenter    : 通用能力（缓存 + diff 比对 + 轮询式 watcher 调度）
//   - Provider 注册表：按类型名（如 consul/nacos/apollo/etcd）注册工厂函数
//   - InitFromConfig: 框架启动时按 RemoteConfig.Type 条件化装配
//
// 上游可以通过三种方式接入自己的配置中心：
//
//  1. 直接实现 ConfigCenter 接口后调用 RegisterConfigCenter(myCenter)
//  2. 实现 ConfigCenter 并注册 Provider，通过配置 RemoteConfig.Type=xxx 自动装配
//  3. 嵌入 BaseCenter，仅实现 fetchFunc 即可免费获得 缓存/diff/监听 能力
//
// @author wanlizhan
// @created 2024-12-05
// @refactored 2026-04-24
package remoteConfig

import (
	"encoding/json"
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
	//   - 实现可以基于原生 Watch（etcd/apollo/nacos）或轮询（consul）
	//   - 同一 key 重复注册应返回 error
	WatchConfig(key string, callback func(any)) error
}

// Closer 可选接口：若 Provider 持有底层长连接/watcher，建议实现以便优雅退出
type Closer interface {
	Close() error
}

// ================================ Provider 注册表 ================================

// Provider 配置中心工厂函数
//
//	cfg 是当前 RemoteConfig.* 的完整配置树（map 形式），
//	Provider 自行从中按需读取自己关心的字段。
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
//	    remoteConfig.RegisterProvider("consul", newConsulProvider)
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
//	  Type: consul       # consul | nacos | apollo | etcd | "" (不启用)
//	  EndPoint: ...
//	  Path: ...
//	  # 各 Provider 私有字段放在对应命名空间下，详见各 Provider 文档
//
//	特殊行为：
//	 - Type 为空或 "none"：什么都不做（允许上游手动 RegisterConfigCenter）
//	 - Type 未在 Provider 注册表中：warn 一次后退出，不 panic
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

	// 2. 读取 Type；兼容旧行为：未显式配置 Type 但有 EndPoint 时默认 consul
	typ := gaia.GetSafeConfString("RemoteConfig.Type")
	if typ == "" {
		if gaia.GetSafeConfString("RemoteConfig.EndPoint") != "" {
			typ = "consul"
		}
	}
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

// ================================ BaseCenter 通用基类 ================================

// FetchFunc 由具体 Provider 提供的读取函数
// 返回值语义同 ConfigCenter.GetConfig
type FetchFunc func(key string) (value any, existed bool, err error)

// BaseCenter 抽取了所有"轮询型"配置中心的通用能力：
//   - 内存缓存（带 TTL）
//   - 配置变更 diff（基于 JSON）
//   - 轮询 watcher 调度
//
// Provider 实现只需要提供一个 FetchFunc，即可免费获得上述能力。
// 对于原生支持 Watch 的后端（etcd/apollo/nacos 推送），可以覆盖 WatchConfig。
type BaseCenter struct {
	fetch      FetchFunc
	cache      map[string]*cacheEntry
	mu         sync.RWMutex
	cacheTTL   time.Duration
	watchers   map[string]*watcherEntry
	watchersMu sync.RWMutex
	stopCh     chan struct{}
	stopOnce   sync.Once
	interval   time.Duration
}

type cacheEntry struct {
	value     any
	existed   bool
	timestamp time.Time
}

type watcherEntry struct {
	key      string
	callback func(any)
	stopCh   chan struct{}
}

// BaseOption BaseCenter 构造选项
type BaseOption struct {
	// CacheTTL 缓存时长；<=0 则使用默认 5 分钟
	CacheTTL time.Duration
	// WatchInterval 轮询间隔；<=0 则使用默认 5 秒
	WatchInterval time.Duration
}

// NewBaseCenter 基于 FetchFunc 构造通用基类
func NewBaseCenter(fetch FetchFunc, opt BaseOption) *BaseCenter {
	if opt.CacheTTL <= 0 {
		opt.CacheTTL = 5 * time.Minute
	}
	if opt.WatchInterval <= 0 {
		opt.WatchInterval = 5 * time.Second
	}
	return &BaseCenter{
		fetch:    fetch,
		cache:    make(map[string]*cacheEntry),
		watchers: make(map[string]*watcherEntry),
		stopCh:   make(chan struct{}),
		cacheTTL: opt.CacheTTL,
		interval: opt.WatchInterval,
	}
}

// GetConfig 实现 ConfigCenter 接口：带缓存的读取
func (b *BaseCenter) GetConfig(key string) (any, bool, error) {
	if entry, hit := b.readCache(key); hit {
		return entry.value, entry.existed, nil
	}
	val, existed, err := b.fetch(key)
	if err != nil {
		return nil, false, err
	}
	b.writeCache(key, val, existed)
	return val, existed, nil
}

// WatchConfig 实现 ConfigCenter 接口：轮询式监听
func (b *BaseCenter) WatchConfig(key string, callback func(any)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	b.watchersMu.Lock()
	defer b.watchersMu.Unlock()

	if _, exists := b.watchers[key]; exists {
		return fmt.Errorf("watcher for key '%s' already exists", key)
	}

	w := &watcherEntry{
		key:      key,
		callback: callback,
		stopCh:   make(chan struct{}),
	}
	b.watchers[key] = w
	go b.pollLoop(w)
	return nil
}

// StopWatch 停止指定 key 的监听
func (b *BaseCenter) StopWatch(key string) error {
	b.watchersMu.Lock()
	defer b.watchersMu.Unlock()
	w, ok := b.watchers[key]
	if !ok {
		return fmt.Errorf("watcher for key '%s' not found", key)
	}
	close(w.stopCh)
	delete(b.watchers, key)
	return nil
}

// StopAllWatches 停止所有 watcher（用于 Close）
func (b *BaseCenter) StopAllWatches() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
}

// Close 实现 Closer 接口
func (b *BaseCenter) Close() error {
	b.StopAllWatches()
	return nil
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

func (b *BaseCenter) writeCache(key string, val any, existed bool) {
	b.mu.Lock()
	b.cache[key] = &cacheEntry{value: val, existed: existed, timestamp: time.Now()}
	b.mu.Unlock()
}

func (b *BaseCenter) pollLoop(w *watcherEntry) {
	defer func() {
		if r := recover(); r != nil {
			gaia.ErrorF("[RemoteConfig] watcher[key=%s] panic: %v", w.key, r)
		}
	}()
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.checkChange(w)
		case <-w.stopCh:
			return
		case <-b.stopCh:
			return
		}
	}
}

func (b *BaseCenter) checkChange(w *watcherEntry) {
	defer func() {
		if r := recover(); r != nil {
			gaia.ErrorF("[RemoteConfig] checkChange[key=%s] panic: %v", w.key, r)
		}
	}()

	// 先取缓存中的旧值
	b.mu.RLock()
	old, hasOld := b.cache[w.key]
	b.mu.RUnlock()

	// 强制绕过缓存拉新值
	val, existed, err := b.fetch(w.key)
	if err != nil {
		gaia.ErrorF("[RemoteConfig] 拉取配置失败[key=%s]: %v", w.key, err)
		return
	}
	b.writeCache(w.key, val, existed)

	if !existed {
		return
	}
	if hasOld && !configEqual(old.value, val) {
		gaia.InfoF("[RemoteConfig] 配置变更[key=%s]", w.key)
		go w.callback(val)
	}
}

// configEqual 基于 JSON 序列化比较两个值是否相等
func configEqual(a, b any) bool {
	aJ, aE := json.Marshal(a)
	bJ, bE := json.Marshal(b)
	if aE != nil || bE != nil {
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
	return string(aJ) == string(bJ)
}
