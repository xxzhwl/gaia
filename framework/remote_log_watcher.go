// Package framework 远程日志状态 watcher
// 根据 Elasticsearch 配置可用性动态开关远程日志推送。
// @author gaia-framework
// @created 2026-04-20
package framework

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

// 默认轮询间隔；可通过配置 Logger.RemoteWatchInterval（单位：秒）覆盖
const defaultRemoteLogWatchInterval = 30 * time.Second

var (
	remoteLogWatcherOnce    sync.Once
	remoteLogWatcherStarted atomic.Bool
	remoteLogWatcherStopCh  = make(chan struct{})
)

// syncRemoteLogByESConfig 根据当前 ES 真实可用性同步远程日志开关。
// 返回当前状态（true = 已启用）。
//
// 判定逻辑：
//  1. Logger.DisableRemote=true：永久禁用（硬性总闸）
//  2. ES 未配置或探活失败：禁用
//  3. ES 已配置且可达：启用
func syncRemoteLogByESConfig() bool {
	// 用户硬性禁用优先
	if gaia.GetSafeConfBool("Logger.DisableRemote") {
		logImpl.SetRemoteLogEnabled(false)
		return false
	}
	// 配置缺失直接禁用
	if gaia.GetSafeConfString("Framework.ES.Address") == "" {
		logImpl.SetRemoteLogEnabled(false)
		return false
	}
	// 已配置：做一次带超时的 probe，避免 ES 配置错误时写入雪崩
	probeTimeout := time.Duration(
		gaia.GetSafeConfInt64WithDefault("Gaia.ProbeTimeout", int64(defaultProbeTimeout/time.Second)),
	) * time.Second
	if probeTimeout <= 0 {
		probeTimeout = defaultProbeTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	if err := probeES(ctx); err != nil {
		logImpl.SetRemoteLogEnabled(false)
		return false
	}
	logImpl.SetRemoteLogEnabled(true)
	return true
}

// startRemoteLogWatcher 启动一个后台 goroutine，周期性地根据 ES 配置可用性
// 动态开关远程日志推送。
//   - ES 配置从无到有：自动启用
//   - ES 配置从有到无：自动禁用（避免日志雪崩）
//   - 用户配置 Logger.DisableRemote=true：始终禁用（优先级最高）
//
// 仅在 Init 中调用一次；重复调用是安全的（sync.Once 保护）。
func startRemoteLogWatcher() {
	remoteLogWatcherOnce.Do(func() {
		interval := time.Duration(
			gaia.GetSafeConfInt64WithDefault("Logger.RemoteWatchInterval", int64(defaultRemoteLogWatchInterval/time.Second)),
		) * time.Second
		interval = max(interval, 5*time.Second)

		remoteLogWatcherStarted.Store(true)
		go func() {
			defer gaia.CatchPanic()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					syncRemoteLogByESConfig()
				case <-remoteLogWatcherStopCh:
					return
				}
			}
		}()
		gaia.InfoF("[Logger] 远程日志状态 watcher 已启动，轮询间隔: %s", interval)
	})
}

// StopRemoteLogWatcher 停止 watcher（用于优雅关闭）。
// 重复调用是安全的：若 watcher 未启动或已停止，则无操作。
func StopRemoteLogWatcher() {
	if !remoteLogWatcherStarted.CompareAndSwap(true, false) {
		return
	}
	close(remoteLogWatcherStopCh)
}
