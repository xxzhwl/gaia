// Package rpcserver gRPC 服务端指标拦截器。
//
// 基于 OTel Meter（与 framework/metrics 共用全局 MeterProvider）。
// 未配置 metrics 时所有 Record/Add 安全降级为 noop，不影响业务。
//
// 暴露指标：
//
//	rpc.server.requests.total      Counter   按 method + code 统计请求数
//	rpc.server.duration            Histogram 服务端处理耗时（ms），按 method + code
//	rpc.server.in_flight           UpDownCounter 当前并发处理中的请求数，按 method
//
// 注意：刻意不把 client_ip 这类高基数字段作为指标标签，避免时序爆炸。
//
// @author gaia-framework
// @created 2026-06-24
package rpcserver

import (
	"context"
	"sync"
	"time"

	"github.com/xxzhwl/gaia/framework/rpcmetric"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	serverMetricsOnce sync.Once
	serverMetrics     *grpcServerMetrics
)

type grpcServerMetrics struct {
	// rec 持有 server/client 通用的 requestsTotal + duration（见 framework/rpcmetric）。
	rec rpcmetric.Recorder
	// inFlight 为服务端专有指标（客户端无此语义），故不放进共享 Recorder。
	inFlight metric.Int64UpDownCounter
}

func getServerMetrics() *grpcServerMetrics {
	serverMetricsOnce.Do(func() {
		meter := otel.Meter("github.com/xxzhwl/gaia/framework/rpcserver",
			metric.WithInstrumentationVersion("1.0.0"),
		)
		m := &grpcServerMetrics{}
		var err error

		m.rec.RequestsTotal, err = meter.Int64Counter("rpc.server.requests.total",
			metric.WithDescription("Total number of gRPC server requests by method and status code"),
		)
		otel.Handle(err)

		m.rec.Duration, err = meter.Float64Histogram("rpc.server.duration",
			metric.WithDescription("gRPC server handling duration in milliseconds"),
			metric.WithUnit("ms"),
		)
		otel.Handle(err)

		m.inFlight, err = meter.Int64UpDownCounter("rpc.server.in_flight",
			metric.WithDescription("Number of in-flight gRPC server requests by method"),
		)
		otel.Handle(err)

		serverMetrics = m
	})
	return serverMetrics
}

// recordMetrics 在 defer 中统一收口：保证 in_flight 计数即使 handler panic
// 也能正确 -1（否则只增不减），同时记录请求总数与耗时。
//
// 通过命名返回值 err 拿到 handler 的错误；panic 时 err 为 nil，故额外接收
// recovered 用于把 code 标记为 Internal，更贴近真实结果。
func (m *grpcServerMetrics) recordMetrics(
	ctx context.Context, fullMethod string, start time.Time,
	methodAttr metric.MeasurementOption, stream bool, err error, recovered any,
) {
	m.inFlight.Add(ctx, -1, methodAttr)

	code := status.Code(err).String()
	if recovered != nil {
		code = grpccodes.Internal.String()
	}
	var extra []attribute.KeyValue
	if stream {
		extra = append(extra, attribute.Bool(rpcmetric.LabelStream, true))
	}
	m.rec.Record(ctx, fullMethod, code, start, extra...)
}

// GrpcMetricsUnaryInterceptor 一元方法指标拦截器。
func GrpcMetricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	m := getServerMetrics()
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		methodAttr := metric.WithAttributes(attribute.String(rpcmetric.LabelMethod, info.FullMethod))
		m.inFlight.Add(ctx, 1, methodAttr)
		start := time.Now()

		defer func() {
			r := recover()
			m.recordMetrics(ctx, info.FullMethod, start, methodAttr, false, err, r)
			if r != nil {
				// 交还给外层 Recovery 拦截器统一处理，避免吞掉 panic
				panic(r)
			}
		}()

		resp, err = handler(ctx, req)
		return resp, err
	}
}

// GrpcMetricsStreamInterceptor 流式方法指标拦截器。
func GrpcMetricsStreamInterceptor() grpc.StreamServerInterceptor {
	m := getServerMetrics()
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		ctx := ss.Context()
		methodAttr := metric.WithAttributes(attribute.String(rpcmetric.LabelMethod, info.FullMethod))
		m.inFlight.Add(ctx, 1, methodAttr)
		start := time.Now()

		defer func() {
			r := recover()
			m.recordMetrics(ctx, info.FullMethod, start, methodAttr, true, err, r)
			if r != nil {
				panic(r)
			}
		}()

		err = handler(srv, ss)
		return err
	}
}
