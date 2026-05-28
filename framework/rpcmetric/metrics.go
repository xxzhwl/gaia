// Package rpcmetric 为 gRPC 服务端（framework/rpcserver）与客户端
// （framework/rpcclient）提供共享的指标基元，避免标签 key 与记录逻辑
// 在两个包里各写一份导致漂移。
//
// 设计原则：
//   - 只持有「通用」部分（请求计数 + 耗时直方图 + 公共标签 key）；
//     server 专有的 in_flight（UpDownCounter）/ panic 处理仍留在 rpcserver。
//   - 不依赖任何 gaia rpc 包，故 server/client 同时 import 也不构成循环依赖。
//   - 指标实例由各自包用 otel.Meter(<自己的 scope>) 创建后注入，
//     instrumentation scope 仍区分 server/client，便于在后端区分来源。
//
// @author gaia-framework
// @created 2026-06-25
package rpcmetric

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// 公共指标标签 key（server/client 单一事实来源，避免拼写漂移）。
const (
	// LabelMethod gRPC 全限定方法名，如 /pkg.Svc/Method。
	LabelMethod = "rpc_method"
	// LabelCode gRPC status code 字符串，如 OK / NotFound / Internal。
	LabelCode = "rpc_code"
	// LabelStream 是否为流式方法（仅服务端使用）。
	LabelStream = "rpc_stream"
)

// DurationMS 返回从 start 至今的毫秒数，保留微秒精度（便于 P95/P99 直方图）。
func DurationMS(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}

// Recorder 封装 gRPC 调用通用的两个指标：请求计数 + 耗时直方图。
// server/client 共用其 Record 逻辑；零值安全（instrument 为 nil 时为 noop）。
type Recorder struct {
	RequestsTotal metric.Int64Counter
	Duration      metric.Float64Histogram
}

// Record 记录一次调用：requestsTotal+1 与 duration（毫秒）。
//
//	method/code 为通用标签；extra 供调用方追加专有标签（如服务端的 stream）。
//
// instrument 为 nil（未初始化）时安全跳过，不 panic。
func (r Recorder) Record(ctx context.Context, method, code string, start time.Time, extra ...attribute.KeyValue) {
	kvs := make([]attribute.KeyValue, 0, 2+len(extra))
	kvs = append(kvs,
		attribute.String(LabelMethod, method),
		attribute.String(LabelCode, code),
	)
	kvs = append(kvs, extra...)
	attrs := metric.WithAttributes(kvs...)

	if r.RequestsTotal != nil {
		r.RequestsTotal.Add(ctx, 1, attrs)
	}
	if r.Duration != nil {
		r.Duration.Record(ctx, DurationMS(start), attrs)
	}
}
