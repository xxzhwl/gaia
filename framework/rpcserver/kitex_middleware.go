// Package rpcserver Kitex 中间件链
// 复用 gaia 框架能力，适配 Kitex 的 endpoint.Middleware 签名
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"context"
	"sync"
	"time"

	"github.com/cloudwego/kitex/pkg/endpoint"
	"github.com/cloudwego/kitex/pkg/rpcinfo"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// ===== 1. Panic 恢复 =====

// KitexRecoveryMiddleware Kitex panic 恢复中间件
func KitexRecoveryMiddleware() endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, req, resp any) (err error) {
			defer func() {
				if r := recover(); r != nil {
					errMsg := gaia.PanicLog(r)
					err = kerror("[Kitex] 服务器内部错误: " + errMsg)
				}
			}()
			return next(ctx, req, resp)
		}
	}
}

// ===== 2. 日志记录 =====

// KitexLoggingMiddleware Kitex 日志记录中间件
func KitexLoggingMiddleware() endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, req, resp any) error {
			startTime := time.Now()

			// 提取 RPC 调用信息
			ri := rpcinfo.GetRPCInfo(ctx)
			method := "unknown"
			clientAddr := "unknown"
			if ri != nil {
				if inv := ri.Invocation(); inv != nil {
					method = inv.ServiceName() + "/" + inv.MethodName()
				}
				if from := ri.From(); from != nil && from.Address() != nil {
					clientAddr = from.Address().String()
				}
			}

			gaia.BuildContextTrace()
			defer gaia.RemoveContextTrace()

			err := next(ctx, req, resp)
			duration := time.Since(startTime)

			if err != nil {
				rpcLogger.WarnF("[Kitex] %s | %s | %v | ERROR: %v",
					method, clientAddr, duration, err)
			} else {
				rpcLogger.InfoF("[Kitex] %s | %s | %v | OK",
					method, clientAddr, duration)
			}

			return err
		}
	}
}

// ===== 3. 链路追踪 =====

// KitexTracingMiddleware Kitex 链路追踪中间件
func KitexTracingMiddleware() endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, req, resp any) error {
			tr := tracer.GetTracer()
			if tr == nil {
				return next(ctx, req, resp)
			}

			ri := rpcinfo.GetRPCInfo(ctx)
			method := "unknown"
			if ri != nil {
				if inv := ri.Invocation(); inv != nil {
					method = inv.ServiceName() + "/" + inv.MethodName()
				}
			}

			ctx, span := tr.Start(ctx, method,
				oteltrace.WithSpanKind(oteltrace.SpanKindServer),
				oteltrace.WithAttributes(
					attribute.String("rpc.system", "kitex"),
					attribute.String("rpc.method", method),
				),
			)
			defer span.End()

			if gaiaCtx := gaia.GetContextTrace(); gaiaCtx != nil {
				gaiaCtx.ParentContext = ctx
			}

			err := next(ctx, req, resp)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}

			return err
		}
	}
}

// ===== 4. 限流 =====

var (
	kitexLimiters   = make(map[string]*rpcRateLimiter)
	kitexLimitersMu sync.RWMutex
)

func getKitexRateLimiter(key string, capacity int, rate float64) *rpcRateLimiter {
	kitexLimitersMu.RLock()
	rl, ok := kitexLimiters[key]
	kitexLimitersMu.RUnlock()
	if ok {
		return rl
	}
	kitexLimitersMu.Lock()
	defer kitexLimitersMu.Unlock()
	if rl, ok := kitexLimiters[key]; ok {
		return rl
	}
	rl = newRpcRateLimiter(capacity, rate)
	kitexLimiters[key] = rl
	return rl
}

// KitexRateLimitMiddleware Kitex 限流中间件
func KitexRateLimitMiddleware(schema string) endpoint.Middleware {
	capacity := int(gaia.GetSafeConfInt64WithDefault(schema+".RateLimit.Capacity", 200))
	rate := gaia.GetSafeConfFloat64WithDefault(schema+".RateLimit.Rate", 100.0)
	enabled := gaia.GetSafeConfBool(schema + ".RateLimit.Enable")

	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, req, resp any) error {
			if !enabled {
				return next(ctx, req, resp)
			}

			ri := rpcinfo.GetRPCInfo(ctx)
			method := "unknown"
			clientAddr := "unknown"
			if ri != nil {
				if inv := ri.Invocation(); inv != nil {
					method = inv.ServiceName() + "/" + inv.MethodName()
				}
				if from := ri.From(); from != nil && from.Address() != nil {
					clientAddr = from.Address().String()
				}
			}

			key := method + "|" + clientAddr
			rl := getKitexRateLimiter(key, capacity, rate)
			if !rl.allow() {
				return kerror("请求过于频繁，请稍后再试")
			}

			return next(ctx, req, resp)
		}
	}
}

// ===== 辅助 =====

// kerror 创建一个通用 Kitex 错误
func kerror(msg string) error {
	// 使用标准 error，Kitex 会自动处理为 RPC 错误
	return &kitexError{msg: msg}
}

type kitexError struct {
	msg string
}

func (e *kitexError) Error() string {
	return e.msg
}
