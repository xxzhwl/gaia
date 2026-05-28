// Package tracer exporter 工厂：根据 Config.Exporter 构造对应的 SpanExporter。
//
// 设计要点：
//  1. 每种 Exporter 一个独立 builder，互不依赖；
//  2. 默认 otlphttp 兼容 Jaeger / Tempo / SigNoz / OTel Collector；
//  3. 失败时返回 error，由上层降级为 NoopTracer。
//
// @author wanlizhan
// @created 2026-06-01
package tracer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// buildExporter 根据 cfg.Exporter 构造对应的 SpanExporter。
//
//	返回 (exporter, nil) 表示需要正常上报；
//	返回 (nil, nil) 表示当前后端为 ExporterNone，调用方应跳过 Provider 创建并使用 NoopTracer。
func buildExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	switch cfg.Exporter {
	case ExporterOTLPHTTP:
		return buildOTLPHTTPExporter(ctx, cfg)
	case ExporterOTLPGRPC:
		return buildOTLPGRPCExporter(ctx, cfg)
	case ExporterStdout:
		return buildStdoutExporter()
	case ExporterNone:
		return nil, nil
	default:
		return nil, fmt.Errorf("不支持的 exporter 类型: %s", cfg.Exporter)
	}
}

// buildOTLPHTTPExporter 通过 OTLP/HTTP 上报（默认 4318，兼容 Jaeger/Tempo/SigNoz/Collector）。
func buildOTLPHTTPExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if cfg.URLPath != "" {
		opts = append(opts, otlptracehttp.WithURLPath(cfg.URLPath))
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}
	exp, err := otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("创建 otlphttp exporter 失败: %w", err)
	}
	return exp, nil
}

// buildOTLPGRPCExporter 通过 OTLP/gRPC 上报（默认 4317，性能更高）。
func buildOTLPGRPCExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}
	exp, err := otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	if err != nil {
		return nil, fmt.Errorf("创建 otlpgrpc exporter 失败: %w", err)
	}
	return exp, nil
}

// buildStdoutExporter 输出 trace 到标准输出，仅用于本地调试。
func buildStdoutExporter() (sdktrace.SpanExporter, error) {
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, fmt.Errorf("创建 stdout exporter 失败: %w", err)
	}
	return exp, nil
}
