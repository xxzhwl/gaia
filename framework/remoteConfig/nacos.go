// Package remoteConfig — Nacos Provider
//
// 配置示例：
//
//	RemoteConfig:
//	  Type: nacos
//	  Nacos:
//	    ServerAddrs: 127.0.0.1:8848           # 多个用逗号分隔
//	    Namespace: ""                          # Nacos 命名空间 ID，默认 public
//	    Group: DEFAULT_GROUP
//	    DataId: gaia-server.yaml
//	    Username: ""
//	    Password: ""
//	    TimeoutMs: 5000
//	    Format: yaml                           # yaml | json
//	    CacheTTL: 300                          # 秒，可选
//	    WatchInterval: 5                       # 秒，可选（仅兜底轮询，真正监听靠 SDK 推送）
//
// Nacos 原生支持 DataId 级别的推送监听。本 Provider 会把整个 DataId 的内容拉下来
// 解析成 map 缓存，配合 BaseCenter 的 cache + diff 能力实现 key 级别变更通知。
//
// @author wanlizhan
// @created 2026-04-24
package remoteConfig

import (
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
}

// NewNacosConfigCenter 直接基于已有 nacos.Client 创建 ConfigCenter
func NewNacosConfigCenter(cli *nacos.Client, opt BaseOption) (*NacosConfigCenter, error) {
	if cli == nil {
		return nil, fmt.Errorf("nacos client 为空")
	}
	center := &NacosConfigCenter{cli: cli}
	center.BaseCenter = NewBaseCenter(center.fetch, opt)

	// 预热一次 + 注册 SDK 推送监听
	if _, err := center.refresh(); err != nil {
		gaia.WarnF("[RemoteConfig][nacos] 初始拉取失败: %v", err)
	}
	if err := cli.WatchConfig(center.onPush); err != nil {
		gaia.WarnF("[RemoteConfig][nacos] 注册 SDK 监听失败: %v", err)
	}
	return center, nil
}

// newNacosProvider 工厂函数
func newNacosProvider(cfg map[string]any) (ConfigCenter, error) {
	cli, err := nacos.NewClientWithSchema("RemoteConfig.Nacos")
	if err != nil {
		return nil, err
	}
	opt := BaseOption{
		CacheTTL:      time.Duration(gaia.GetSafeConfInt64("RemoteConfig.Nacos.CacheTTL")) * time.Second,
		WatchInterval: time.Duration(gaia.GetSafeConfInt64("RemoteConfig.Nacos.WatchInterval")) * time.Second,
	}
	return NewNacosConfigCenter(cli, opt)
}

// ----- 内部方法 -----

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

// refresh 拉取 DataId 全量内容并更新内存快照
func (n *NacosConfigCenter) refresh() (map[string]any, error) {
	m, err := n.cli.PullMap()
	if err != nil {
		return nil, err
	}
	n.snapMu.Lock()
	n.snap = m
	n.snapMu.Unlock()
	return m, nil
}

// onPush SDK 推送回调：DataId 整体内容变化
func (n *NacosConfigCenter) onPush(content string) {
	// 重新拉取并刷新快照（直接 parse content 也可以，这里走统一 pull 保证与 SDK 语义一致）
	if _, err := n.refresh(); err != nil {
		gaia.ErrorF("[RemoteConfig][nacos] 推送刷新失败: %v", err)
		return
	}
	gaia.Info("[RemoteConfig][nacos] 收到 DataId 变更推送，已刷新快照")
	// 注意：BaseCenter 的轮询 watcher 会在下一轮 checkChange 时感知到并触发回调
	_ = content
}

// Close 覆盖 BaseCenter.Close，额外释放 SDK client
func (n *NacosConfigCenter) Close() error {
	_ = n.cli.CancelWatch()
	_ = n.cli.Close()
	return n.BaseCenter.Close()
}

// ================================ 辅助函数 ================================

// getString 从 map 中读取 string 字段
func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
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
		_, err := fmt.Sscanf(x, "%d", &r)
		if err == nil {
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
	val, err := dic.GetValueByMapPath(m, key)
	if err != nil {
		return nil, false, nil
	}
	return val, val != nil, nil
}
