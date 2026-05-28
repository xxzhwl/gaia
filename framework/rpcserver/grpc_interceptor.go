// Package rpcserver gRPC 拦截器链
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"context"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
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

// 注：日志拦截器（GrpcLoggingUnaryInterceptor / GrpcLoggingStreamInterceptor）
// 已迁移至 grpc_logger.go，并负责构建/续接 gaia 链路上下文。

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

		if gaiaCtx := gaia.GetContextTrace(); gaiaCtx != nil {
			gaiaCtx.ParentContext = ctx
		}

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

// rateLimitOptions 限流配置（拦截器创建时一次性读取）。
//
// 配置：
//
//	{schema}.RateLimit.Enable     bool    是否启用
//	{schema}.RateLimit.Capacity   int     令牌桶容量（突发上限），默认 200
//	{schema}.RateLimit.Rate       float   令牌生成速率（个/秒），默认 100
//	{schema}.RateLimit.PerClient  bool    是否按客户端 IP 维度限流，默认 false（按 method）
//	{schema}.RateLimit.MaxKeys    int     PerClient 时限流器 key 上限（LRU 淘汰），默认 4096
type rateLimitOptions struct {
	enabled   bool
	perClient bool
	reg       *limiterRegistry
}

func loadRateLimitOptions(schema string) rateLimitOptions {
	capacity := int(gaia.GetSafeConfInt64WithDefault(schema+".RateLimit.Capacity", 200))
	rate := gaia.GetSafeConfFloat64WithDefault(schema+".RateLimit.Rate", 100.0)
	maxKeys := int(gaia.GetSafeConfInt64WithDefault(schema+".RateLimit.MaxKeys", 4096))
	return rateLimitOptions{
		enabled:   gaia.GetSafeConfBool(schema + ".RateLimit.Enable"),
		perClient: gaia.GetSafeConfBool(schema + ".RateLimit.PerClient"),
		reg:       newLimiterRegistry(maxKeys, capacity, rate),
	}
}

// limiterKey 根据配置决定限流维度：默认按 method（key 有界），
// PerClient 时按 method|ip（带 LRU 上限，避免高基数 IP 撑爆内存）。
func (o rateLimitOptions) limiterKey(fullMethod, clientIP string) string {
	if o.perClient {
		return fullMethod + "|" + clientIP
	}
	return fullMethod
}

func GrpcRateLimitUnaryInterceptor(schema string) grpc.UnaryServerInterceptor {
	o := loadRateLimitOptions(schema)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !o.enabled {
			return handler(ctx, req)
		}
		rl := o.reg.get(o.limiterKey(info.FullMethod, grpcExtractClientIP(ctx)))
		if !rl.allow() {
			return nil, status.Errorf(grpccodes.ResourceExhausted, "请求过于频繁，请稍后再试")
		}
		return handler(ctx, req)
	}
}

func GrpcRateLimitStreamInterceptor(schema string) grpc.StreamServerInterceptor {
	o := loadRateLimitOptions(schema)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !o.enabled {
			return handler(srv, ss)
		}
		rl := o.reg.get(o.limiterKey(info.FullMethod, grpcExtractClientIP(ss.Context())))
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

	carrier := propagation.MapCarrier{}
	for key, values := range md {
		if len(values) > 0 {
			carrier[key] = values[0]
		}
	}
	// 仅提取 OTel 链路上下文；gaia 自有的 TraceId 续接由日志拦截器（grpc_logger.go）
	// 统一负责，避免两套体系互相覆盖导致 TraceId 不一致。
	ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
	return ctx
}
