package ratelimiter

import (
	"context"
	"testing"
	"time"
)

func TestNewLocalLimiter(t *testing.T) {
	limiter := NewLocalLimiter(10, 5)
	if limiter == nil {
		t.Fatal("NewLocalLimiter 返回 nil")
	}
}

func TestAllow(t *testing.T) {
	// 10 QPS, burst 3
	limiter := NewLocalLimiter(10, 3)

	// burst 内应该立即放行
	for i := 0; i < 3; i++ {
		if !limiter.Allow() {
			t.Fatalf("第 %d 次请求应该被放行（在 burst 内）", i+1)
		}
	}

	// burst 用完后，紧接着的请求应该被限流
	if limiter.Allow() {
		t.Fatal("burst 用完后应该被限流")
	}
}

func TestAllowCtx(t *testing.T) {
	limiter := NewLocalLimiter(100, 5)

	ok, err := limiter.AllowCtx(context.Background())
	if err != nil {
		t.Fatalf("AllowCtx 不应返回错误: %v", err)
	}
	if !ok {
		t.Fatal("AllowCtx 应该放行")
	}
}

func TestWait(t *testing.T) {
	limiter := NewLocalLimiter(100, 1) // 100 QPS

	// 先用掉 burst
	limiter.Allow()

	// Wait 应该在约 10ms 后返回
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := limiter.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Wait 不应超时: %v", err)
	}
	if elapsed < 5*time.Millisecond {
		t.Fatal("Wait 应该有一定等待时间")
	}
}

func TestWaitTimeout(t *testing.T) {
	limiter := NewLocalLimiter(1, 1) // 1 QPS

	// 用掉 burst
	limiter.Allow()

	// 超时时间极短
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	err := limiter.Wait(ctx)
	if err == nil {
		t.Fatal("极短超时应该返回错误")
	}
}

func TestSetRate(t *testing.T) {
	limiter := NewLocalLimiter(1, 1)
	limiter.Allow() // 用掉 burst

	// 提高速率到 10000 QPS
	limiter.SetRate(10000)
	time.Sleep(1 * time.Millisecond)

	if !limiter.Allow() {
		t.Fatal("提高速率后应该能立即放行")
	}
}

func TestSetBurst(t *testing.T) {
	limiter := NewLocalLimiter(10, 1) // 10 QPS
	limiter.Allow()                   // 用掉 burst

	// 增加 burst
	limiter.SetBurst(10)

	// 等 token 补充（10 QPS，200ms 至少补 1 个）
	time.Sleep(200 * time.Millisecond)
	if !limiter.Allow() {
		t.Fatal("增加 burst 并等待补充后应该能放行")
	}
}

func TestLimiterInterface(t *testing.T) {
	// 确保 LocalLimiter 实现了 Limiter 接口
	var _ Limiter = &LocalLimiter{}
}
