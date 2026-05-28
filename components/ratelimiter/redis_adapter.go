// Package ratelimiter Redis 分布式限流适配器。
package ratelimiter

// 让 redis 组件中的 RateLimitAllow / FixedWindowAllow 也实现统一的 Limiter 接口，
// 使业务层可以无感切换"本地令牌桶"与"分布式滑动窗口/固定窗口"。
//
// 典型用法：
//
//	import (
//	    "github.com/xxzhwl/gaia/components/ratelimiter"
//	    "github.com/xxzhwl/gaia/components/redis"
//	)
//
//	cli := redis.NewFrameworkClient()
//	rl  := ratelimiter.NewRedisLimiter(cli, "api:/users", 100, time.Second)
//	if ok, _ := rl.AllowCtx(ctx); ok { /* 放行 */ }
//
// @author wanlizhan
// @created 2026-05-28

import (
	"context"
	"time"
)

// RedisLimiterBackend Redis 限流后端必须满足的接口
//
// components/redis.Client 已自然实现该接口（RateLimitAllow / FixedWindowAllow），
// 抽象出该接口仅是为了避免对 redis 包的强依赖（避免循环引用、便于测试 mock）。
type RedisLimiterBackend interface {
	RateLimitAllow(key string, limit int, window time.Duration) (bool, error)
	FixedWindowAllow(key string, limit int64, window time.Duration) (bool, error)
}

// RedisAlgorithm Redis 限流算法
type RedisAlgorithm int

const (
	// AlgoSlidingWindow 滑动窗口（默认，更平滑，禁止突发）
	AlgoSlidingWindow RedisAlgorithm = iota
	// AlgoFixedWindow 固定窗口（性能略好，可能允许窗口边界突发）
	AlgoFixedWindow
)

// RedisLimiter 基于 Redis 的分布式限流器，实现 Limiter 接口
type RedisLimiter struct {
	backend RedisLimiterBackend
	key     string
	limit   int
	window  time.Duration
	algo    RedisAlgorithm
}

// NewRedisLimiter 使用滑动窗口算法创建分布式限流器
//   - backend: 一般传入 *redis.Client
//   - key:     限流标识（如 "api:/users"、"user:12345"）
//   - limit:   窗口内最大允许请求数
//   - window:  窗口大小
func NewRedisLimiter(backend RedisLimiterBackend, key string, limit int, window time.Duration) *RedisLimiter {
	return &RedisLimiter{
		backend: backend,
		key:     key,
		limit:   limit,
		window:  window,
		algo:    AlgoSlidingWindow,
	}
}

// NewRedisFixedWindowLimiter 使用固定窗口算法创建分布式限流器
func NewRedisFixedWindowLimiter(backend RedisLimiterBackend, key string, limit int, window time.Duration) *RedisLimiter {
	return &RedisLimiter{
		backend: backend,
		key:     key,
		limit:   limit,
		window:  window,
		algo:    AlgoFixedWindow,
	}
}

// AllowCtx 实现 Limiter 接口
//
// ctx 目前未透传到 redis 调用（受底层 Client 接口限制），保留参数仅用于接口兼容；
// 后续 redis.Client 支持 ctx 版本时可顺势升级。
func (r *RedisLimiter) AllowCtx(_ context.Context) (bool, error) {
	switch r.algo {
	case AlgoFixedWindow:
		return r.backend.FixedWindowAllow(r.key, int64(r.limit), r.window)
	default:
		return r.backend.RateLimitAllow(r.key, r.limit, r.window)
	}
}

// Allow 便捷方法（不区分错误，限流或出错均拒绝）
func (r *RedisLimiter) Allow() bool {
	ok, err := r.AllowCtx(context.Background())
	if err != nil {
		return false
	}
	return ok
}

// Key 返回限流键，便于上层做日志/监控
func (r *RedisLimiter) Key() string { return r.key }
