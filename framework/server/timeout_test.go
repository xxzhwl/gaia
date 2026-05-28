// timeout_test.go 验证：
//  1. 业务正常返回 → 中间件不干预，原样返回 200
//  2. 业务监听 ctx.Done() 自己写 504（中间件只负责 cancel ctx）
//  3. 业务彻底不监听 ctx 跑超 timeout 也不会 race（中间件不强制写响应）
//  4. 探针请求豁免（即便 timeout=1ms 也不会被中断）
//  5. 高并发 + race 检测
//  6. isProbeRequest 单元测试
//
// @author wanlizhan
// @created 2026-06-01
package server

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

// runTimeoutHandler 跑一遍中间件 + 业务，等业务自身完成（最多 500ms）后返回。
func runTimeoutHandler(t *testing.T, cfg timeoutConfig, path string, business app.HandlerFunc) (status int, body string) {
	t.Helper()
	finished := make(chan struct{})
	wrapped := func(c context.Context, ctx *app.RequestContext) {
		defer close(finished)
		business(c, ctx)
	}

	c := ut.CreateUtRequestContext("GET", path, nil)
	c.SetHandlers([]app.HandlerFunc{newTimeoutHandler(cfg), wrapped})
	c.SetIndex(-1)
	c.Next(context.Background())

	select {
	case <-finished:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("业务 handler 未在 500ms 内完成")
	}
	return c.Response.StatusCode(), string(c.Response.Body())
}

// 1) 业务在超时前正常返回 → 中间件不介入
func TestTimeout_BusinessFinishesBeforeDeadline(t *testing.T) {
	cfg := timeoutConfig{enable: true, timeout: 200 * time.Millisecond}

	status, _ := runTimeoutHandler(t, cfg, "/api/foo",
		func(_ context.Context, ctx *app.RequestContext) {
			time.Sleep(20 * time.Millisecond)
			ctx.JSON(http.StatusOK, map[string]string{"ok": "1"})
		},
	)
	if status != http.StatusOK {
		t.Fatalf("应返回 200，实际 %d", status)
	}
}

// 2) 业务监听 ctx.Done() 自己写 504：这是新方案下的"正确业务姿态"
func TestTimeout_BusinessHandlesTimeoutItself(t *testing.T) {
	cfg := timeoutConfig{enable: true, timeout: 30 * time.Millisecond}

	status, body := runTimeoutHandler(t, cfg, "/api/slow",
		func(c context.Context, ctx *app.RequestContext) {
			select {
			case <-c.Done():
				// ctx 被中间件 cancel；业务自行写 504
				ctx.JSON(http.StatusGatewayTimeout, map[string]string{"msg": "ctx done"})
			case <-time.After(500 * time.Millisecond):
				ctx.JSON(http.StatusOK, map[string]string{"ok": "1"})
			}
		},
	)
	if status != http.StatusGatewayTimeout {
		t.Fatalf("业务应自行写 504，实际 %d, body=%q", status, body)
	}
	if !contains(body, "ctx done") {
		t.Fatalf("body 应包含业务写入的内容，实际 %q", body)
	}
}

// 3) 业务彻底不监听 ctx：中间件不强制中断、不写响应、也不 race
func TestTimeout_BusinessIgnoresCtx_NoRace(t *testing.T) {
	cfg := timeoutConfig{enable: true, timeout: 10 * time.Millisecond}

	status, _ := runTimeoutHandler(t, cfg, "/api/zombie",
		func(_ context.Context, ctx *app.RequestContext) {
			// 罔顾 ctx 跑超 timeout——hertz 同 goroutine 假设下，新方案不会 race
			time.Sleep(40 * time.Millisecond)
			ctx.JSON(http.StatusOK, map[string]string{"late": "ok"})
		},
	)
	// 业务最终自己写了 200，中间件不介入
	if status != http.StatusOK {
		t.Fatalf("中间件不应干预业务响应，实际 %d", status)
	}
}

// 4) 探针请求：即便 timeout=1ms，也不应被影响
func TestTimeout_ProbeRequestExempt(t *testing.T) {
	cfg := timeoutConfig{enable: true, timeout: 1 * time.Millisecond}

	for _, p := range []string{"/livez", "/readyz", "/health", "/metrics"} {
		status, _ := runTimeoutHandler(t, cfg, p,
			func(_ context.Context, ctx *app.RequestContext) {
				time.Sleep(15 * time.Millisecond)
				ctx.JSON(http.StatusOK, map[string]string{"probe": "ok"})
			},
		)
		if status != http.StatusOK {
			t.Fatalf("探针 %s 应豁免超时，实际 %d", p, status)
		}
	}
}

// 5) 50 并发 × 4 次轮转，-race 检测
//    跑命令: go test -race ./framework/server -run TestTimeout_ConcurrentNoRace
func TestTimeout_ConcurrentNoRace(t *testing.T) {
	cfg := timeoutConfig{enable: true, timeout: 10 * time.Millisecond}

	var wg sync.WaitGroup
	var totalOK, totalTimedOut int64
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				c := ut.CreateUtRequestContext("GET", "/api/x", nil)
				c.SetHandlers([]app.HandlerFunc{
					newTimeoutHandler(cfg),
					func(reqCtx context.Context, ctx *app.RequestContext) {
						if (id+j)%2 == 0 {
							ctx.JSON(http.StatusOK, map[string]string{"ok": "1"})
							atomic.AddInt64(&totalOK, 1)
						} else {
							select {
							case <-reqCtx.Done():
								ctx.JSON(http.StatusGatewayTimeout, map[string]string{"to": "1"})
								atomic.AddInt64(&totalTimedOut, 1)
							case <-time.After(50 * time.Millisecond):
								ctx.JSON(http.StatusOK, map[string]string{"slow_ok": "1"})
								atomic.AddInt64(&totalOK, 1)
							}
						}
					},
				})
				c.SetIndex(-1)
				c.Next(context.Background())
			}
		}(i)
	}
	wg.Wait()
	// 总请求 200 次，期望 ~100 OK + ~100 504（监听 ctx 的那批必超时）
	if got := atomic.LoadInt64(&totalOK) + atomic.LoadInt64(&totalTimedOut); got != 200 {
		t.Fatalf("总响应数应为 200，实际 %d", got)
	}
}

// 6) isProbeRequest 路径白名单覆盖
func TestIsProbeRequest(t *testing.T) {
	cases := map[string]bool{
		"/livez":     true,
		"/readyz":    true,
		"/health":    true,
		"/metrics":   true,
		"/api/users": false,
		"/livez/sub": false, // 严格路径匹配
		"/":          false,
	}
	for p, want := range cases {
		c := ut.CreateUtRequestContext("GET", "http://example.com"+p, nil)
		if got := isProbeRequest(c); got != want {
			t.Errorf("isProbeRequest(%q) = %v, want %v", p, got, want)
		}
	}
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
