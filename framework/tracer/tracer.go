// Package tracer 包注释
// @author wanlizhan
// @created 2024-12-10
package tracer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	ttrace "go.opentelemetry.io/otel/trace"
)

var (
	LocalTrace    ttrace.Tracer
	shutdownFunc  func(context.Context) error
	mu            sync.RWMutex
	isInitialized bool
)

// TraceConfig 追踪配置
type TraceConfig struct {
	ServiceName        string
	Endpoint           string
	Insecure           bool
	SampleRate         float64
	BatchTimeout       time.Duration
	MaxQueueSize       int
	MaxExportBatchSize int
}

// SetupTracer 设置Tracer
func SetupTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	// 避免重复初始化
	mu.Lock()
	defer mu.Unlock()

	if isInitialized {
		return shutdownFunc, nil
	}

	// 构建配置
	config := TraceConfig{
		ServiceName:        serviceName,
		Endpoint:           gaia.GetSafeConfStringWithDefault("Framework.JaegerTracePoint", "localhost:4318"),
		Insecure:           true,
		SampleRate:         gaia.GetSafeConfFloat64WithDefault("Framework.TraceSampleRate", 1.0),
		BatchTimeout:       time.Second * time.Duration(gaia.GetSafeConfInt64WithDefault("Framework.TraceBatchTimeout", 1)),
		MaxQueueSize:       int(gaia.GetSafeConfInt64WithDefault("Framework.TraceMaxQueueSize", 1024)),
		MaxExportBatchSize: int(gaia.GetSafeConfInt64WithDefault("Framework.TraceMaxExportBatchSize", 512)),
	}

	tracerProvider, err := newTraceProvider(ctx, config)
	if err != nil {
		gaia.WarnF("初始化追踪系统失败，将使用NoopTracer: %v", err)
		// 使用NoopTracer作为降级策略
		otel.SetTracerProvider(trace.NewTracerProvider())
		LocalTrace = otel.Tracer(serviceName)
		isInitialized = true
		return func(context.Context) error { return nil }, nil
	}

	otel.SetTracerProvider(tracerProvider)
	LocalTrace = otel.Tracer(serviceName)
	shutdownFunc = tracerProvider.Shutdown
	isInitialized = true

	gaia.InfoF("追踪系统初始化成功: Service=%s, Endpoint=%s, SampleRate=%.2f",
		config.ServiceName, config.Endpoint, config.SampleRate)

	return shutdownFunc, nil
}

// newTraceProvider 创建一个 Trace Provider
func newTraceProvider(ctx context.Context, config TraceConfig) (*trace.TracerProvider, error) {
	// 创建一个使用 HTTP 协议连接Jaeger的 Exporter
	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(config.Endpoint),
		otlptracehttp.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("创建exporter失败: %w", err)
	}

	// 创建资源
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion("1.0.0"),
			semconv.DeploymentEnvironment(gaia.GetSafeConfStringWithDefault("Environment", "development")),
		))
	if err != nil {
		return nil, fmt.Errorf("创建resource失败: %w", err)
	}

	// 创建采样器
	var sampler trace.Sampler
	if config.SampleRate >= 1.0 {
		sampler = trace.AlwaysSample()
	} else if config.SampleRate <= 0 {
		sampler = trace.NeverSample()
	} else {
		sampler = trace.TraceIDRatioBased(config.SampleRate)
	}

	// 创建批处理器
	bsp := trace.NewBatchSpanProcessor(exp,
		trace.WithBatchTimeout(config.BatchTimeout),
		trace.WithMaxQueueSize(config.MaxQueueSize),
		trace.WithMaxExportBatchSize(config.MaxExportBatchSize),
	)

	// 创建TraceProvider
	traceProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSampler(sampler),
		trace.WithSpanProcessor(bsp),
	)

	return traceProvider, nil
}

// GetTracer 获取Tracer实例
func GetTracer() ttrace.Tracer {
	mu.RLock()
	defer mu.RUnlock()

	if LocalTrace == nil {
		// 确保至少有一个NoopTracer
		LocalTrace = otel.Tracer("default")
	}

	return LocalTrace
}

// ShutdownTracer 关闭追踪系统
func ShutdownTracer(ctx context.Context) error {
	mu.Lock()
	defer mu.Unlock()

	if shutdownFunc != nil {
		err := shutdownFunc(ctx)
		if err != nil {
			return fmt.Errorf("关闭追踪系统失败: %w", err)
		}
		gaia.Info("追踪系统已关闭")
	}

	isInitialized = false
	LocalTrace = nil
	shutdownFunc = nil

	return nil
}

// IsInitialized 检查追踪系统是否已初始化
func IsInitialized() bool {
	mu.RLock()
	defer mu.RUnlock()

	return isInitialized
}
