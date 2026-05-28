// Package tracer 包注释
// @author wanlizhan
// @created 2024-12-10
package tracer

import (
	"context"
	"fmt"
	"sync"

	"github.com/xxzhwl/gaia"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
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

// SetupTracer 设置 Tracer（使用配置中心 + 环境变量加载配置）。
//
//	serviceName: 服务名（用于 resource.ServiceName 标签），为空时回退到 gaia.GetSystemEnName()。
//
// 重复调用是幂等的。失败时降级为 NoopTracer，并返回 nil error，不影响业务进程。
func SetupTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	cfg := LoadConfig(serviceName)
	return SetupTracerWithConfig(ctx, cfg)
}

// SetupTracerWithConfig 使用显式 Config 初始化追踪系统，绕过配置中心。
//
// 重复调用是幂等的：第二次起直接返回首次的 shutdown 句柄。
func SetupTracerWithConfig(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	mu.Lock()
	defer mu.Unlock()

	if isInitialized {
		return shutdownFunc, nil
	}

	// 完全关闭：直接走 NoopTracer
	if !cfg.Enabled || cfg.Exporter == ExporterNone {
		setupNoop(cfg.ServiceName)
		gaia.InfoF("追踪系统已禁用 (Enabled=%v, Exporter=%s)，使用 NoopTracer", cfg.Enabled, cfg.Exporter)
		return shutdownFunc, nil
	}

	tp, err := newTraceProvider(ctx, cfg)
	if err != nil {
		gaia.WarnF("初始化追踪系统失败，将使用 NoopTracer: %v", err)
		setupNoop(cfg.ServiceName)
		return shutdownFunc, nil
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	LocalTrace = otel.Tracer(cfg.ServiceName)
	shutdownFunc = tp.Shutdown
	isInitialized = true

	gaia.InfoF("追踪系统初始化成功: Service=%s, Exporter=%s, Endpoint=%s, SampleRate=%.2f",
		cfg.ServiceName, cfg.Exporter, cfg.Endpoint, cfg.SampleRate)

	return shutdownFunc, nil
}

// setupNoop 配置 NoopTracer 作为降级 / disabled 路径。
// 调用方需持有 mu 写锁。
func setupNoop(serviceName string) {
	otel.SetTracerProvider(trace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	if serviceName == "" {
		serviceName = "gaia-app"
	}
	LocalTrace = otel.Tracer(serviceName)
	shutdownFunc = func(context.Context) error { return nil }
	isInitialized = true
}

// newTraceProvider 创建一个 Trace Provider（根据 cfg.Exporter 选择具体后端）。
func newTraceProvider(ctx context.Context, cfg Config) (*trace.TracerProvider, error) {
	exp, err := buildExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if exp == nil {
		return nil, fmt.Errorf("exporter 为 nil")
	}

	// 创建资源
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("1.0.0"),
			semconv.DeploymentEnvironment(gaia.GetSafeConfStringWithDefault("Environment", "development")),
		))
	if err != nil {
		return nil, fmt.Errorf("创建resource失败: %w", err)
	}

	// 创建采样器
	//
	// 设计：用 ParentBased 包裹本地采样器——
	//  1) 上游已下决心采/不采（traceparent 里的 sampled flag）时，下游必须遵从，
	//     否则跨服务调用链会断掉一半（采样到一半被裁剪）；
	//  2) 仅当本服务是链路根（无父 span）时，才用本地配置的比例随机采样；
	//  3) 采样比例边界值用 AlwaysSample / NeverSample 走快路径，省一次哈希计算。
	var localSampler trace.Sampler
	switch {
	case cfg.SampleRate >= 1.0:
		localSampler = trace.AlwaysSample()
	case cfg.SampleRate <= 0:
		localSampler = trace.NeverSample()
	default:
		localSampler = trace.TraceIDRatioBased(cfg.SampleRate)
	}
	sampler := trace.ParentBased(localSampler)

	// 创建批处理器
	bsp := trace.NewBatchSpanProcessor(exp,
		trace.WithBatchTimeout(cfg.BatchTimeout),
		trace.WithMaxQueueSize(cfg.MaxQueueSize),
		trace.WithMaxExportBatchSize(cfg.MaxExportBatchSize),
	)

	return trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithSampler(sampler),
		trace.WithSpanProcessor(bsp),
	), nil
}

// GetTracer 获取Tracer实例
func GetTracer() ttrace.Tracer {
	mu.RLock()
	t := LocalTrace
	mu.RUnlock()
	if t != nil {
		return t
	}
	// 双重检查：升级为写锁
	mu.Lock()
	defer mu.Unlock()
	if LocalTrace == nil {
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
