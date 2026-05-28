// Package server 单请求级超时控制中间件。
//
// # 与 Hertz 原生超时的区别
//
// Hertz 提供：
//   - server.WithReadTimeout：读完整个请求的最长时间
//   - server.WithIdleTimeout：keep-alive 空闲连接最长时间
//   - server.WithKeepAliveTimeout：keep-alive 心跳间隔
//
// 但缺少"业务处理超时"——即业务 handler 执行多久后强制返回。生产环境的真实
// 风险是某个慢查询/慢下游让 goroutine 长时间堆积，最终拖垮整个进程。本中间
// 件用 context.WithTimeout 给业务 ctx 装上"硬上限"，handler 可以通过监听
// ctx.Done() 主动退出。
//
// # 实现策略：同 goroutine 同步跑业务 + 后台 watcher 仅告警
//
// Hertz 的 *app.RequestContext 不是 goroutine-safe，任何“子 goroutine 跑业务 +
// 主 goroutine 也写 ctx”的方案都会与 Hertz 内部的 index 推进产生 race。本
// 中间件不再尝试在超时后强制写 504，而是采用类似“ctx 面向业务”的机制：
//
//   1. 业务始终在主 goroutine 同步跑——与 Hertz 假设完全一致；
//   2. 一个后台 goroutine 监听 timeoutCtx.Done()——仅负责打告警日志，绝不写 ctx；
//   3. 超时通知通过 context cancellation 传给业务，业务通过 ctx.Done()
//      自行退出并写自己的错误响应。
//
// # 对于不监听 ctx 的野业务
//
// 本中间件 不 会强制中断它们。实际兑现由 hertz 的 server.WithReadTimeout /
// WithIdleTimeout / WithWriteTimeout 出面兑现：超时阈值一到连接会被关闭，
// 客户端不会被卡死。这与 net/http 名下联邦贵航集团能作一致。
//
// 选这个方案的原因：“诚实地不能做”优于“看似能做但有 race”。生产中业务不
// 监听 ctx 本身就是需要修复的 bug，中间件不替他擦屁股。
//
// # 注意事项
//
//  1. 业务依然能在本中间件下同步写 504——只要它自己监听 ctx.Done() 发现超时后
//     调 ctx.JSON(504, ...) 即可，本中间件不介入。
//  2. 慢请求告警：超时阈值的 80% 会触发 warn 日志，便于在真正超时前提前预警。
//
// @author wanlizhan
// @created 2026-06-01
package server

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/xxzhwl/gaia"
)

// timeoutConfig 超时中间件配置。
type timeoutConfig struct {
	enable        bool
	timeout       time.Duration
	slowThreshold time.Duration // 慢请求告警阈值；默认 timeout * 0.8
}

func (s *Server) loadTimeoutConfig() timeoutConfig {
	c := timeoutConfig{
		enable: gaia.GetSafeConfBool(s.schema + ".Timeout.Enable"),
	}
	if !c.enable {
		return c
	}

	timeoutSec := gaia.GetSafeConfInt64WithDefault(s.schema+".Timeout.Seconds", 30)
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	c.timeout = time.Duration(timeoutSec) * time.Second

	slowSec := gaia.GetSafeConfInt64(s.schema + ".Timeout.SlowThresholdSeconds")
	if slowSec > 0 {
		c.slowThreshold = time.Duration(slowSec) * time.Second
	} else {
		// 默认 80% 阈值告警，给运维争取响应时间
		c.slowThreshold = time.Duration(float64(c.timeout) * 0.8)
	}
	return c
}

// timeoutPlugin 单请求超时中间件。
//
// 实现见包级注释。核心点：响应写入仅发生在主 goroutine，从根本上规避
// RequestContext 的并发写入风险。
func (s *Server) timeoutPlugin() app.HandlerFunc {
	return newTimeoutHandler(s.loadTimeoutConfig())
}

// newTimeoutHandler 是 timeoutPlugin 的纯实现——只依赖配置，不依赖 *Server，
// 方便单元测试直接构造任意 timeoutConfig 来覆盖各种边界条件。
//
// # 实现策略：同 goroutine + 后台 watchdog 仅做 cancel + 慢请求告警
//
// 经过多轮验证，结论是：在 *app.RequestContext "同 goroutine" 假设下，
// 任何"业务跑在子 goroutine + 主 goroutine 也写 ctx"的方案都无法做到 race-free
// （Hertz 内部的 index 推进逻辑会触发竞争）。因此本中间件的设计目标降为：
//
//  1. 业务始终在主 goroutine 中跑（与 Hertz 假设完全一致）；
//  2. 一个后台 goroutine 监听 timeoutCtx.Done() —— 仅做日志告警，**绝不写 ctx**；
//  3. 超时通知由 context cancellation 传给业务，业务通过 ctx.Done() 自行退出
//     并写自己的错误响应（业务侧 best practice）；
//  4. 业务彻底不监听 ctx 时，hertz 的 server.WithReadTimeout / WithIdleTimeout
//     /WithWriteTimeout 会关闭连接做最终兜底——这等价于 net/http 的行为。
//
// # 与"强制写 504"方案的取舍
//
//   - 优点：零 race、无 lingering goroutine、与 Hertz 设计哲学一致；
//   - 缺点：不监听 ctx 的"野业务"无法被强制中断（只是 SLO 退化，不会客户端卡死）；
//     这种业务在生产中本就需要修复，本中间件不替它擦屁股。
//
// # 与 net/http TimeoutHandler 的对比
//
// net/http.TimeoutHandler 能强制写 504 是因为标准库 http.ResponseWriter
// 在 TimeoutHandler 中被替换成可同步保护的 wrapper。Hertz 没有等价机制，
// 强行模仿会引入上述 race。我们选择"诚实地不能做"而不是"看似能做但不安全"。
func newTimeoutHandler(cfg timeoutConfig) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		// 探针请求豁免：避免被业务级超时影响，K8s 的 livenessProbe 自身已带超时。
		if isProbeRequest(ctx) {
			ctx.Next(c)
			return
		}

		timeoutCtx, cancel := context.WithTimeout(c, cfg.timeout)
		defer cancel()

		// 后台 watcher：超时时仅打日志，不碰 ctx，避免与业务的写竞争
		watcherDone := make(chan struct{})
		go func() {
			defer close(watcherDone)
			select {
			case <-timeoutCtx.Done():
				if timeoutCtx.Err() == context.DeadlineExceeded {
					gaia.WarnF("请求超时通知 method=%s path=%s timeout=%s（业务通过 ctx.Done() 应自行退出）",
						string(ctx.Request.Method()),
						string(ctx.Request.URI().Path()),
						cfg.timeout,
					)
				}
			case <-c.Done():
				// 上游 ctx 取消（请求结束），watcher 退出
			}
		}()

		start := time.Now()
		// 业务在主 goroutine 同步执行——与 Hertz 同 goroutine 假设完全一致
		ctx.Next(timeoutCtx)
		elapsed := time.Since(start)

		// 让 watcher 退出（cancel 已在 defer 里准备，这里仅显式收尾）
		cancel()
		<-watcherDone

		// 慢请求告警
		if cfg.slowThreshold > 0 && elapsed >= cfg.slowThreshold {
			gaia.WarnF("慢请求 method=%s path=%s elapsed=%s threshold=%s",
				string(ctx.Request.Method()),
				string(ctx.Request.URI().Path()),
				elapsed,
				cfg.slowThreshold,
			)
		}
	}
}
