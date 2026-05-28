// Package metrics OTLP gRPC push 后端实现。
// @author wanlizhan
// @created 2026/05/28
package metrics

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

// buildOTLPProvider 构造 OTLP gRPC push 模式的 MeterProvider。
func buildOTLPProvider(ctx context.Context, cfg Config, res *resource.Resource) (
	*sdkmetric.MeterProvider, func(context.Context) error, func(context.Context) error, error,
) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
	}
	if cfg.OTLPInsecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}

	exporter, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create otlp metric exporter: %w", err)
	}

	reader := sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(cfg.OTLPPushInterval),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)

	return mp, mp.Shutdown, nil, nil
}
