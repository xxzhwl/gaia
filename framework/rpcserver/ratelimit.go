// Package rpcserver 通用令牌桶限流器
// gRPC 和 Kitex 的拦截器/中间件共用此限流实现
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"sync"
	"time"
)

// rpcRateLimiter 令牌桶限流器
type rpcRateLimiter struct {
	capacity   int
	rate       float64
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

func newRpcRateLimiter(capacity int, rate float64) *rpcRateLimiter {
	return &rpcRateLimiter{
		capacity:   capacity,
		rate:       rate,
		tokens:     float64(capacity),
		lastRefill: time.Now(),
	}
}

func (rl *rpcRateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	newTokens := now.Sub(rl.lastRefill).Seconds() * rl.rate
	rl.tokens = min(float64(rl.capacity), rl.tokens+newTokens)
	rl.lastRefill = now

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}
