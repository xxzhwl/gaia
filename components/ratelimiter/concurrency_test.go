// @author wanlizhan
// @created 2026-05-28
package ratelimiter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencyLimiter_Basic(t *testing.T) {
	cl := NewConcurrencyLimiter(3)
	if cl.Max() != 3 {
		t.Fatalf("Max 应为 3，实际 %d", cl.Max())
	}
	if cl.Running() != 0 {
		t.Fatalf("初始 Running 应为 0")
	}
	if cl.Available() != 3 {
		t.Fatalf("初始 Available 应为 3")
	}
}

func TestConcurrencyLimiter_NormalizeMax(t *testing.T) {
	cl := NewConcurrencyLimiter(0)
	if cl.Max() != 1 {
		t.Fatalf("max<=0 时应纠正为 1，实际 %d", cl.Max())
	}
	cl2 := NewConcurrencyLimiter(-5)
	if cl2.Max() != 1 {
		t.Fatalf("max<0 时应纠正为 1")
	}
}

func TestConcurrencyLimiter_TryAcquire(t *testing.T) {
	cl := NewConcurrencyLimiter(2)
	if !cl.TryAcquire() {
		t.Fatal("第 1 次 TryAcquire 应成功")
	}
	if !cl.TryAcquire() {
		t.Fatal("第 2 次 TryAcquire 应成功")
	}
	if cl.TryAcquire() {
		t.Fatal("超限的 TryAcquire 应失败")
	}
	if cl.Running() != 2 {
		t.Fatalf("Running 应为 2，实际 %d", cl.Running())
	}
	cl.Release()
	if cl.Running() != 1 {
		t.Fatalf("释放后 Running 应为 1")
	}
	if !cl.TryAcquire() {
		t.Fatal("释放后 TryAcquire 应成功")
	}
}

func TestConcurrencyLimiter_AcquireBlocks(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	_ = cl.Acquire(context.Background())

	// 第二次 Acquire 应阻塞，直到 Release
	done := make(chan struct{})
	go func() {
		_ = cl.Acquire(context.Background())
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Acquire 在名额满时不应立即返回")
	case <-time.After(20 * time.Millisecond):
	}

	cl.Release()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Release 后 Acquire 应快速返回")
	}
	cl.Release()
}

func TestConcurrencyLimiter_AcquireCtxCancel(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	_ = cl.Acquire(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := cl.Acquire(ctx)
	if err == nil {
		t.Fatal("ctx 超时时 Acquire 应返回错误")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err 应为 DeadlineExceeded，实际 %v", err)
	}
	cl.Release()
}

func TestConcurrencyLimiter_Do(t *testing.T) {
	cl := NewConcurrencyLimiter(2)
	var counter int32
	var wg sync.WaitGroup

	// 并发 10 个，但同时执行最多 2 个
	maxParallel := int32(0)
	current := int32(0)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cl.Do(context.Background(), func() error {
				now := atomic.AddInt32(&current, 1)
				for {
					old := atomic.LoadInt32(&maxParallel)
					if now <= old || atomic.CompareAndSwapInt32(&maxParallel, old, now) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				atomic.AddInt32(&current, -1)
				atomic.AddInt32(&counter, 1)
				return nil
			})
		}()
	}
	wg.Wait()

	if atomic.LoadInt32(&counter) != 10 {
		t.Fatalf("counter 应为 10，实际 %d", counter)
	}
	if atomic.LoadInt32(&maxParallel) > 2 {
		t.Fatalf("最大并发不应超过 2，实际 %d", maxParallel)
	}
}

func TestConcurrencyLimiter_DoErrorPropagation(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	want := errors.New("boom")
	got := cl.Do(context.Background(), func() error { return want })
	if !errors.Is(got, want) {
		t.Fatalf("Do 应透传 fn 的 error")
	}
	if cl.Running() != 0 {
		t.Fatal("Do 返回后必须释放名额")
	}
}

func TestConcurrencyLimiter_DoNilFn(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	if err := cl.Do(context.Background(), nil); err != nil {
		t.Fatalf("Do(nil) 应返回 nil，实际 %v", err)
	}
}

func TestConcurrencyLimiter_OverRelease(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("超额 Release 应 panic")
		}
	}()
	cl.Release()
}
