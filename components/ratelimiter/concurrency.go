// Package ratelimiter 并发节流器（Concurrency Limiter）。
//
// 区别于令牌桶 / 滑动窗口（按"单位时间内的请求数"限流），
// 并发节流器按"同时在执行的任务数"进行流量治理，常用于：
//   - 限制下游调用的并发度，避免压垮被依赖方
//   - 限制 goroutine 总数，防止资源耗尽
//   - 批量处理任务时控制并发
//
// 典型用法：
//
//	cl := ratelimiter.NewConcurrencyLimiter(10) // 最多 10 个并发
//
//	// 方式 1：手动 Acquire / Release（推荐用 defer）
//	if err := cl.Acquire(ctx); err != nil {
//	    return err
//	}
//	defer cl.Release()
//	doWork()
//
//	// 方式 2：Do 包装函数调用（推荐）
//	err := cl.Do(ctx, func() error { return doWork() })
//
//	// 方式 3：TryAcquire 非阻塞获取
//	if cl.TryAcquire() {
//	    defer cl.Release()
//	    doWork()
//	}
//
// @author wanlizhan
// @created 2026-05-28
package ratelimiter

import (
	"context"
	"errors"
	"sync/atomic"
)

// ErrConcurrencyLimitExceeded 非阻塞模式下并发数已达上限
var ErrConcurrencyLimitExceeded = errors.New("ratelimiter: 并发数已达上限")

// ConcurrencyLimiter 并发节流器（基于带缓冲 channel 的信号量实现）
//
// 并发安全：所有方法可在多 goroutine 中并发使用。
type ConcurrencyLimiter struct {
	sem     chan struct{}
	max     int
	running int64 // 当前正在执行的任务数
}

// NewConcurrencyLimiter 创建并发节流器
//   - max: 允许同时执行的最大任务数，必须 > 0
//
// 当 max <= 0 时，会被纠正为 1，避免 panic。
func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 {
		max = 1
	}
	return &ConcurrencyLimiter{
		sem: make(chan struct{}, max),
		max: max,
	}
}

// Acquire 阻塞式获取一个并发名额，直到拿到为止或 ctx 被取消
//
// 拿到后必须调用 Release 释放，建议配合 defer。
func (c *ConcurrencyLimiter) Acquire(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case c.sem <- struct{}{}:
		atomic.AddInt64(&c.running, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquire 非阻塞获取一个并发名额
//
// 拿到返回 true，需调用 Release 释放；
// 名额已满返回 false，不会修改状态。
func (c *ConcurrencyLimiter) TryAcquire() bool {
	select {
	case c.sem <- struct{}{}:
		atomic.AddInt64(&c.running, 1)
		return true
	default:
		return false
	}
}

// Release 释放一个并发名额
//
// 多次释放会 panic（与 sync.Mutex.Unlock 行为一致），调用方须保证 Acquire/Release 配对。
func (c *ConcurrencyLimiter) Release() {
	select {
	case <-c.sem:
		atomic.AddInt64(&c.running, -1)
	default:
		panic("ratelimiter: ConcurrencyLimiter.Release 调用次数超过 Acquire")
	}
}

// Do 在并发约束下执行 fn
//
// 内部自动处理 Acquire/Release，是最安全的使用方式。
// 若 ctx 在等待名额时被取消，立即返回 ctx.Err()，fn 不会被执行。
func (c *ConcurrencyLimiter) Do(ctx context.Context, fn func() error) error {
	if fn == nil {
		return nil
	}
	if err := c.Acquire(ctx); err != nil {
		return err
	}
	defer c.Release()
	return fn()
}

// Running 当前正在执行的任务数（瞬时值，仅供观测）
func (c *ConcurrencyLimiter) Running() int {
	return int(atomic.LoadInt64(&c.running))
}

// Max 配置的最大并发数
func (c *ConcurrencyLimiter) Max() int {
	return c.max
}

// Available 当前剩余可用名额（瞬时值，仅供观测）
func (c *ConcurrencyLimiter) Available() int {
	return c.max - c.Running()
}
