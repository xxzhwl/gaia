// Package ratelimiter Hertz 限流中间件。
//
// 提供开箱即用的 Hertz 中间件，支持：
//   - 任意 Limiter 实现（本地令牌桶 / Redis 分布式 / 多维度组合）
//   - 自定义 Key 提取函数（按 IP / UserID / Path / 任意自定义维度）
//   - 自定义拒绝响应（默认返回 429 + Retry-After）
//   - 按 key 隔离时支持 **闲置自动清理**，避免 key 基数膨胀导致内存泄漏
//
// 典型用法：
//
//	// 1. 全局限流：所有请求共享一个限流器
//	limiter := ratelimiter.NewLocalLimiter(1000, 200)
//	h.Use(ratelimiter.HertzMiddleware(limiter))
//
//	// 2. 按 IP 限流：每个 IP 独立的限流器
//	h.Use(ratelimiter.HertzMiddlewareByKey(
//	    func(c *app.RequestContext) string { return c.ClientIP() },
//	    func(key string) ratelimiter.Limiter {
//	        return ratelimiter.NewLocalLimiter(10, 5)
//	    },
//	))
//
//	// 3. 按用户 ID + Redis 分布式限流
//	cli := redis.NewFrameworkClient()
//	h.Use(ratelimiter.HertzMiddlewareByKey(
//	    func(c *app.RequestContext) string { return c.GetString("userID") },
//	    func(key string) ratelimiter.Limiter {
//	        return ratelimiter.NewRedisLimiter(cli, "user:"+key, 100, time.Minute)
//	    },
//	))
//
//	// 4. 按 IP+Path 限流，并启用闲置 30 分钟自动清理（防内存膨胀）
//	mw, stop := ratelimiter.HertzMiddlewareByKeyWithCleanup(
//	    ratelimiter.KeyByIPAndPath(),
//	    func(_ string) ratelimiter.Limiter { return ratelimiter.NewLocalLimiter(50, 100) },
//	    ratelimiter.MiddlewareOption{IdleTTL: 30 * time.Minute, CleanupInterval: 10 * time.Minute},
//	)
//	defer stop()
//	h.Use(mw)
//
// @author wanlizhan
// @created 2026-05-28
package ratelimiter

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
)

// KeyFunc 从请求中提取限流键
type KeyFunc func(c *app.RequestContext) string

// LimiterFactory 根据 key 创建（或返回已有的）限流器
type LimiterFactory func(key string) Limiter

// RejectHandler 自定义命中限流时的响应处理；为 nil 时使用默认 429 JSON
type RejectHandler func(ctx context.Context, c *app.RequestContext, key string)

// MiddlewareOption 中间件可选配置
type MiddlewareOption struct {
	// RetryAfter 命中限流时返回的 Retry-After 头（秒），<=0 时不设置
	RetryAfter int
	// OnReject  自定义拒绝处理；为 nil 时使用默认响应
	OnReject RejectHandler
	// OnError   限流器内部错误的处理；为 nil 时默认放行（fail-open，避免拖垮业务）
	OnError func(ctx context.Context, c *app.RequestContext, err error)

	// IdleTTL  仅 HertzMiddlewareByKey 生效：某 key 在该时长内无请求则被清理。
	// <=0 表示不启用清理（保持原有"永不回收"行为，向后兼容）。
	IdleTTL time.Duration
	// CleanupInterval  清理协程的轮询间隔；<=0 时取 IdleTTL/3（最小 1 分钟）。
	// 仅在 IdleTTL > 0 时生效。
	CleanupInterval time.Duration
}

// HertzMiddleware 全局共享一个 Limiter 的中间件
func HertzMiddleware(limiter Limiter, opts ...MiddlewareOption) app.HandlerFunc {
	opt := mergeOption(opts)
	return func(ctx context.Context, c *app.RequestContext) {
		ok, err := limiter.AllowCtx(ctx)
		if err != nil {
			if opt.OnError != nil {
				opt.OnError(ctx, c, err)
			}
			c.Next(ctx)
			return
		}
		if !ok {
			rejectWith(ctx, c, "", opt)
			return
		}
		c.Next(ctx)
	}
}

// HertzMiddlewareByKey 按 key 隔离的中间件
//
//   - keyFn:     从请求中提取限流键（如 IP / UserID / Path）；返回空字符串视为"全局"
//   - factory:   根据 key 构造该维度的 Limiter（首次调用时创建并缓存）
//
// 内部维护一个 sync.Map 缓存 key -> Limiter，避免每次请求都创建新限流器。
//
// 闲置清理：当 opts[0].IdleTTL > 0 时，会启动一个 daemon 清理协程，定期回收
// 超过 IdleTTL 未访问的 key。该协程随进程结束而退出；如需显式停止，请改用
// HertzMiddlewareByKeyWithCleanup。
//
// 注意：若 key 基数巨大（如纯随机），未启用 IdleTTL 时会导致内存增长，业务方应
// 自行保证 key 收敛或开启 IdleTTL。
func HertzMiddlewareByKey(keyFn KeyFunc, factory LimiterFactory, opts ...MiddlewareOption) app.HandlerFunc {
	mw, _ := buildKeyedMiddleware(keyFn, factory, mergeOption(opts), false)
	return mw
}

// HertzMiddlewareByKeyWithCleanup 与 HertzMiddlewareByKey 行为一致，
// 额外返回一个 stop 函数用于显式停止后台清理协程（多次调用安全）。
//
// 当 IdleTTL <= 0 时，stop 为一个 no-op，但仍可安全调用。
func HertzMiddlewareByKeyWithCleanup(keyFn KeyFunc, factory LimiterFactory, opts ...MiddlewareOption) (app.HandlerFunc, func()) {
	return buildKeyedMiddleware(keyFn, factory, mergeOption(opts), true)
}

// keyedEntry 缓存条目：包装 Limiter 并维护最近访问时间（仅 IdleTTL>0 时使用）
type keyedEntry struct {
	limiter        Limiter
	lastAccessNano atomic.Int64 // UnixNano；0 视为未初始化
}

func (e *keyedEntry) touch(now time.Time) {
	e.lastAccessNano.Store(now.UnixNano())
}

// buildKeyedMiddleware 内部统一实现，returnStop 决定是否将 stop 暴露给调用方。
func buildKeyedMiddleware(keyFn KeyFunc, factory LimiterFactory, opt MiddlewareOption, returnStop bool) (app.HandlerFunc, func()) {
	if keyFn == nil || factory == nil {
		panic("ratelimiter: HertzMiddlewareByKey 的 keyFn 和 factory 不能为 nil")
	}

	cache := &sync.Map{}
	enableCleanup := opt.IdleTTL > 0

	// 仅当启用清理时启动后台 goroutine
	stopCh := make(chan struct{})
	stopOnce := sync.Once{}
	stopFn := func() {
		stopOnce.Do(func() { close(stopCh) })
	}
	if !enableCleanup {
		// 没有 goroutine 也提供一个无害的 stop
		stopFn = func() {}
	} else {
		interval := opt.CleanupInterval
		if interval <= 0 {
			interval = opt.IdleTTL / 3
			if interval < time.Minute {
				interval = time.Minute
			}
		}
		go cleanupLoop(cache, opt.IdleTTL, interval, stopCh)
	}

	mw := func(ctx context.Context, c *app.RequestContext) {
		key := keyFn(c)
		entry := getOrCreateEntry(cache, key, factory)
		if enableCleanup {
			entry.touch(time.Now())
		}

		ok, err := entry.limiter.AllowCtx(ctx)
		if err != nil {
			if opt.OnError != nil {
				opt.OnError(ctx, c, err)
			}
			c.Next(ctx)
			return
		}
		if !ok {
			rejectWith(ctx, c, key, opt)
			return
		}
		c.Next(ctx)
	}

	if !returnStop {
		// 即使内部启动了清理 goroutine，也返回一个 no-op 的 stop（不暴露给外部）。
		// 这是为了保持 HertzMiddlewareByKey 的旧签名不变。
		return mw, func() {}
	}
	return mw, stopFn
}

func mergeOption(opts []MiddlewareOption) MiddlewareOption {
	var opt MiddlewareOption
	if len(opts) > 0 {
		opt = opts[0]
	}
	return opt
}

func getOrCreateEntry(cache *sync.Map, key string, factory LimiterFactory) *keyedEntry {
	if v, ok := cache.Load(key); ok {
		return v.(*keyedEntry)
	}
	created := &keyedEntry{limiter: factory(key)}
	created.lastAccessNano.Store(time.Now().UnixNano())
	actual, _ := cache.LoadOrStore(key, created)
	return actual.(*keyedEntry)
}

// cleanupLoop 周期性清理超过 idleTTL 未访问的 entry
func cleanupLoop(cache *sync.Map, idleTTL, interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case now := <-ticker.C:
			cleanupOnce(cache, idleTTL, now)
		}
	}
}

// cleanupOnce 抽出供测试直接调用
func cleanupOnce(cache *sync.Map, idleTTL time.Duration, now time.Time) (removed int) {
	threshold := now.Add(-idleTTL).UnixNano()
	cache.Range(func(k, v any) bool {
		entry, ok := v.(*keyedEntry)
		if !ok {
			cache.Delete(k)
			removed++
			return true
		}
		if entry.lastAccessNano.Load() < threshold {
			// 二次确认：CAS 风格删除——只删原值，避免误删并发新建条目
			cache.CompareAndDelete(k, v)
			removed++
		}
		return true
	})
	return removed
}

func rejectWith(ctx context.Context, c *app.RequestContext, key string, opt MiddlewareOption) {
	if opt.OnReject != nil {
		opt.OnReject(ctx, c, key)
		c.Abort()
		return
	}
	if opt.RetryAfter > 0 {
		c.Response.Header.Set("Retry-After", strconv.Itoa(opt.RetryAfter))
	}
	c.JSON(http.StatusTooManyRequests, utils.H{
		"code": http.StatusTooManyRequests,
		"msg":  "请求过于频繁，请稍后再试",
	})
	c.Abort()
}

// ===== 便捷工厂：常用维度的 KeyFunc =====

// KeyByIP 按客户端 IP 限流
func KeyByIP() KeyFunc {
	return func(c *app.RequestContext) string {
		return c.ClientIP()
	}
}

// KeyByPath 按请求路径限流（优先 FullPath 路由模板，未命中路由时退化为 URI 主体）
func KeyByPath() KeyFunc {
	return func(c *app.RequestContext) string {
		if p := string(c.FullPath()); p != "" {
			return p
		}
		uri := string(c.Request.RequestURI())
		// 去掉 query 防止恶意构造突破限流
		for i := 0; i < len(uri); i++ {
			if uri[i] == '?' {
				return uri[:i]
			}
		}
		return uri
	}
}

// KeyByIPAndPath  按 IP + 路径组合限流
func KeyByIPAndPath() KeyFunc {
	pathFn := KeyByPath()
	return func(c *app.RequestContext) string {
		return c.ClientIP() + "|" + pathFn(c)
	}
}

// KeyByHeader 按指定请求头取值限流（如 X-User-ID、X-API-Key）
func KeyByHeader(header string) KeyFunc {
	return func(c *app.RequestContext) string {
		return string(c.Request.Header.Peek(header))
	}
}
