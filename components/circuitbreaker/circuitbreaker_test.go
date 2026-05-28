// Package circuitbreaker 单元测试。
// @author wanlizhan
// @created 2026-06-01
package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBreaker_ClosedToOpenByFailureRate(t *testing.T) {
	b := New(Settings{
		Name:             "test",
		FailureThreshold: 0.5,
		MinRequests:      4,
		OpenTimeout:      100 * time.Millisecond,
		HalfOpenMaxCalls: 1,
	})

	// 4 次调用：2 成功 2 失败，刚好达到 50% 阈值 → 应该触发熔断
	for i := 0; i < 4; i++ {
		gen, err := b.Allow()
		if err != nil {
			t.Fatalf("Allow 不应失败: %v", err)
		}
		if i%2 == 0 {
			b.Report(gen, errors.New("boom"))
		} else {
			b.Report(gen, nil)
		}
	}

	if got := b.State(); got != StateOpen {
		t.Fatalf("期望 Open，实际 %s", got)
	}

	// Open 状态下立即 Allow 应失败
	if _, err := b.Allow(); !errors.Is(err, ErrOpen) {
		t.Fatalf("期望 ErrOpen，实际 %v", err)
	}
}

func TestBreaker_ConsecutiveFailures(t *testing.T) {
	b := New(Settings{
		Name:                "test-conseq",
		ConsecutiveFailures: 3,
		MinRequests:          1000, // 故意调高，避免被失败率判据干扰
		OpenTimeout:          100 * time.Millisecond,
	})

	// 连续 3 次失败应触发
	for i := 0; i < 3; i++ {
		gen, err := b.Allow()
		if err != nil {
			t.Fatalf("Allow 不应失败: %v", err)
		}
		b.Report(gen, errors.New("boom"))
	}
	if got := b.State(); got != StateOpen {
		t.Fatalf("期望 Open，实际 %s", got)
	}
}

func TestBreaker_HalfOpenRecover(t *testing.T) {
	b := New(Settings{
		Name:             "test-recover",
		FailureThreshold: 0.5,
		MinRequests:      2,
		OpenTimeout:      30 * time.Millisecond,
		HalfOpenMaxCalls: 2,
	})

	// 触发 Open
	for i := 0; i < 2; i++ {
		gen, _ := b.Allow()
		b.Report(gen, errors.New("boom"))
	}
	if b.State() != StateOpen {
		t.Fatal("应进入 Open")
	}

	// 等待 OpenTimeout 过期
	time.Sleep(50 * time.Millisecond)

	// 第一次 Allow → 进入 HalfOpen，发起探活 1
	gen1, err := b.Allow()
	if err != nil {
		t.Fatalf("Open 过期后第一次 Allow 应放行: %v", err)
	}
	if b.State() != StateHalfOpen {
		t.Fatalf("应进入 HalfOpen，实际 %s", b.State())
	}
	// HalfOpen 探活并发额度=2，第二次 Allow 也应放行
	gen2, err := b.Allow()
	if err != nil {
		t.Fatalf("HalfOpen 第二次探活应放行: %v", err)
	}
	// 第三次应被拒（超过 HalfOpenMaxCalls）
	if _, err := b.Allow(); !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("HalfOpen 第三次应返回 ErrTooManyRequests，实际 %v", err)
	}

	// 两次探活成功 → 转 Closed
	b.Report(gen1, nil)
	b.Report(gen2, nil)
	if got := b.State(); got != StateClosed {
		t.Fatalf("两次探活成功应转 Closed，实际 %s", got)
	}
}

func TestBreaker_HalfOpenFailReturnsToOpen(t *testing.T) {
	b := New(Settings{
		Name:             "test-halfopen-fail",
		FailureThreshold: 0.5,
		MinRequests:      2,
		OpenTimeout:      20 * time.Millisecond,
		HalfOpenMaxCalls: 1,
	})

	// 进入 Open
	for i := 0; i < 2; i++ {
		gen, _ := b.Allow()
		b.Report(gen, errors.New("boom"))
	}
	time.Sleep(40 * time.Millisecond)

	// HalfOpen 探活失败 → 重新 Open
	gen, err := b.Allow()
	if err != nil {
		t.Fatalf("应进入 HalfOpen 并放行探活: %v", err)
	}
	b.Report(gen, errors.New("still bad"))

	if got := b.State(); got != StateOpen {
		t.Fatalf("HalfOpen 探活失败应回到 Open，实际 %s", got)
	}
}

func TestBreaker_GenerationDropsStaleReports(t *testing.T) {
	b := New(Settings{
		Name:             "test-gen",
		FailureThreshold: 0.5,
		MinRequests:      2,
		OpenTimeout:      30 * time.Millisecond,
		HalfOpenMaxCalls: 1,
	})

	// 拿到 closed 代的 gen
	staleGen, _ := b.Allow()

	// 触发熔断
	gen, _ := b.Allow()
	b.Report(gen, errors.New("boom"))
	gen, _ = b.Allow()
	b.Report(gen, errors.New("boom"))
	if b.State() != StateOpen {
		t.Fatal("应进入 Open")
	}

	// 用 staleGen 上报：应被丢弃，状态不变
	b.Report(staleGen, errors.New("late report"))
	if b.State() != StateOpen {
		t.Fatalf("过期上报不应改变状态")
	}
}

func TestBreaker_ExecuteOpenFastFail(t *testing.T) {
	b := New(Settings{
		Name:                "exec",
		ConsecutiveFailures: 1,
		MinRequests:          1000,
	})

	// 第一次失败 → Open
	_ = b.Execute(context.Background(), func() error { return errors.New("fail") })
	if b.State() != StateOpen {
		t.Fatal("应进入 Open")
	}

	// Open 状态：fn 不应被执行，直接返回 ErrOpen
	called := false
	err := b.Execute(context.Background(), func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("期望 ErrOpen，实际 %v", err)
	}
	if called {
		t.Fatal("Open 状态下 fn 不应被执行（fast-fail 是熔断器核心价值）")
	}
}

func TestBreaker_CountWindowReset(t *testing.T) {
	b := New(Settings{
		Name:             "win",
		FailureThreshold: 0.99, // 几乎要求 100% 失败才熔断
		MinRequests:      4,
		OpenTimeout:      time.Second,
		CountWindow:      30 * time.Millisecond,
	})

	// 失败 2 次（达不到熔断条件）
	for i := 0; i < 2; i++ {
		gen, _ := b.Allow()
		b.Report(gen, errors.New("x"))
	}
	// 等窗口过期
	time.Sleep(50 * time.Millisecond)
	// 上报 1 次成功 → 之前的失败应已清零
	gen, _ := b.Allow()
	b.Report(gen, nil)

	snap := b.Snapshot()
	if snap.Failures != 0 {
		t.Fatalf("窗口过期后 failures 应清零，实际 %d", snap.Failures)
	}
	if snap.Successes != 1 {
		t.Fatalf("窗口过期后 successes 应只剩本次的 1，实际 %d", snap.Successes)
	}
}

func TestBreaker_OnStateChangeCallback(t *testing.T) {
	var transitions []string
	var mu sync.Mutex

	b := New(Settings{
		Name:                "cb-cb",
		ConsecutiveFailures: 1,
		MinRequests:          1000,
		OpenTimeout:          20 * time.Millisecond,
		HalfOpenMaxCalls:     1,
		OnStateChange: func(_ string, from, to State) {
			mu.Lock()
			defer mu.Unlock()
			transitions = append(transitions, from.String()+"->"+to.String())
		},
	})

	gen, _ := b.Allow()
	b.Report(gen, errors.New("boom")) // closed -> open
	time.Sleep(40 * time.Millisecond)
	gen, _ = b.Allow() // open -> half-open
	b.Report(gen, nil) // half-open -> closed

	mu.Lock()
	defer mu.Unlock()
	want := []string{"closed->open", "open->half-open", "half-open->closed"}
	if len(transitions) != len(want) {
		t.Fatalf("状态切换次数不符: 期望 %v, 实际 %v", want, transitions)
	}
	for i := range want {
		if transitions[i] != want[i] {
			t.Fatalf("第 %d 次切换不符: 期望 %s, 实际 %s", i, want[i], transitions[i])
		}
	}
}

func TestBreaker_ConcurrentSafety(t *testing.T) {
	b := New(Settings{
		Name:             "concurrent",
		FailureThreshold: 0.99, // 不会被触发，专测线程安全
		MinRequests:      999999,
		OpenTimeout:      time.Second,
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				gen, err := b.Allow()
				if err != nil {
					continue
				}
				if (id+j)%3 == 0 {
					b.Report(gen, errors.New("x"))
				} else {
					b.Report(gen, nil)
				}
			}
		}(i)
	}
	wg.Wait()

	// 只要不发生 race / panic 即通过；这里同时验证 snapshot 可读
	snap := b.Snapshot()
	if snap.Requests == 0 && snap.Successes == 0 {
		t.Fatal("并发场景下应该有计数")
	}
}
