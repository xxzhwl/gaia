// Package rpcserver 通用令牌桶限流器及其有界管理器。
//
// gRPC 服务端限流拦截器共用此实现。
//
// 关键设计（修复原实现的内存泄漏）：
//   - 原实现以 method|clientIP 为 key 写入全局 map 且永不清理，
//     高基数客户端 IP 会导致 map 无限增长 → 内存泄漏。
//   - 现改为 limiterRegistry：带容量上限 + LRU 淘汰，key 数量有界。
//
// @author gaia-framework
// @created 2026-04-17
// @refactored 2026-06-24
package rpcserver

import (
	"container/list"
	"sync"
	"time"
)

// rpcRateLimiter 令牌桶限流器。
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
	elapsed := now.Sub(rl.lastRefill).Seconds()
	if elapsed > 0 {
		rl.tokens = min(float64(rl.capacity), rl.tokens+elapsed*rl.rate)
		rl.lastRefill = now
	}

	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}
	return false
}

// limiterRegistry 带 LRU 淘汰的限流器注册表，保证 key 数量有界。
type limiterRegistry struct {
	mu       sync.Mutex
	maxKeys  int
	capacity int
	rate     float64
	ll       *list.List               // 最近使用顺序，front=最新
	items    map[string]*list.Element // key -> 链表节点
}

type limiterEntry struct {
	key string
	rl  *rpcRateLimiter
}

func newLimiterRegistry(maxKeys, capacity int, rate float64) *limiterRegistry {
	if maxKeys <= 0 {
		maxKeys = 4096
	}
	return &limiterRegistry{
		maxKeys:  maxKeys,
		capacity: capacity,
		rate:     rate,
		ll:       list.New(),
		items:    make(map[string]*list.Element),
	}
}

// get 获取（或创建）指定 key 的限流器，并维护 LRU 顺序。
func (r *limiterRegistry) get(key string) *rpcRateLimiter {
	r.mu.Lock()
	defer r.mu.Unlock()

	if el, ok := r.items[key]; ok {
		r.ll.MoveToFront(el)
		return el.Value.(*limiterEntry).rl
	}

	rl := newRpcRateLimiter(r.capacity, r.rate)
	el := r.ll.PushFront(&limiterEntry{key: key, rl: rl})
	r.items[key] = el

	// 超出上限则淘汰最久未使用的
	for r.ll.Len() > r.maxKeys {
		oldest := r.ll.Back()
		if oldest == nil {
			break
		}
		r.ll.Remove(oldest)
		delete(r.items, oldest.Value.(*limiterEntry).key)
	}
	return rl
}

// len 当前持有的 key 数量（仅测试 / 监控用）。
func (r *limiterRegistry) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ll.Len()
}
