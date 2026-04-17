// Package rpcserver gRPC 拦截器链
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"context"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// ===== 1. Panic 恢复 =====

func GrpcRecoveryUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				errMsg := gaia.PanicLog(r)
				err = status.Errorf(grpccodes.Internal, "服务器内部错误: %s", errMsg)
			}
		}()
		return handler(ctx, req)
	}
}

func GrpcRecoveryStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				errMsg := gaia.PanicLog(r)
				err = status.Errorf(grpccodes.Internal, "服务器内部错误: %s", errMsg)
			}
		}()
		return handler(srv, ss)
	}
}

// ===== 2. 日志记录 =====

func GrpcLoggingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		startTime := time.Now()
		clientIP := grpcExtractClientIP(ctx)

		gaia.BuildContextTrace()
		defer gaia.RemoveContextTrace()

		resp, err = handler(ctx, req)
		duration := time.Since(startTime)

		if err != nil {
			st, _ := status.FromError(err)
			rpcLogger.WarnF("[gRPC] %s | %s | %v | code=%s | %s",
				info.FullMethod, clientIP, duration, st.Code(), st.Message())
		} else {
			rpcLogger.InfoF("[gRPC] %s | %s | %v | OK",
				info.FullMethod, clientIP, duration)
		}
		return resp, err
	}
}

func GrpcLoggingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		startTime := time.Now()
		clientIP := grpcExtractClientIP(ss.Context())

		gaia.BuildContextTrace()
		defer gaia.RemoveContextTrace()

		err := handler(srv, ss)
		duration := time.Since(startTime)

		if err != nil {
			st, _ := status.FromError(err)
			rpcLogger.WarnF("[gRPC-Stream] %s | %s | %v | code=%s | %s",
				info.FullMethod, clientIP, duration, st.Code(), st.Message())
		} else {
			rpcLogger.InfoF("[gRPC-Stream] %s | %s | %v | OK",
				info.FullMethod, clientIP, duration)
		}
		return err
	}
}

// ===== 3. 链路追踪 =====

func GrpcTracingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		tr := tracer.GetTracer()
		if tr == nil {
			return handler(ctx, req)
		}

		ctx = grpcExtractTraceContext(ctx)
		ctx, span := tr.Start(ctx, info.FullMethod,
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
			oteltrace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", info.FullMethod),
				attribute.String("rpc.client_ip", grpcExtractClientIP(ctx)),
			),
		)
		defer span.End()

		if gaiaCtx := gaia.GetContextTrace(); gaiaCtx != nil {
			gaiaCtx.ParentContext = ctx
		}

		resp, err = handler(ctx, req)
		if err != nil {
			st, _ := status.FromError(err)
			span.SetStatus(codes.Error, st.Message())
			span.SetAttributes(attribute.String("rpc.grpc.status_code", st.Code().String()))
		} else {
			span.SetStatus(codes.Ok, "")
			span.SetAttributes(attribute.String("rpc.grpc.status_code", grpccodes.OK.String()))
		}
		return resp, err
	}
}

func GrpcTracingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		tr := tracer.GetTracer()
		if tr == nil {
			return handler(srv, ss)
		}

		ctx := grpcExtractTraceContext(ss.Context())
		ctx, span := tr.Start(ctx, info.FullMethod,
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
			oteltrace.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", info.FullMethod),
				attribute.Bool("rpc.stream", true),
			),
		)
		defer span.End()

		wrappedStream := &grpcWrappedServerStream{ServerStream: ss, ctx: ctx}
		err := handler(srv, wrappedStream)

		if err != nil {
			st, _ := status.FromError(err)
			span.SetStatus(codes.Error, st.Message())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return err
	}
}

type grpcWrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *grpcWrappedServerStream) Context() context.Context {
	return w.ctx
}

// ===== 4. 限流 =====

var (
	grpcLimiters   = make(map[string]*rpcRateLimiter)
	grpcLimitersMu sync.RWMutex
)

func getGrpcRateLimiter(key string, capacity int, rate float64) *rpcRateLimiter {
	grpcLimitersMu.RLock()
	rl, ok := grpcLimiters[key]
	grpcLimitersMu.RUnlock()
	if ok {
		return rl
	}
	grpcLimitersMu.Lock()
	defer grpcLimitersMu.Unlock()
	if rl, ok := grpcLimiters[key]; ok {
		return rl
	}
	rl = newRpcRateLimiter(capacity, rate)
	grpcLimiters[key] = rl
	return rl
}

func GrpcRateLimitUnaryInterceptor(schema string) grpc.UnaryServerInterceptor {
	capacity := int(gaia.GetSafeConfInt64WithDefault(schema+".RateLimit.Capacity", 200))
	rate := gaia.GetSafeConfFloat64WithDefault(schema+".RateLimit.Rate", 100.0)
	enabled := gaia.GetSafeConfBool(schema + ".RateLimit.Enable")

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !enabled {
			return handler(ctx, req)
		}
		key := info.FullMethod + "|" + grpcExtractClientIP(ctx)
		rl := getGrpcRateLimiter(key, capacity, rate)
		if !rl.allow() {
			return nil, status.Errorf(grpccodes.ResourceExhausted, "请求过于频繁，请稍后再试")
		}
		return handler(ctx, req)
	}
}

func GrpcRateLimitStreamInterceptor(schema string) grpc.StreamServerInterceptor {
	capacity := int(gaia.GetSafeConfInt64WithDefault(schema+".RateLimit.Capacity", 200))
	rate := gaia.GetSafeConfFloat64WithDefault(schema+".RateLimit.Rate", 100.0)
	enabled := gaia.GetSafeConfBool(schema + ".RateLimit.Enable")

	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !enabled {
			return handler(srv, ss)
		}
		key := info.FullMethod + "|" + grpcExtractClientIP(ss.Context())
		rl := getGrpcRateLimiter(key, capacity, rate)
		if !rl.allow() {
			return status.Errorf(grpccodes.ResourceExhausted, "请求过于频繁，请稍后再试")
		}
		return handler(srv, ss)
	}
}

// ===== gRPC 辅助函数 =====

func grpcExtractClientIP(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return "unknown"
}

func grpcExtractTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	if vals := md.Get("traceparent"); len(vals) > 0 {
		ctx = metadata.NewIncomingContext(ctx, md)
	}
	if vals := md.Get("x-trace-id"); len(vals) > 0 {
		if gaiaCtx := gaia.GetContextTrace(); gaiaCtx != nil {
			gaiaCtx.TraceId = vals[0]
		}
	}
	return ctx
}
