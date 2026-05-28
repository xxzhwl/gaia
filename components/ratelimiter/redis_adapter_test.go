// @author wanlizhan
// @created 2026-05-28
package ratelimiter

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRedisBackend 用于测试的内存 fake，实现 RedisLimiterBackend 接口
type fakeRedisBackend struct {
	slidingCalls int
	fixedCalls   int
	allowSliding bool
	allowFixed   bool
	err          error
	lastKey      string
	lastLimit    int
	lastWindow   time.Duration
}

func (f *fakeRedisBackend) RateLimitAllow(key string, limit int, window time.Duration) (bool, error) {
	f.slidingCalls++
	f.lastKey = key
	f.lastLimit = limit
	f.lastWindow = window
	if f.err != nil {
		return false, f.err
	}
	return f.allowSliding, nil
}

func (f *fakeRedisBackend) FixedWindowAllow(key string, limit int64, window time.Duration) (bool, error) {
	f.fixedCalls++
	f.lastKey = key
	f.lastLimit = int(limit)
	f.lastWindow = window
	if f.err != nil {
		return false, f.err
	}
	return f.allowFixed, nil
}

func TestRedisLimiter_SlidingWindow(t *testing.T) {
	be := &fakeRedisBackend{allowSliding: true}
	rl := NewRedisLimiter(be, "api:/users", 100, time.Second)

	ok, err := rl.AllowCtx(context.Background())
	if err != nil {
		t.Fatalf("应无错误，实际 %v", err)
	}
	if !ok {
		t.Fatal("应放行")
	}
	if be.slidingCalls != 1 || be.fixedCalls != 0 {
		t.Fatalf("应仅调用滑动窗口算法")
	}
	if be.lastKey != "api:/users" || be.lastLimit != 100 || be.lastWindow != time.Second {
		t.Fatalf("透传参数错误: %+v", be)
	}
}

func TestRedisLimiter_FixedWindow(t *testing.T) {
	be := &fakeRedisBackend{allowFixed: true}
	rl := NewRedisFixedWindowLimiter(be, "api:/orders", 50, time.Minute)

	ok, _ := rl.AllowCtx(context.Background())
	if !ok {
		t.Fatal("应放行")
	}
	if be.fixedCalls != 1 || be.slidingCalls != 0 {
		t.Fatal("应仅调用固定窗口算法")
	}
}

func TestRedisLimiter_Deny(t *testing.T) {
	be := &fakeRedisBackend{allowSliding: false}
	rl := NewRedisLimiter(be, "k", 1, time.Second)
	ok, err := rl.AllowCtx(context.Background())
	if err != nil {
		t.Fatalf("被拒绝时不应返回 error: %v", err)
	}
	if ok {
		t.Fatal("应被拒绝")
	}
}

func TestRedisLimiter_Error(t *testing.T) {
	wantErr := errors.New("redis down")
	be := &fakeRedisBackend{err: wantErr}
	rl := NewRedisLimiter(be, "k", 1, time.Second)
	ok, err := rl.AllowCtx(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("应透传 error，实际 %v", err)
	}
	if ok {
		t.Fatal("出错应返回 false")
	}
}

func TestRedisLimiter_AllowConvenience(t *testing.T) {
	be := &fakeRedisBackend{allowSliding: true}
	rl := NewRedisLimiter(be, "k", 1, time.Second)
	if !rl.Allow() {
		t.Fatal("Allow 应放行")
	}

	be2 := &fakeRedisBackend{err: errors.New("oops")}
	rl2 := NewRedisLimiter(be2, "k", 1, time.Second)
	if rl2.Allow() {
		t.Fatal("出错时 Allow 应返回 false")
	}
}

func TestRedisLimiter_KeyAndInterface(t *testing.T) {
	be := &fakeRedisBackend{allowSliding: true}
	rl := NewRedisLimiter(be, "user:42", 1, time.Second)
	if rl.Key() != "user:42" {
		t.Fatalf("Key 不一致")
	}
	// 实现 Limiter 接口的编译期断言
	var _ Limiter = rl
}
