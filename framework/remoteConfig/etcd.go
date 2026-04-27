// Package remoteConfig — ETCD Provider
//
// 配置示例：
//
//	RemoteConfig:
//	  Type: etcd
//	  Etcd:
//	    Endpoints: 127.0.0.1:2379            # 多个逗号分隔
//	    Prefix: /gaia/app                    # 必填，所有 key 的统一前缀
//	    Username: ""
//	    Password: ""
//	    DialTimeout: 5                       # 秒
//	    RequestTimeout: 3                    # 秒
//	    CacheTTL: 300                        # 秒，可选
//	    WatchInterval: 5                     # 秒，可选（兜底轮询，真正监听靠原生 watch）
//
// ETCD 原生 Watch 性能很好，本 Provider 会覆盖 WatchConfig 使用 etcd 原生 watch，
// 不走轮询。
//
// 存储约定：key 对应 `${Prefix}/${key}`；value 为 JSON 编码。
//
// @author wanlizhan
// @created 2026-04-24
package remoteConfig

import (
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/etcd"
)

func init() {
	RegisterProvider("etcd", newEtcdProvider)
}

// EtcdConfigCenter etcd 配置中心实现
type EtcdConfigCenter struct {
	*BaseCenter
	cli *etcd.Client

	// 记录 key -> 原生 watcher 的映射（用于 StopWatch）
	nativeWatchersMu sync.Mutex
	nativeWatchers   map[string]struct{}
}

// NewEtcdConfigCenter 基于已有 etcd.Client 创建 ConfigCenter
func NewEtcdConfigCenter(cli *etcd.Client, opt BaseOption) (*EtcdConfigCenter, error) {
	if cli == nil {
		return nil, fmt.Errorf("etcd client 为空")
	}
	center := &EtcdConfigCenter{
		cli:            cli,
		nativeWatchers: map[string]struct{}{},
	}
	center.BaseCenter = NewBaseCenter(center.fetch, opt)
	return center, nil
}

// newEtcdProvider 工厂函数
func newEtcdProvider(cfg map[string]any) (ConfigCenter, error) {
	cli, err := etcd.NewClientWithSchema("RemoteConfig.Etcd")
	if err != nil {
		return nil, err
	}
	opt := BaseOption{
		CacheTTL:      time.Duration(gaia.GetSafeConfInt64("RemoteConfig.Etcd.CacheTTL")) * time.Second,
		WatchInterval: time.Duration(gaia.GetSafeConfInt64("RemoteConfig.Etcd.WatchInterval")) * time.Second,
	}
	return NewEtcdConfigCenter(cli, opt)
}

// fetch 从 etcd 直接拉取 key 对应值
func (e *EtcdConfigCenter) fetch(key string) (any, bool, error) {
	return e.cli.GetConfig(key)
}

// WatchConfig 覆盖 BaseCenter 的轮询实现：etcd 原生 watch 性能更好、延迟更低
func (e *EtcdConfigCenter) WatchConfig(key string, callback func(any)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	e.nativeWatchersMu.Lock()
	defer e.nativeWatchersMu.Unlock()

	if _, exists := e.nativeWatchers[key]; exists {
		return fmt.Errorf("watcher for key '%s' already exists", key)
	}

	err := e.cli.WatchKey(key, func(val any) {
		// 刷新 BaseCenter 缓存
		e.BaseCenter.writeCache(key, val, val != nil)
		gaia.InfoF("[RemoteConfig][etcd] 配置变更[key=%s]", key)
		go callback(val)
	})
	if err != nil {
		return err
	}
	e.nativeWatchers[key] = struct{}{}
	return nil
}

// StopWatch 停止监听
func (e *EtcdConfigCenter) StopWatch(key string) error {
	e.nativeWatchersMu.Lock()
	defer e.nativeWatchersMu.Unlock()

	if _, ok := e.nativeWatchers[key]; !ok {
		return fmt.Errorf("watcher for key '%s' not found", key)
	}
	e.cli.StopWatch(key)
	delete(e.nativeWatchers, key)
	return nil
}

// Close 覆盖 BaseCenter.Close，额外释放 SDK client
func (e *EtcdConfigCenter) Close() error {
	e.nativeWatchersMu.Lock()
	for k := range e.nativeWatchers {
		e.cli.StopWatch(k)
	}
	e.nativeWatchers = map[string]struct{}{}
	e.nativeWatchersMu.Unlock()

	_ = e.cli.Close()
	return e.BaseCenter.Close()
}
