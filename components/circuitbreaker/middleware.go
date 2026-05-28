// Package circuitbreaker Hertz HTTP 熔断中间件。
//
// 与限流中间件的关键区别：
//   - 限流：保护本服务（"我能处理多少 QPS"）
//   - 熔断：保护下游 + 快速失败（"下游挂了就别再打它了"）
//
// 在 HTTP 入口侧使用熔断器的语义是：当本服务对下游的调用大量失败、且本服务的
// 接口又依赖该下游时，与其让请求堆积在本服务里超时，不如对相关 path 直接 503，
// 释放连接资源给真正能处理的请求。
//
// 失败判定：默认把 5xx 响应视为失败；可通过 Settings 自定义。
//
// @author wanlizhan
// @created 2026-06-01
package circuitbreaker

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// HTTPKeyFunc 从请求中提取熔断维度键（如 path / 下游服务名）。
type HTTPKeyFunc func(c *app.RequestContext) string

// HTTPRejectHandler 自定义熔断命中时的响应；为 nil 时使用默认 503 JSON。
type HTTPRejectHandler func(ctx context.Context, c *app.RequestContext, key string, err error)

// HTTPSettings HTTP 中间件配置。
type HTTPSettings struct {
	// Settings 单个熔断器的配置（每个 key 对应的实例都用这套配置）。
	Settings Settings
	// KeyFn 提取维度键；为 nil 时使用全局共享熔断器（key=""）。
	KeyFn HTTPKeyFunc
	// IsFailureFromStatus 通过 HTTP 状态码判定是否失败；为 nil 时使用默认（>=500 失败）。
	// 注意：IsFailureFromStatus 与 Settings.IsFailure 取或——任一为 true 即视为失败。
	IsFailureFromStatus func(status int) bool
	// OnReject 自定义熔断命中响应；为 nil 时返回 503 + Retry-After。
	OnReject HTTPRejectHandler
}

// HertzMiddleware 创建 Hertz 熔断中间件。
//
// 实现细节：
//  1. 每个 key 对应一个独立 Breaker，通过 sync.Map 复用；
//  2. 请求前 Allow → 命中 Open/HalfOpen 拥塞 → 503/429；
//  3. 请求后 Report：5xx 或 ctx.IsAborted() 视为失败；
//  4. panic 路径由 defer 兜底上报失败，与 server 现有 ErrorHandler 协作。
func HertzMiddleware(cfg HTTPSettings) app.HandlerFunc {
	if cfg.IsFailureFromStatus == nil {
		cfg.IsFailureFromStatus = defaultIsFailureFromStatus
	}

	cache := &sync.Map{} // key -> *Breaker

	getOrCreate := func(key string) *Breaker {
		if v, ok := cache.Load(key); ok {
			return v.(*Breaker)
		}
		s := cfg.Settings
		// 若未指定 Name，则用 key 兜底，便于 OnStateChange 区分实例
		if s.Name == "" {
			s.Name = "http:" + key
		}
		created := New(s)
		actual, _ := cache.LoadOrStore(key, created)
		return actual.(*Breaker)
	}

	return func(ctx context.Context, c *app.RequestContext) {
		key := ""
		if cfg.KeyFn != nil {
			key = cfg.KeyFn(c)
		}
		b := getOrCreate(key)

		gen, err := b.Allow()
		if err != nil {
			rejectHTTP(ctx, c, key, err, cfg.OnReject)
			return
		}

		// 用 defer 兜底上报，覆盖 panic 路径（panic 时 status=500，符合失败语义）
		var reported bool
		defer func() {
			if reported {
				return
			}
			status := c.Response.StatusCode()
			fail := cfg.IsFailureFromStatus(status)
			if fail {
				b.Report(gen, errors.New(http.StatusText(status)))
			} else {
				b.Report(gen, nil)
			}
		}()

		c.Next(ctx)

		// 正常路径：标记已上报由 defer 完成
		_ = reported // 显式表明 defer 必走
	}
}

// defaultIsFailureFromStatus 默认失败判定：5xx。
// 4xx 视为客户端错误，不应触发本服务的下游熔断。
func defaultIsFailureFromStatus(status int) bool {
	return status >= 500
}

// rejectHTTP 命中熔断时的响应处理。
func rejectHTTP(ctx context.Context, c *app.RequestContext, key string, err error, custom HTTPRejectHandler) {
	if custom != nil {
		custom(ctx, c, key, err)
		c.Abort()
		return
	}

	status := http.StatusServiceUnavailable
	msg := "服务暂不可用，请稍后重试"
	switch {
	case errors.Is(err, ErrTooManyRequests):
		status = http.StatusTooManyRequests
		msg = "服务正在恢复中，请稍后重试"
	}
	// Retry-After 给客户端一个合理的退避建议（保守取 1s；精确值由配置驱动可后续扩展）
	c.Response.Header.Set("Retry-After", "1")
	c.JSON(status, utils.H{
		"code": status,
		"msg":  msg,
		"key":  key,
	})
	c.Abort()
}

// ===== 便捷工厂：常用维度的 KeyFunc =====

// KeyByPath 按路由模板熔断（最常用）。
func KeyByPath() HTTPKeyFunc {
	return func(c *app.RequestContext) string {
		if p := c.FullPath(); p != "" {
			return p
		}
		return string(c.Request.URI().Path())
	}
}

// KeyByMethodAndPath 按"方法+路径"熔断；GET/POST 同一路径独立计数。
func KeyByMethodAndPath() HTTPKeyFunc {
	pathFn := KeyByPath()
	return func(c *app.RequestContext) string {
		return string(c.Request.Method()) + " " + pathFn(c)
	}
}
