// Package rpcserver gRPC 服务端鉴权拦截器。
//
// 支持基于 metadata 的 Token 鉴权（Bearer / 自定义 header），可按方法白名单跳过。
//
// 配置示例：
//
//	RpcServer:
//	  Auth:
//	    Enable: true
//	    HeaderKey: authorization        # 读取的 metadata key，默认 authorization
//	    Scheme: Bearer                  # token 前缀，默认 Bearer；为空则取整个 header 值
//	    Tokens:                         # 允许的 token 列表（逗号分隔字符串）
//	      - "token-a,token-b"
//	    SkipMethods:                    # 跳过鉴权的方法全名（逗号分隔），健康检查 / 反射默认跳过
//	      - "/grpc.health.v1.Health/Check"
//
// 也可通过 SetAuthFunc 注入自定义鉴权逻辑（优先级高于内置 token 校验）。
//
// @author gaia-framework
// @created 2026-06-24
package rpcserver

import (
	"context"
	"strings"
	"sync"

	"github.com/xxzhwl/gaia"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthFunc 自定义鉴权函数。返回非 nil error 表示拒绝（建议用 status.Error 携带 code）。
// 可通过返回的 context 向 handler 传递鉴权后的身份信息。
type AuthFunc func(ctx context.Context, fullMethod string) (context.Context, error)

var (
	customAuthFunc   AuthFunc
	customAuthFuncMu sync.RWMutex
)

// SetAuthFunc 注入自定义鉴权函数（全局生效，优先于内置 token 校验）。
// 传 nil 可清除。
func SetAuthFunc(f AuthFunc) {
	customAuthFuncMu.Lock()
	customAuthFunc = f
	customAuthFuncMu.Unlock()
}

func getAuthFunc() AuthFunc {
	customAuthFuncMu.RLock()
	defer customAuthFuncMu.RUnlock()
	return customAuthFunc
}

// authConfig 鉴权配置（拦截器创建时一次性读取）。
type authConfig struct {
	enabled     bool
	headerKey   string
	scheme      string
	tokens      map[string]struct{}
	skipMethods map[string]struct{}
}

func loadAuthConfig(schema string) authConfig {
	cfg := authConfig{
		enabled:     gaia.GetSafeConfBool(schema + ".Auth.Enable"),
		headerKey:   strings.ToLower(gaia.GetSafeConfStringWithDefault(schema+".Auth.HeaderKey", "authorization")),
		scheme:      gaia.GetSafeConfStringWithDefault(schema+".Auth.Scheme", "Bearer"),
		tokens:      map[string]struct{}{},
		skipMethods: map[string]struct{}{},
	}
	for _, t := range gaia.GetSafeConfStringSliceFromString(schema + ".Auth.Tokens") {
		if t = strings.TrimSpace(t); t != "" {
			cfg.tokens[t] = struct{}{}
		}
	}
	// 健康检查与反射默认免鉴权
	cfg.skipMethods["/grpc.health.v1.Health/Check"] = struct{}{}
	cfg.skipMethods["/grpc.health.v1.Health/Watch"] = struct{}{}
	cfg.skipMethods["/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"] = struct{}{}
	cfg.skipMethods["/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo"] = struct{}{}
	for _, mth := range gaia.GetSafeConfStringSliceFromString(schema + ".Auth.SkipMethods") {
		if mth = strings.TrimSpace(mth); mth != "" {
			cfg.skipMethods[mth] = struct{}{}
		}
	}
	return cfg
}

// authorize 执行鉴权校验，返回（可能被增强的）context 或错误。
func (c authConfig) authorize(ctx context.Context, fullMethod string) (context.Context, error) {
	// 自定义鉴权优先
	if f := getAuthFunc(); f != nil {
		return f(ctx, fullMethod)
	}

	if !c.enabled {
		return ctx, nil
	}
	if _, skip := c.skipMethods[fullMethod]; skip {
		return ctx, nil
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, status.Error(grpccodes.Unauthenticated, "缺少认证元数据")
	}
	vals := md.Get(c.headerKey)
	if len(vals) == 0 {
		return ctx, status.Errorf(grpccodes.Unauthenticated, "缺少认证头 %s", c.headerKey)
	}

	token := strings.TrimSpace(vals[0])
	if c.scheme != "" {
		prefix := c.scheme + " "
		if !strings.HasPrefix(token, prefix) {
			return ctx, status.Errorf(grpccodes.Unauthenticated, "认证头格式错误，期望前缀 %q", c.scheme)
		}
		token = strings.TrimSpace(strings.TrimPrefix(token, prefix))
	}

	if len(c.tokens) == 0 {
		// 启用了鉴权但未配置任何合法 token：拒绝，避免误以为已保护实则放行
		return ctx, status.Error(grpccodes.Unauthenticated, "服务端未配置任何合法 token")
	}
	if _, valid := c.tokens[token]; !valid {
		return ctx, status.Error(grpccodes.Unauthenticated, "无效的 token")
	}
	return ctx, nil
}

// GrpcAuthUnaryInterceptor 一元鉴权拦截器。
func GrpcAuthUnaryInterceptor(schema string) grpc.UnaryServerInterceptor {
	cfg := loadAuthConfig(schema)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		newCtx, err := cfg.authorize(ctx, info.FullMethod)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
}

// GrpcAuthStreamInterceptor 流式鉴权拦截器。
func GrpcAuthStreamInterceptor(schema string) grpc.StreamServerInterceptor {
	cfg := loadAuthConfig(schema)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		newCtx, err := cfg.authorize(ss.Context(), info.FullMethod)
		if err != nil {
			return err
		}
		return handler(srv, &grpcWrappedServerStream{ServerStream: ss, ctx: newCtx})
	}
}
