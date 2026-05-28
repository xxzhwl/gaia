// Package remoteConfig — K8s ConfigMap Provider
//
// 适用场景：
//
//	gaia 服务跑在 K8s（或本地容器）上，配置以 ConfigMap 卷挂载方式注入到 Pod 文件系统。
//	不需要部署任何额外的配置中心组件，本 Provider 直接读取挂载文件 + fsnotify 监听变更。
//
// 配置示例 1（推荐：单文件模式）：
//
//	# K8s ConfigMap 用 subPath 挂载成单个文件，例如 /etc/gaia/config.yaml
//	RemoteConfig:
//	  Type: configmap
//	  ConfigMap:
//	    Path: /etc/gaia/config.yaml
//	    Format: yaml                  # 可选；空则按扩展名判定
//	    DebounceMs: 500               # 可选；多事件去抖窗口，默认 500ms
//	    CacheTTL: 60                  # 可选；BaseCenter 二级缓存 TTL（秒）
//
// 配置示例 2（目录模式：每个 ConfigMap key 是一个独立文件）：
//
//	# K8s ConfigMap 默认挂载成目录，每个 key 一个文件
//	# 此时本 Provider 会把每个文件视为一个顶层配置项：
//	#   - 若文件后缀是 yaml/yml/json → 解析为 map，挂在 key=文件名(去后缀) 下
//	#   - 否则 → 文件全文作为字符串，挂在 key=文件名 下
//	RemoteConfig:
//	  Type: configmap
//	  ConfigMap:
//	    Path: /etc/gaia
//	    DirMode: true                 # 显式声明目录模式；不填时按 Path 是否目录自动判定
//	    DebounceMs: 500
//
// 监听机制：
//
//   - K8s 通过原子 rename 更新 ConfigMap 卷（实际是 ".data" 符号链接切换），
//     fsnotify 会在父目录上收到 Create / Remove 事件；
//     因此本 Provider 始终监听 *父目录*，并对事件做去抖再 reload。
//   - reload 后做新旧快照 diff，触发 key 级 watcher、失效 gaia.GetConf 缓存、写入快照镜像。
//
// @author wanlizhan
// @created 2026-06-05
package remoteConfig

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/xxzhwl/gaia"
)

func init() {
	RegisterProvider("configmap", newConfigMapProvider)
}

// configMapOptions 内部解析后的配置选项
type configMapOptions struct {
	Path       string
	Format     string
	DirMode    bool
	Debounce   time.Duration
	CacheTTL   time.Duration
}

// ConfigMapCenter K8s ConfigMap 配置中心实现
type ConfigMapCenter struct {
	*BaseCenter

	opt configMapOptions

	snap   map[string]any
	snapMu sync.RWMutex

	// key 级 watcher：与 Nacos Provider 一致，diff 出 changedPaths 后按前缀匹配触发
	watchersMu sync.Mutex
	watchers   map[string][]func(any)

	// fsnotify
	watcher  *fsnotify.Watcher
	stopCh   chan struct{}
	stopOnce sync.Once
}

// newConfigMapProvider 工厂函数
func newConfigMapProvider(cfg map[string]any) (ConfigCenter, error) {
	cmCfg := getMap(cfg, "ConfigMap")
	path := strings.TrimSpace(getString(cmCfg, "Path"))
	if path == "" {
		return nil, fmt.Errorf("RemoteConfig.ConfigMap.Path 必填")
	}

	// DirMode：显式优先；否则按 Path 实际类型自动判定
	dirMode := getBool(cmCfg, "DirMode")
	if _, exists := cmCfg["DirMode"]; !exists {
		if info, err := os.Stat(path); err == nil {
			dirMode = info.IsDir()
		}
	}

	debounce := toDuration(cmCfg["DebounceMs"], 0) * time.Millisecond
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	ttl := toDuration(cmCfg["CacheTTL"], 0) * time.Second

	opt := configMapOptions{
		Path:     path,
		Format:   strings.ToLower(strings.TrimSpace(getString(cmCfg, "Format"))),
		DirMode:  dirMode,
		Debounce: debounce,
		CacheTTL: ttl,
	}
	return NewConfigMapCenter(opt)
}

// NewConfigMapCenter 创建 ConfigMap 配置中心
func NewConfigMapCenter(opt configMapOptions) (*ConfigMapCenter, error) {
	c := &ConfigMapCenter{
		opt:      opt,
		watchers: map[string][]func(any){},
		stopCh:   make(chan struct{}),
	}
	c.BaseCenter = NewBaseCenter(c.fetch, BaseOption{CacheTTL: opt.CacheTTL})

	// 标记接管 remoteConfig.json 写入：configmap 也维护完整快照镜像
	gaia.RemoteSnapshotOwned = true

	// 初次加载
	if err := c.reload(); err != nil {
		gaia.WarnF("[RemoteConfig][configmap] 初次加载失败: %v，尝试本地快照兜底", err)
		c.loadLocalSnapshot()
	}

	// 启动 fsnotify 监听
	if err := c.startWatcher(); err != nil {
		gaia.WarnF("[RemoteConfig][configmap] 启动 fsnotify 失败，将退化为只读模式: %v", err)
	}

	// 启动期信息日志：方便排障与多环境核对
	gaia.InfoF("[RemoteConfig][configmap] 已就绪 Path=%q DirMode=%v Format=%q DebounceMs=%d",
		c.opt.Path, c.opt.DirMode, c.opt.Format, c.opt.Debounce/time.Millisecond)
	return c, nil
}

// fetch 读取 key —— 优先内存快照；快照为空时尝试 reload
func (c *ConfigMapCenter) fetch(key string) (any, bool, error) {
	c.snapMu.RLock()
	snap := c.snap
	c.snapMu.RUnlock()
	if snap == nil {
		if err := c.reload(); err != nil {
			return nil, false, err
		}
		c.snapMu.RLock()
		snap = c.snap
		c.snapMu.RUnlock()
	}
	return lookup(snap, key)
}

// WatchConfig 注册 key 级监听；机制同 Nacos：diff changedPaths 后前缀匹配触发
func (c *ConfigMapCenter) WatchConfig(key string, callback func(any)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	c.watchersMu.Lock()
	defer c.watchersMu.Unlock()
	if _, ok := c.watchers[key]; ok {
		return fmt.Errorf("watcher for key '%s' already exists", key)
	}
	c.watchers[key] = []func(any){callback}
	return nil
}

// StopWatch 停止指定 key 的监听
func (c *ConfigMapCenter) StopWatch(key string) error {
	c.watchersMu.Lock()
	defer c.watchersMu.Unlock()
	if _, ok := c.watchers[key]; !ok {
		return fmt.Errorf("watcher for key '%s' not found", key)
	}
	delete(c.watchers, key)
	return nil
}

// DumpConfig 返回完整快照
func (c *ConfigMapCenter) DumpConfig() (map[string]any, error) {
	c.snapMu.RLock()
	snap := c.snap
	c.snapMu.RUnlock()
	if snap == nil {
		if err := c.reload(); err != nil {
			return nil, err
		}
		c.snapMu.RLock()
		snap = c.snap
		c.snapMu.RUnlock()
	}
	return snap, nil
}

// Close 停止监听
func (c *ConfigMapCenter) Close() error {
	c.stopOnce.Do(func() { close(c.stopCh) })
	if c.watcher != nil {
		_ = c.watcher.Close()
	}
	return c.BaseCenter.Close()
}

// Probe 实现 remoteConfig.Prober：检查挂载文件 / 目录可读
func (c *ConfigMapCenter) Probe(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("configmap probe panic: %v", r)
			}
		}()
		info, err := os.Stat(c.opt.Path)
		if err != nil {
			done <- fmt.Errorf("configmap 路径不可访问[%s]: %w", c.opt.Path, err)
			return
		}
		if c.opt.DirMode && !info.IsDir() {
			done <- fmt.Errorf("configmap 配置为目录模式但 Path=%s 不是目录", c.opt.Path)
			return
		}
		if !c.opt.DirMode && info.IsDir() {
			done <- fmt.Errorf("configmap 配置为单文件模式但 Path=%s 是目录", c.opt.Path)
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ----- 加载逻辑 -----

// reload 重新读取 Path，构建新快照并刷新缓存 + 落盘
func (c *ConfigMapCenter) reload() error {
	newSnap, err := c.loadSnapshot()
	if err != nil {
		return err
	}

	c.snapMu.Lock()
	oldSnap := c.snap
	c.snap = newSnap
	c.snapMu.Unlock()

	c.persistSnapshot(newSnap)

	// 启动期 oldSnap 为 nil → 不需要派发 watcher（也没人注册）
	if oldSnap == nil {
		return nil
	}

	changedPaths := diffPaths(oldSnap, newSnap)
	if len(changedPaths) == 0 {
		return nil
	}
	c.BaseCenter.InvalidateAllCache()
	gaia.InvalidateConfCacheBatch(changedPaths)
	gaia.InvalidateAllRequestedRemoteConfCache()
	c.dispatchKeyWatchers(changedPaths, newSnap)
	gaia.InfoF("[RemoteConfig][configmap] 检测到配置变更，已刷新快照（changed=%d）", len(changedPaths))
	return nil
}

// loadSnapshot 按当前模式加载快照
func (c *ConfigMapCenter) loadSnapshot() (map[string]any, error) {
	if c.opt.DirMode {
		return c.loadDirSnapshot()
	}
	return c.loadFileSnapshot(c.opt.Path)
}

// loadFileSnapshot 单文件模式：整文件解析为 map
func (c *ConfigMapCenter) loadFileSnapshot(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败[%s]: %w", path, err)
	}
	format := c.opt.Format
	if format == "" {
		format = guessFormatByExt(path)
	}
	m, err := decodeConfig(raw, format)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件失败[%s]: %w", path, err)
	}
	return m, nil
}

// loadDirSnapshot 目录模式：每个文件 → 顶层 key
//
// 跳过：
//   - 隐藏文件（以 "." 开头），覆盖 K8s 的 "..data" / "..2024_01_01_xxx" 这类内部目录
//   - 子目录（非递归）
func (c *ConfigMapCenter) loadDirSnapshot() (map[string]any, error) {
	entries, err := os.ReadDir(c.opt.Path)
	if err != nil {
		return nil, fmt.Errorf("读取配置目录失败[%s]: %w", c.opt.Path, err)
	}
	snap := map[string]any{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			continue
		}
		full := filepath.Join(c.opt.Path, name)
		raw, err := os.ReadFile(full)
		if err != nil {
			gaia.WarnF("[RemoteConfig][configmap] 读取 %s 失败: %v", full, err)
			continue
		}
		// K8s ConfigMap 卷挂载里有的文件可能是符号链接到 ..data/<key>，已被 os.ReadFile 透明解析
		base := name
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(base, ext)
		switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
		case "yaml", "yml":
			if m, err := decodeConfig(raw, "yaml"); err == nil {
				snap[stem] = m
				continue
			}
		case "json":
			if m, err := decodeConfig(raw, "json"); err == nil {
				snap[stem] = m
				continue
			}
		}
		// 非结构化文件 / 解析失败：保留原文（用文件名整体作为 key，便于按 ConfigMap 原始 key 访问）
		snap[base] = string(raw)
	}
	return snap, nil
}

// persistSnapshot 把快照写到 gaia.DefaultRemoteConfigFile（与 Nacos Provider 一致）
func (c *ConfigMapCenter) persistSnapshot(m map[string]any) {
	if m == nil {
		return
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		gaia.WarnF("[RemoteConfig][configmap] 序列化快照失败: %v", err)
		return
	}
	if err := gaia.FilePutContent(gaia.DefaultRemoteConfigFile, string(data)); err != nil {
		gaia.WarnF("[RemoteConfig][configmap] 写入快照文件失败[%s]: %v", gaia.DefaultRemoteConfigFile, err)
		return
	}
	gaia.TraceF("[RemoteConfig][configmap] 已写入完整快照: %s (%d top-level keys)", gaia.DefaultRemoteConfigFile, len(m))
}

// loadLocalSnapshot 启动时如果 Path 不可读，尝试用上次的镜像兜底
func (c *ConfigMapCenter) loadLocalSnapshot() {
	raw, err := gaia.ReadFileAll(gaia.DefaultRemoteConfigFile)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		gaia.WarnF("[RemoteConfig][configmap] 解析本地快照失败: %v", err)
		return
	}
	c.snapMu.Lock()
	c.snap = m
	c.snapMu.Unlock()
	gaia.InfoF("[RemoteConfig][configmap] 已从本地快照文件加载兜底配置: %s", gaia.DefaultRemoteConfigFile)
}

// ----- watcher -----

// startWatcher 启动 fsnotify 监听父目录，对事件做去抖后调用 reload
func (c *ConfigMapCenter) startWatcher() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	c.watcher = w

	// 总是监听父目录：
	//   - 单文件模式：父目录 = filepath.Dir(Path)
	//   - 目录模式：父目录 = Path 本身
	watchDir := c.opt.Path
	if !c.opt.DirMode {
		watchDir = filepath.Dir(c.opt.Path)
	}
	if err := w.Add(watchDir); err != nil {
		_ = w.Close()
		c.watcher = nil
		return fmt.Errorf("fsnotify 监听 %s 失败: %w", watchDir, err)
	}

	go c.watchLoop()
	return nil
}

// watchLoop 事件循环：去抖 + reload
func (c *ConfigMapCenter) watchLoop() {
	defer func() {
		if r := recover(); r != nil {
			gaia.ErrorF("[RemoteConfig][configmap] watchLoop panic: %v", r)
		}
	}()

	var (
		timer  *time.Timer
		timerC <-chan time.Time
	)
	scheduleReload := func() {
		if timer == nil {
			timer = time.NewTimer(c.opt.Debounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(c.opt.Debounce)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-c.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return
		case ev, ok := <-c.watcher.Events:
			if !ok {
				return
			}
			if !c.shouldReact(ev) {
				continue
			}
			scheduleReload()
		case err, ok := <-c.watcher.Errors:
			if !ok {
				return
			}
			gaia.WarnF("[RemoteConfig][configmap] fsnotify error: %v", err)
		case <-timerC:
			timerC = nil
			if err := c.reload(); err != nil {
				gaia.WarnF("[RemoteConfig][configmap] reload 失败: %v", err)
			}
		}
	}
}

// shouldReact 过滤无关事件
//
//   - 单文件模式：只关心目标文件本身的 Create / Write / Remove / Rename / Chmod
//     （K8s 通过原子 rename 切换 "..data" 符号链接，最终落到对应文件名上）
//   - 目录模式：忽略隐藏文件，其余事件都触发
func (c *ConfigMapCenter) shouldReact(ev fsnotify.Event) bool {
	base := filepath.Base(ev.Name)
	// 隐藏文件：K8s 的 "..data" / "..YYYY_MM_DD..." 这些都以 "." 开头
	if strings.HasPrefix(base, ".") {
		// K8s 的真正切换发生在 "..data" 上，但用户只关心可见文件最终是否变更，
		// 所以这里直接忽略隐藏文件事件 —— 隐藏文件切换之后会带动可见文件触发新事件。
		// 即使个别 K8s 版本不触发可见文件事件，超时 reload 也能容错（debounce + 启动期一次性 reload）。
		return false
	}
	if c.opt.DirMode {
		return true
	}
	// 单文件模式
	return base == filepath.Base(c.opt.Path)
}

// dispatchKeyWatchers 与 Nacos 实现保持一致
func (c *ConfigMapCenter) dispatchKeyWatchers(changedPaths []string, newSnap map[string]any) {
	if len(changedPaths) == 0 {
		return
	}
	c.watchersMu.Lock()
	type firing struct {
		callbacks []func(any)
		val       any
	}
	var fires []firing
	for key, cbs := range c.watchers {
		if !keyMatchesAnyChanged(key, changedPaths) {
			continue
		}
		val, _, _ := lookup(newSnap, key)
		cbsCopy := make([]func(any), len(cbs))
		copy(cbsCopy, cbs)
		fires = append(fires, firing{callbacks: cbsCopy, val: val})
	}
	c.watchersMu.Unlock()

	for _, f := range fires {
		val := f.val
		for _, cb := range f.callbacks {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						gaia.ErrorF("[RemoteConfig][configmap] watcher callback panic: %v", r)
					}
				}()
				cb(val)
			}()
		}
	}
}

// ================================ 工具函数 ================================

// guessFormatByExt 按文件扩展名猜测格式；未知则按 yaml 兜底
func guessFormatByExt(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	default:
		return "yaml"
	}
}

// decodeConfig 解析 raw 为 map
func decodeConfig(raw []byte, format string) (map[string]any, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	m := map[string]any{}
	switch strings.ToLower(format) {
	case "json":
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("解析 JSON 失败: %w", err)
		}
	case "yaml", "yml":
		if err := yaml.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("解析 YAML 失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("不支持的格式: %s", format)
	}
	return m, nil
}
