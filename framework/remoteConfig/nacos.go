// Package remoteConfig — Nacos Provider
//
// 配置示例（单 DataId）：
//
//	RemoteConfig:
//	  Type: nacos
//	  RemoteTimeoutMs: 3000                    # 建议 ≥2000，启动期 Nacos 首拉相对慢
//	  Nacos:
//	    ServerAddrs: 127.0.0.1:8848            # 多个用逗号分隔
//	    Endpoint: ""                           # 阿里云 MSE / 公网 Nacos：服务发现端点
//	    Namespace: ""                          # Nacos 命名空间 ID，默认 public
//	    Group: DEFAULT_GROUP                   # 所有 DataIds 共用此分组
//	    DataIds:                               # 至少一个；数组顺序 = 优先级（后者覆盖前者）
//	      - app.yaml
//	    Username: ""
//	    Password: ""
//	    AccessKey: ""                          # MSE / 阿里云 Nacos 鉴权
//	    SecretKey: ""
//	    Scheme: ""                             # http / https
//	    ContextPath: ""                        # 非默认部署路径，例如 "/nacos"
//	    AppName: ""                            # 控制台来源识别
//	    LogLevel: warn                         # debug / info / warn / error
//	    DisableUseSnapshot: false              # 远端不可达时禁止读 SDK 本地 snapshot
//	    OpenKMS: false                         # 阿里云 KMS 解密；cipher- 前缀 dataId 自动解密
//	    RegionId: ""
//	    KMSVersion: ""                         # v1.0 / v3.0
//	    TimeoutMs: 5000
//	    Format: yaml                           # 默认格式（每个 DataId 优先按文件名后缀判定）
//	    CacheTTL: 60                           # 秒，可选；BaseCenter 二级缓存 TTL
//
// 配置示例（多 DataId）：
//
//	RemoteConfig:
//	  Type: nacos
//	  Nacos:
//	    ServerAddrs: 127.0.0.1:8848
//	    Group: DEFAULT_GROUP                   # 所有 DataIds 共用此 group
//	    DataIds:                               # 数组顺序 = 优先级，后定义的覆盖先定义的
//	      - common.yaml                        # 优先级最低（base）
//	      - biz.yaml
//	      - overrides.yaml                     # 优先级最高（override）
//
// 多 DataId 合并语义：
//   - 顶层 deep-merge：子 map 递归合并、叶子值后者覆盖前者
//   - 所有 DataId 共用 Nacos.Group；如需跨 group 加载，请手动用多个 nacos.Client 实例
//   - 任一 DataId 推送变更 → Nacos Client 重建合并视图 → Provider diff changedPaths → 触发 watcher
//
// 设计要点：
//   - 单/多 DataId 整体内容会被解析为嵌套 map，按 spec 顺序合并后作为内存快照（snap）
//   - 每次 refresh/onPushMap 后，**完整快照** 会被持久化到 gaia.DefaultRemoteConfigFile
//     这样 remoteConfig.json 是"远端配置的合并镜像"，便于审阅和冷启动兜底
//   - 启动时若 Nacos 不可达，会尝试从 remoteConfig.json 加载上次的快照作为初值
//   - SDK 推送回调 OnChange 已经把最新内容传过来了，**不再二次 PullMap**
//   - WatchConfig(key, callback) 由本 Provider 自己管理：
//     维护 key → []callback，DataId 推送时 diff 出 changedPaths，按前缀匹配触发对应 callback；
//     **不再退化为按 key 轮询**
//   - onPushMap 时会做新旧快照 diff，对所有变更/删除的 key 同时失效：
//     1) BaseCenter 内存缓存
//     2) gaia.GetConf 一级缓存（conf-{key}）
//     使变更/删除立刻可见，无需等待 cacheTTL 过期
//
// @author wanlizhan
// @created 2026-04-24
// @refactored 2026-06-05
package remoteConfig

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/nacos"
	"github.com/xxzhwl/gaia/dic"
)

func init() {
	RegisterProvider("nacos", newNacosProvider)
}

// NacosConfigCenter Nacos 配置中心实现
type NacosConfigCenter struct {
	*BaseCenter
	cli *nacos.Client

	// 整份 DataId 内容的内存快照（map 形式）
	snap   map[string]any
	snapMu sync.RWMutex

	// key 级 watcher：key → callbacks。DataId 推送变更时按 changedPath 前缀匹配触发。
	watchersMu sync.Mutex
	watchers   map[string][]func(any)
}

// NewNacosConfigCenter 直接基于已有 nacos.Client 创建 ConfigCenter
func NewNacosConfigCenter(cli *nacos.Client, opt BaseOption) (*NacosConfigCenter, error) {
	if cli == nil {
		return nil, fmt.Errorf("nacos client 为空")
	}
	center := &NacosConfigCenter{
		cli:      cli,
		watchers: map[string][]func(any){},
	}
	center.BaseCenter = NewBaseCenter(center.fetch, opt)

	// 标记：远端中心接管 remoteConfig.json 的写入，
	// gaia.GetConf 不再以 flat-map 形式追加写文件，文件作为完整快照的镜像
	gaia.RemoteSnapshotOwned = true

	// 预热一次：Nacos 可达 → 把快照落盘；不可达 → 尝试从已有 remoteConfig.json 兜底
	if _, err := center.refresh(); err != nil {
		gaia.WarnF("[RemoteConfig][nacos] 初始拉取失败: %v，尝试从本地快照文件兜底", err)
		if loaded := center.loadLocalSnapshot(); loaded {
			gaia.InfoF("[RemoteConfig][nacos] 已从本地快照文件加载兜底配置: %s", gaia.DefaultRemoteConfigFile)
		}
	}

	// 注册 SDK 推送监听 —— 直接拿 map，避免二次 PullMap
	if err := cli.WatchMapConfig(center.onPushMap); err != nil {
		gaia.WarnF("[RemoteConfig][nacos] 注册 SDK 监听失败: %v", err)
	}

	// 启动期信息日志：把当前生效的关键参数打出来，方便排障与多环境核对
	gaia.InfoF("[RemoteConfig][nacos] 已就绪 ServerAddrs=%v Namespace=%q Group=%q DataIds=%v",
		cli.ServerAddrs(), cli.Namespace(), cli.Group(), cli.DataIds())
	return center, nil
}

// newNacosProvider 工厂函数：从 cfg map 解析所有 Nacos 字段，不再回头读 gaia.GetSafeConf
func newNacosProvider(cfg map[string]any) (ConfigCenter, error) {
	nacosCfg := getMap(cfg, "Nacos")

	cli, err := nacos.NewClient(nacos.Config{
		ServerAddrs:        getStringSlice(nacosCfg, "ServerAddrs"),
		Namespace:          getString(nacosCfg, "Namespace"),
		Group:              getString(nacosCfg, "Group"),
		DataIds:            parseDataIds(nacosCfg["DataIds"]),
		Username:           getString(nacosCfg, "Username"),
		Password:           getString(nacosCfg, "Password"),
		AccessKey:          getString(nacosCfg, "AccessKey"),
		SecretKey:          getString(nacosCfg, "SecretKey"),
		Endpoint:           getString(nacosCfg, "Endpoint"),
		Scheme:             getString(nacosCfg, "Scheme"),
		ContextPath:        getString(nacosCfg, "ContextPath"),
		AppName:            getString(nacosCfg, "AppName"),
		LogLevel:           getString(nacosCfg, "LogLevel"),
		LogDir:             getString(nacosCfg, "LogDir"),
		CacheDir:           getString(nacosCfg, "CacheDir"),
		Format:             getString(nacosCfg, "Format"),
		TimeoutMs:          uint64(toInt64(nacosCfg["TimeoutMs"], 0)),
		DisableUseSnapshot: getBool(nacosCfg, "DisableUseSnapshot"),
		OpenKMS:            getBool(nacosCfg, "OpenKMS"),
		RegionId:           getString(nacosCfg, "RegionId"),
		KMSVersion:         getString(nacosCfg, "KMSVersion"),
	})
	if err != nil {
		return nil, err
	}
	opt := BaseOption{
		CacheTTL: toDuration(nacosCfg["CacheTTL"], 0) * time.Second,
	}
	return NewNacosConfigCenter(cli, opt)
}

// parseDataIds 把配置里 "Nacos.DataIds" 解析为字符串数组
//
// 兼容三种写法：
//   - YAML 数组：["a.yaml", "b.yaml"]   → []any of string
//   - 已是字符串切片：[]string
//   - 逗号分隔字符串："a.yaml,b.yaml"
func parseDataIds(v any) []string {
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
		return splitAndTrim(x)
	}
	return nil
}

// ----- ConfigCenter / 内部实现 -----

// fetch 读取 key —— 优先内存快照，快照不存在时拉远端
func (n *NacosConfigCenter) fetch(key string) (any, bool, error) {
	n.snapMu.RLock()
	snap := n.snap
	n.snapMu.RUnlock()

	if snap == nil {
		m, err := n.refresh()
		if err != nil {
			return nil, false, err
		}
		snap = m
	}
	return lookup(snap, key)
}

// WatchConfig 注册 key 级监听。同一 key 重复注册会返回 error。
//
// 与 BaseCenter 的轮询版本不同：本实现不开 goroutine，
// 仅在 SDK 推送 onPushMap 时按 changedPath 前缀匹配触发回调。
func (n *NacosConfigCenter) WatchConfig(key string, callback func(any)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	n.watchersMu.Lock()
	defer n.watchersMu.Unlock()
	if _, ok := n.watchers[key]; ok {
		return fmt.Errorf("watcher for key '%s' already exists", key)
	}
	n.watchers[key] = []func(any){callback}
	return nil
}

// StopWatch 停止指定 key 的监听
func (n *NacosConfigCenter) StopWatch(key string) error {
	n.watchersMu.Lock()
	defer n.watchersMu.Unlock()
	if _, ok := n.watchers[key]; !ok {
		return fmt.Errorf("watcher for key '%s' not found", key)
	}
	delete(n.watchers, key)
	return nil
}

// refresh 拉取 DataId 全量内容并更新内存快照 + 落盘
func (n *NacosConfigCenter) refresh() (map[string]any, error) {
	m, err := n.cli.PullMap()
	if err != nil {
		return nil, err
	}
	n.snapMu.Lock()
	n.snap = m
	n.snapMu.Unlock()
	// 持久化到本地，作为 Nacos 不可达时的冷启动兜底，同时是"远端配置的可读镜像"
	n.persistSnapshot(m)
	return m, nil
}

// persistSnapshot 把当前快照以缩进 JSON 写入 gaia.DefaultRemoteConfigFile
// 失败仅 warn，不影响业务
func (n *NacosConfigCenter) persistSnapshot(m map[string]any) {
	if m == nil {
		return
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		gaia.WarnF("[RemoteConfig][nacos] 序列化快照失败: %v", err)
		return
	}
	if err := gaia.FilePutContent(gaia.DefaultRemoteConfigFile, string(data)); err != nil {
		gaia.WarnF("[RemoteConfig][nacos] 写入快照文件失败[%s]: %v", gaia.DefaultRemoteConfigFile, err)
		return
	}
	gaia.TraceF("[RemoteConfig][nacos] 已写入完整快照: %s (%d top-level keys)", gaia.DefaultRemoteConfigFile, len(m))
}

// loadLocalSnapshot 从本地 remoteConfig.json 加载快照作为冷启动兜底
// 仅在远端首拉失败时调用；返回是否成功加载
func (n *NacosConfigCenter) loadLocalSnapshot() bool {
	raw, err := gaia.ReadFileAll(gaia.DefaultRemoteConfigFile)
	if err != nil {
		return false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return false
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		gaia.WarnF("[RemoteConfig][nacos] 解析本地快照失败: %v", err)
		return false
	}
	n.snapMu.Lock()
	n.snap = m
	n.snapMu.Unlock()
	return true
}

// onPushMap SDK 推送回调（已解析为 map）：DataId 整体内容变化
func (n *NacosConfigCenter) onPushMap(newSnap map[string]any, raw string) {
	// 1) 抓取旧快照用于 diff
	n.snapMu.RLock()
	oldSnap := n.snap
	n.snapMu.RUnlock()

	// 2) 解析失败 → 回退到一次 PullMap
	if newSnap == nil {
		gaia.WarnF("[RemoteConfig][nacos] 推送内容解析失败，回退 PullMap (raw_len=%d)", len(raw))
		if _, err := n.refresh(); err != nil {
			gaia.ErrorF("[RemoteConfig][nacos] 推送刷新失败: %v", err)
			return
		}
		n.snapMu.RLock()
		newSnap = n.snap
		n.snapMu.RUnlock()
	} else {
		n.snapMu.Lock()
		n.snap = newSnap
		n.snapMu.Unlock()
		n.persistSnapshot(newSnap)
	}

	// 3) diff 出所有变更/删除的 dot-path
	changedPaths := diffPaths(oldSnap, newSnap)

	// 4) 失效 BaseCenter 内存缓存（无法预知具体 path 是否曾被读过，直接全清）
	n.BaseCenter.InvalidateAllCache()

	// 5) 失效 gaia.GetConf 的 L1 缓存
	gaia.InvalidateConfCacheBatch(changedPaths)
	gaia.InvalidateAllRequestedRemoteConfCache()

	// 6) 触发匹配的 key 级 watcher
	n.dispatchKeyWatchers(changedPaths, newSnap)

	gaia.InfoF("[RemoteConfig][nacos] 收到 DataId 变更推送，已刷新快照并失效缓存（changed=%d）", len(changedPaths))
}

// dispatchKeyWatchers 按 changedPaths 触发匹配的 key 级 callback
//
// 匹配规则：watcher 注册的 key 与 changedPath 满足前缀关系即触发：
//   - watcher.key == changedPath
//   - watcher.key 是 changedPath 的祖先（注册了 "Server"，叶子 "Server.Port" 变更也触发）
//   - changedPath 是 watcher.key 的祖先（注册了 "Server.Port"，整段 "Server" 变化也触发）
func (n *NacosConfigCenter) dispatchKeyWatchers(changedPaths []string, newSnap map[string]any) {
	if len(changedPaths) == 0 {
		return
	}
	n.watchersMu.Lock()
	type firing struct {
		callbacks []func(any)
		val       any
	}
	var fires []firing
	for key, cbs := range n.watchers {
		if !keyMatchesAnyChanged(key, changedPaths) {
			continue
		}
		val, _, _ := lookup(newSnap, key)
		// 复制一份 callbacks 切片，避免持锁回调时阻塞 / 被并发修改
		cbsCopy := make([]func(any), len(cbs))
		copy(cbsCopy, cbs)
		fires = append(fires, firing{callbacks: cbsCopy, val: val})
	}
	n.watchersMu.Unlock()

	for _, f := range fires {
		val := f.val
		for _, cb := range f.callbacks {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						gaia.ErrorF("[RemoteConfig][nacos] watcher callback panic: %v", r)
					}
				}()
				cb(val)
			}()
		}
	}
}

// keyMatchesAnyChanged 判断 watcher.key 是否与某个 changedPath 处于前缀关系
func keyMatchesAnyChanged(key string, changedPaths []string) bool {
	for _, p := range changedPaths {
		if p == key {
			return true
		}
		if strings.HasPrefix(p, key+".") {
			return true
		}
		if strings.HasPrefix(key, p+".") {
			return true
		}
	}
	return false
}

// Close 释放 SDK client 与监听
func (n *NacosConfigCenter) Close() error {
	_ = n.cli.CancelWatch()
	_ = n.cli.Close()
	return n.BaseCenter.Close()
}

// DumpConfig 返回完整配置快照（ConfigDumper 接口）
func (n *NacosConfigCenter) DumpConfig() (map[string]any, error) {
	n.snapMu.RLock()
	snap := n.snap
	n.snapMu.RUnlock()
	if snap == nil {
		return n.refresh()
	}
	return snap, nil
}

// ListDataIds 返回所有订阅的 DataId（供配置管理面使用）
func (n *NacosConfigCenter) ListDataIds() []string {
	return n.cli.DataIds()
}

// PullRawByDataId 拉取指定 DataId 的原始配置内容
func (n *NacosConfigCenter) PullRawByDataId(dataId string) (string, error) {
	return n.cli.GetConfigByDataId(dataId)
}

// Probe 实现 remoteConfig.Prober：触发一次首个 DataId 的远程拉取以验证可达 + 鉴权
//
//   - 仅在 ctx 给定的 timeout 内尝试，超时即视为探活失败
//   - 只检测连接性 + 鉴权，不依赖业务 key；DataId 不存在不视为故障
func (n *NacosConfigCenter) Probe(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("nacos probe panic: %v", r)
			}
		}()
		_, err := n.cli.PullRaw()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("nacos 探活失败: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ================================ map 字段读取辅助 ================================

func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	v, ok := m[key]
	if !ok {
		return map[string]any{}
	}
	if mm, ok := v.(map[string]any); ok {
		return mm
	}
	return map[string]any{}
}

// getString 从 map 中读取 string 字段
func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// getStringSlice 既支持 yaml 数组（[]any of string），也支持逗号分隔字符串
func getStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case string:
		return splitAndTrim(x)
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		return x
	}
	return nil
}

// toInt64 将 map 字段转为 int64；支持 int/int64/float64/string
func toInt64(v any, fallback int64) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case string:
		var r int64
		if _, err := fmt.Sscanf(x, "%d", &r); err == nil {
			return r
		}
	}
	return fallback
}

// splitAndTrim 按逗号分割并 trim 空白
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// lookup 在 map 中按 dot-path 取值
func lookup(m map[string]any, key string) (any, bool, error) {
	if m == nil {
		return nil, false, nil
	}
	val, err := dic.GetValueByMapPath(m, key)
	if err != nil {
		return nil, false, nil
	}
	return val, val != nil, nil
}

// ================================ 快照 diff 辅助（供 Nacos / ConfigMap 复用） ================================

// diffPaths 计算两份嵌套 map 之间所有"叶子级别"且发生变更/删除的 dot-path 集合
// 包含三类变更：值修改 / 新增 / 删除
func diffPaths(oldM, newM map[string]any) []string {
	out := map[string]struct{}{}
	collectDiff("", oldM, newM, out)
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	return keys
}

func collectDiff(prefix string, oldV, newV any, out map[string]struct{}) {
	if jsonEqual(oldV, newV) {
		return
	}
	oldMap, oldIsMap := oldV.(map[string]any)
	newMap, newIsMap := newV.(map[string]any)

	// 双方都不是 map：到达"叶子"，记录 path
	if !oldIsMap && !newIsMap {
		if prefix != "" {
			out[prefix] = struct{}{}
		}
		return
	}
	// 一边是 map、一边不是：整段都算变更
	if oldIsMap != newIsMap {
		if prefix != "" {
			out[prefix] = struct{}{}
		}
		if oldIsMap {
			collectAllPaths(prefix, oldMap, out)
		}
		if newIsMap {
			collectAllPaths(prefix, newMap, out)
		}
		return
	}
	// 双方都是 map：求并集 key 递归
	keySet := map[string]struct{}{}
	for k := range oldMap {
		keySet[k] = struct{}{}
	}
	for k := range newMap {
		keySet[k] = struct{}{}
	}
	for k := range keySet {
		next := k
		if prefix != "" {
			next = prefix + "." + k
		}
		collectDiff(next, oldMap[k], newMap[k], out)
	}
}

func collectAllPaths(prefix string, m map[string]any, out map[string]struct{}) {
	for k, v := range m {
		next := k
		if prefix != "" {
			next = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			collectAllPaths(next, sub, out)
			continue
		}
		out[next] = struct{}{}
	}
}

func jsonEqual(a, b any) bool {
	aJ, aE := json.Marshal(a)
	bJ, bE := json.Marshal(b)
	if aE != nil || bE != nil {
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
	return string(aJ) == string(bJ)
}
