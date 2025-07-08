// Package tracer 包注释
// @author wanlizhan
// @created 2024-12-10
package tracer

import (
	"context"
	"github.com/xxzhwl/gaia"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	ttrace "go.opentelemetry.io/otel/trace"
	"time"
)

var LocalTrace ttrace.Tracer

// SetupTracer 设置Tracer
func SetupTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	tracerProvider, err := newJaegerTraceProvider(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	otel.SetTracerProvider(tracerProvider)
	return tracerProvider.Shutdown, nil
}

// newJaegerTraceProvider 创建一个 Jaeger Trace Provider
func newJaegerTraceProvider(ctx context.Context, serviceName string) (*trace.TracerProvider, error) {
	// 创建一个使用 HTTP 协议连接本机Jaeger的 Exporter
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(gaia.GetSafeConfStringWithDefault("Framework.JaegerTracePoint", "localhost:4318")),
		otlptracehttp.WithInsecure())
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(serviceName)))
	if err != nil {
		return nil, err
	}
	traceProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSampler(trace.AlwaysSample()), // 采样
		trace.WithBatcher(exp, trace.WithBatchTimeout(time.Second)),
	)
	return traceProvider, nil
}

func GetTracer() ttrace.Tracer {
	return LocalTrace
}
