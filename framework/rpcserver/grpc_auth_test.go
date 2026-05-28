package rpcserver

import (
	"context"
	"testing"

	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func newAuthCfg() authConfig {
	return authConfig{
		enabled:   true,
		headerKey: "authorization",
		scheme:    "Bearer",
		tokens:    map[string]struct{}{"good-token": {}},
		skipMethods: map[string]struct{}{
			"/skip.Svc/Method": {},
		},
	}
}

func ctxWithMD(kv ...string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(kv...))
}

func wantCode(t *testing.T, err error, code grpccodes.Code) {
	t.Helper()
	if status.Code(err) != code {
		t.Fatalf("期望 code=%s，实际 err=%v (code=%s)", code, err, status.Code(err))
	}
}

func TestAuthorize_Disabled(t *testing.T) {
	cfg := newAuthCfg()
	cfg.enabled = false
	if _, err := cfg.authorize(context.Background(), "/any/Method"); err != nil {
		t.Fatalf("未启用鉴权应放行，得到 %v", err)
	}
}

func TestAuthorize_SkipMethod(t *testing.T) {
	cfg := newAuthCfg()
	if _, err := cfg.authorize(context.Background(), "/skip.Svc/Method"); err != nil {
		t.Fatalf("白名单方法应放行，得到 %v", err)
	}
}

func TestAuthorize_NoMetadata(t *testing.T) {
	cfg := newAuthCfg()
	_, err := cfg.authorize(context.Background(), "/svc/M")
	wantCode(t, err, grpccodes.Unauthenticated)
}

func TestAuthorize_MissingHeader(t *testing.T) {
	cfg := newAuthCfg()
	_, err := cfg.authorize(ctxWithMD("other", "x"), "/svc/M")
	wantCode(t, err, grpccodes.Unauthenticated)
}

func TestAuthorize_BadScheme(t *testing.T) {
	cfg := newAuthCfg()
	_, err := cfg.authorize(ctxWithMD("authorization", "Token good-token"), "/svc/M")
	wantCode(t, err, grpccodes.Unauthenticated)
}

func TestAuthorize_NoTokensConfigured(t *testing.T) {
	cfg := newAuthCfg()
	cfg.tokens = map[string]struct{}{}
	_, err := cfg.authorize(ctxWithMD("authorization", "Bearer good-token"), "/svc/M")
	wantCode(t, err, grpccodes.Unauthenticated)
}

func TestAuthorize_InvalidToken(t *testing.T) {
	cfg := newAuthCfg()
	_, err := cfg.authorize(ctxWithMD("authorization", "Bearer wrong"), "/svc/M")
	wantCode(t, err, grpccodes.Unauthenticated)
}

func TestAuthorize_ValidToken(t *testing.T) {
	cfg := newAuthCfg()
	if _, err := cfg.authorize(ctxWithMD("authorization", "Bearer good-token"), "/svc/M"); err != nil {
		t.Fatalf("合法 token 应放行，得到 %v", err)
	}
}

// TestAuthorize_EmptyScheme scheme 为空时取整个 header 值作为 token。
func TestAuthorize_EmptyScheme(t *testing.T) {
	cfg := newAuthCfg()
	cfg.scheme = ""
	if _, err := cfg.authorize(ctxWithMD("authorization", "good-token"), "/svc/M"); err != nil {
		t.Fatalf("空 scheme 下整串匹配应放行，得到 %v", err)
	}
}

// TestAuthorize_CustomFuncTakesPrecedence 自定义鉴权函数优先于内置 token 校验，且在 enabled=false 时也生效。
func TestAuthorize_CustomFuncTakesPrecedence(t *testing.T) {
	called := false
	SetAuthFunc(func(ctx context.Context, fullMethod string) (context.Context, error) {
		called = true
		return ctx, status.Error(grpccodes.PermissionDenied, "denied by custom")
	})
	defer SetAuthFunc(nil)

	cfg := newAuthCfg()
	cfg.enabled = false // 即便关闭内置鉴权，自定义函数仍应被调用
	_, err := cfg.authorize(context.Background(), "/svc/M")
	if !called {
		t.Fatal("自定义鉴权函数应被调用")
	}
	wantCode(t, err, grpccodes.PermissionDenied)
}

// TestLoadAuthConfig_Defaults 无配置时的默认值与内置免鉴权方法。
func TestLoadAuthConfig_Defaults(t *testing.T) {
	cfg := loadAuthConfig("NonExistSchemaForTest")
	if cfg.enabled {
		t.Fatal("默认应为未启用")
	}
	if cfg.headerKey != "authorization" {
		t.Fatalf("默认 headerKey 应为 authorization，实际 %q", cfg.headerKey)
	}
	if cfg.scheme != "Bearer" {
		t.Fatalf("默认 scheme 应为 Bearer，实际 %q", cfg.scheme)
	}
	if _, ok := cfg.skipMethods["/grpc.health.v1.Health/Check"]; !ok {
		t.Fatal("健康检查方法应默认免鉴权")
	}
}
