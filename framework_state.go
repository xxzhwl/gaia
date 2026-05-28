// Package gaia 框架初始化状态跟踪（根包级实现）
//
// 本文件提供 framework.Init 是否已执行完成的全局标志与自检能力。
// 标志位放根包而非 framework 包，是因为 framework 下的子包（logImpl/tracer/
// metrics/httpclient 等）都依赖根包；若标志在 framework 包，子包反向 import
// 会形成循环依赖。framework 包通过薄封装（framework.IsInitialized /
// framework.EnsureInitialized）转发到本文件。
//
// 设计目标：让"业务用了 framework 的功能却忘了调 framework.Init()"从
// 静默降级变成显式告警，避免日志/trace/metrics/远程配置/告警在不知不觉中失效。
//
// @author wanlizhan
// @created 2026-06-22
package gaia

import (
	"sync"
	"sync/atomic"
)

// frameworkInitialized framework.Init() 是否已执行完成。
// 用 atomic.Bool 支持热路径无锁读。
var frameworkInitialized atomic.Bool

// frameworkWarnOnceMu 保护 frameworkWarnOnceMap，对每个 callerKey 只 warn 一次。
var (
	frameworkWarnOnceMu  sync.Mutex
	frameworkWarnOnceMap = map[string]struct{}{}
)

// SetFrameworkInitialized 设置 framework 初始化状态。
// 由 framework.Init() 末尾调用（framework.markInitialized 转发到此处）。
// 业务通常无需直接调用；测试场景下可传 false 重置状态。
func SetFrameworkInitialized(v bool) {
	frameworkInitialized.Store(v)
}

// IsFrameworkInitialized 返回 framework.Init 是否已执行完成。
func IsFrameworkInitialized() bool {
	return frameworkInitialized.Load()
}

// EnsureFrameworkInitialized 在"会真正依赖 Init 装配结果"的关键路径上做一次性自检。
//
// 若 Init 未执行：
//   - 每个 callerKey 只 warn 一次（避免热路径刷屏）
//   - 不 panic（兼容只需部分能力的场景，如单独用 server 起调试 HTTP）
//   - 返回 false，调用方可据此决定是否走兜底逻辑
//
// callerKey 建议用 "子包.函数名" 形式（如 "logImpl.PushLog"），便于日志定位。
// 同一个 callerKey 第二次及以后调用直接返回 false 不再打日志。
func EnsureFrameworkInitialized(callerKey string) bool {
	if frameworkInitialized.Load() {
		return true
	}
	frameworkWarnOnceMu.Lock()
	_, warned := frameworkWarnOnceMap[callerKey]
	if !warned {
		frameworkWarnOnceMap[callerKey] = struct{}{}
	}
	frameworkWarnOnceMu.Unlock()
	if !warned {
		WarnF("[framework] %s 被调用但 framework.Init() 尚未执行，"+
			"相关能力（日志/trace/metrics/远程配置/告警）可能已静默降级。"+
			"请在程序入口（main/启动函数最前面）调用 framework.Init()。", callerKey)
	}
	return false
}
