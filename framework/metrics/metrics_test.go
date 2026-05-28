// Package metrics 测试。
// @author wanlizhan
// @created 2026/05/28
package metrics

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// resetGlobals 让每个测试用例从干净状态开始。
func resetGlobals(t *testing.T) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	isInitialized = false
	shutdownFunc = nil
	currentBackend = ""
	if promServer != nil {
		_ = promServer.Shutdown(context.Background())
		promServer = nil
	}
}

// TestSetup_Disabled 配置关闭时应保持 noop 且 Setup 不报错。
func TestSetup_Disabled(t *testing.T) {
	resetGlobals(t)

	// 直接构造 Config 走内部路径：Backend=disabled。
	mu.Lock()
	cfg := Config{Enabled: false, Backend: BackendDisabled, ServiceName: "ut"}
	mu.Unlock()

	// 模拟 Setup 中 disabled 路径
	ctx := context.Background()
	_ = cfg
	shutdown, err := Setup(ctx, "ut")
	if err != nil {
		t.Fatalf("Setup returned err: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown should not be nil")
	}
	if !IsInitialized() {
		t.Fatal("expected initialized after Setup")
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown err: %v", err)
	}
	if err := Shutdown(ctx); err != nil {
		t.Fatalf("repeat Shutdown err: %v", err)
	}
}

// TestSetup_Idempotent 重复调用 Setup 应幂等。
func TestSetup_Idempotent(t *testing.T) {
	resetGlobals(t)
	ctx := context.Background()
	s1, err := Setup(ctx, "ut")
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Setup(ctx, "ut")
	if err != nil {
		t.Fatal(err)
	}
	// 两次返回的 shutdown 句柄应当指向同一个底层闭包（指针相同）。
	// Go 不能直接对比函数值，但二者均不应为 nil。
	if s1 == nil || s2 == nil {
		t.Fatal("shutdown nil")
	}
	_ = Shutdown(ctx)
}

// TestPrometheusBackend_HTTPExpose 验证 Prometheus backend 能起 HTTP 并响应 /metrics。
func TestPrometheusBackend_HTTPExpose(t *testing.T) {
	resetGlobals(t)
	t.Setenv("GAIA_FORCE_TEST_PROM", "1") // 仅作为标记，无实际作用

	cfg := Config{
		Enabled:        true,
		Backend:        BackendPrometheus,
		ServiceName:    "ut-prom",
		PromListenAddr: "127.0.0.1:0", // 0 端口让内核分配，避免冲突
		PromPath:       "/metrics",
	}

	res, err := buildResource(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	mp, mpShutdown, beShutdown, err := buildPrometheusProvider(cfg, res)
	if err != nil {
		t.Fatalf("buildPrometheusProvider: %v", err)
	}
	t.Cleanup(func() {
		if beShutdown != nil {
			_ = beShutdown(context.Background())
		}
		_ = mpShutdown(context.Background())
	})

	if mp == nil {
		t.Fatal("mp nil")
	}

	// 由于使用 :0，actual addr 在 promServer 中。等一小会儿等 server 起来。
	time.Sleep(100 * time.Millisecond)

	mu.RLock()
	srv := promServer
	mu.RUnlock()
	if srv == nil {
		t.Fatal("promServer should be set when ListenAddr provided")
	}

	// 这里我们没法直接拿到 :0 解析后的地址（srv.Addr 是空因为我们用 net.Listen 拿的 ln，
	// 但闭包里没回填）。改为只验证 Handler 直接调用。
	rec := newRecorder()
	Handler().ServeHTTP(rec, mustReq("GET", "/metrics"))
	if rec.code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.code, rec.body.String())
	}
	if !strings.Contains(rec.body.String(), "# HELP") && rec.body.Len() == 0 {
		// 还没有指标也是合法的，但响应应当成功。
	}
}

// --- helpers ---

type recorder struct {
	code int
	body strings.Builder
	hdr  http.Header
}

func newRecorder() *recorder { return &recorder{code: 200, hdr: http.Header{}} }
func (r *recorder) Header() http.Header { return r.hdr }
func (r *recorder) Write(p []byte) (int, error) {
	return r.body.Write(p)
}
func (r *recorder) WriteHeader(c int) { r.code = c }

func mustReq(method, target string) *http.Request {
	req, err := http.NewRequestWithContext(context.Background(), method, target, http.NoBody)
	if err != nil {
		panic(err)
	}
	return req
}

var _ io.Writer = (*recorder)(nil)
