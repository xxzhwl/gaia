// Package ratelimiter 提供本地令牌桶限流器。
//
// Redis 分布式限流已内置在 redis 组件中（Client.RateLimitAllow / Client.FixedWindowAllow），
// 本包专注于进程内限流（无外部依赖）。
//
// 典型用法：
//
//	limiter := ratelimiter.NewLocalLimiter(100, 10) // 100 QPS, burst 10
//	if limiter.Allow() { /* 放行 */ }
//	limiter.Wait(ctx) // 阻塞等待
//
// @author wanlizhan
// @created 2026-04-24
package ratelimiter

import (
	"context"

	"golang.org/x/time/rate"
)

// Limiter 限流器通用接口
type Limiter interface {
	// AllowCtx 是否允许本次请求通过
	AllowCtx(ctx context.Context) (bool, error)
}

// LocalLimiter 基于 golang.org/x/time/rate 的本地令牌桶限流器
type LocalLimiter struct {
	limiter *rate.Limiter
}

// NewLocalLimiter 创建本地令牌桶限流器
//   - ratePerSec: 每秒允许的请求数
//   - burst: 突发容量
func NewLocalLimiter(ratePerSec float64, burst int) *LocalLimiter {
	return &LocalLimiter{
		limiter: rate.NewLimiter(rate.Limit(ratePerSec), burst),
	}
}

// Allow 立即判断是否允许
func (l *LocalLimiter) Allow() bool {
	return l.limiter.Allow()
}

// AllowCtx 实现 Limiter 接口
func (l *LocalLimiter) AllowCtx(_ context.Context) (bool, error) {
	return l.limiter.Allow(), nil
}

// Wait 阻塞等待直到允许或 ctx 过期
func (l *LocalLimiter) Wait(ctx context.Context) error {
	return l.limiter.Wait(ctx)
}

// SetRate 动态调整速率
func (l *LocalLimiter) SetRate(ratePerSec float64) {
	l.limiter.SetLimit(rate.Limit(ratePerSec))
}

// SetBurst 动态调整突发容量
func (l *LocalLimiter) SetBurst(burst int) {
	l.limiter.SetBurst(burst)
}
