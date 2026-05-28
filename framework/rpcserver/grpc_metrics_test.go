package rpcserver

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

// TestGrpcMetricsUnary_Normal 正常路径返回值透传。
func TestGrpcMetricsUnary_Normal(t *testing.T) {
	itc := GrpcMetricsUnaryInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/svc/M"}
	resp, err := itc(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
		return "ok", nil
	})
	if err != nil || resp != "ok" {
		t.Fatalf("期望 resp=ok err=nil，实际 resp=%v err=%v", resp, err)
	}
}

// TestGrpcMetricsUnary_RePanic handler panic 时拦截器应在收口指标后重新抛出 panic，
// 交给外层 Recovery 处理（验证 in_flight 收口 + 不吞 panic）。
func TestGrpcMetricsUnary_RePanic(t *testing.T) {
	itc := GrpcMetricsUnaryInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/svc/M"}

	didPanic := func() (p bool) {
		defer func() {
			if r := recover(); r != nil {
				p = true
			}
		}()
		_, _ = itc(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
			panic("boom")
		})
		return false
	}()

	if !didPanic {
		t.Fatal("metrics 拦截器应重新抛出 handler 的 panic")
	}
}

// TestGrpcRecoveryWithMetrics_Chain Recovery 包裹 Metrics 时，handler panic 被转换为 Internal error，
// 且不再向外传播（验证两个拦截器协作）。
func TestGrpcRecoveryWithMetrics_Chain(t *testing.T) {
	recovery := GrpcRecoveryUnaryInterceptor()
	metricsItc := GrpcMetricsUnaryInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/svc/M"}

	// 组合：recovery(metrics(handler))
	_, err := recovery(context.Background(), "req", info, func(ctx context.Context, req any) (any, error) {
		return metricsItc(ctx, req, info, func(ctx context.Context, req any) (any, error) {
			panic("boom")
		})
	})
	if err == nil {
		t.Fatal("Recovery 应把 panic 转为 error")
	}
}
