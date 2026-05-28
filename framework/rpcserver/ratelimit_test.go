package rpcserver

import (
	"testing"
	"time"
)

// TestRpcRateLimiter_NoRefill rate=0 时令牌桶不补充，消耗完即拒绝。
func TestRpcRateLimiter_NoRefill(t *testing.T) {
	rl := newRpcRateLimiter(3, 0)
	for i := 0; i < 3; i++ {
		if !rl.allow() {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if rl.allow() {
		t.Fatal("令牌耗尽后应拒绝")
	}
}

// TestRpcRateLimiter_Refill 经过时间后按速率补充令牌。
func TestRpcRateLimiter_Refill(t *testing.T) {
	rl := newRpcRateLimiter(1, 100) // 容量1，100 token/s
	if !rl.allow() {
		t.Fatal("首次应放行")
	}
	if rl.allow() {
		t.Fatal("瞬时第二次应被拒绝")
	}
	time.Sleep(30 * time.Millisecond) // 30ms*100 = 3 个令牌
	if !rl.allow() {
		t.Fatal("补充后应放行")
	}
}

// TestRpcRateLimiter_CapacityCeil 令牌不超过容量上限。
func TestRpcRateLimiter_CapacityCeil(t *testing.T) {
	rl := newRpcRateLimiter(2, 1000)
	time.Sleep(20 * time.Millisecond) // 即便补充很多也应被容量截断为 2
	allowed := 0
	for i := 0; i < 5; i++ {
		if rl.allow() {
			allowed++
		}
	}
	if allowed > 3 {
		// 容量2 + 极短时间内的微量补充，放行数应接近 2，远小于 5
		t.Fatalf("放行数 %d 超出预期（容量应限制突发）", allowed)
	}
}

func TestLimiterRegistry_GetReuse(t *testing.T) {
	reg := newLimiterRegistry(10, 5, 1)
	a1 := reg.get("k")
	a2 := reg.get("k")
	if a1 != a2 {
		t.Fatal("同一 key 应复用同一限流器")
	}
	if reg.len() != 1 {
		t.Fatalf("len 应为 1，实际 %d", reg.len())
	}
}

func TestLimiterRegistry_LRUEviction(t *testing.T) {
	reg := newLimiterRegistry(2, 5, 1)
	p1 := reg.get("a")
	reg.get("b")
	reg.get("c") // 超过 maxKeys=2，淘汰最久未用的 "a"
	if reg.len() != 2 {
		t.Fatalf("len 应被限制为 2，实际 %d", reg.len())
	}
	p2 := reg.get("a") // "a" 已被淘汰，重新创建
	if p1 == p2 {
		t.Fatal("被淘汰的 key 再次获取应是新的限流器实例")
	}
}

func TestLimiterRegistry_DefaultMaxKeys(t *testing.T) {
	reg := newLimiterRegistry(0, 5, 1)
	if reg.maxKeys != 4096 {
		t.Fatalf("maxKeys<=0 应回退为 4096，实际 %d", reg.maxKeys)
	}
}
