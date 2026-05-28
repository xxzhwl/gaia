package rpcclient

import (
	"context"
	"testing"

	"github.com/xxzhwl/gaia"
	"google.golang.org/grpc/metadata"
)

func TestInjectGaiaTrace_WithContext(t *testing.T) {
	gaia.BuildContextTrace()
	defer gaia.RemoveContextTrace()
	gc := gaia.GetContextTrace()
	if gc == nil {
		t.Skip("当前环境无法构建 gaia 链路上下文，跳过")
	}

	ctx := injectGaiaTrace(context.Background())
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("应写入 outgoing metadata")
	}
	got := md.Get("traceid")
	if len(got) == 0 || got[0] != gc.TraceId {
		t.Fatalf("应注入 gaia TraceId=%q，实际 %v", gc.TraceId, got)
	}
}

func TestInjectGaiaTrace_NoContext(t *testing.T) {
	// 确保当前 goroutine 无残留 trace 上下文
	gaia.RemoveContextTrace()

	ctx := injectGaiaTrace(context.Background())
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		t.Fatal("即使无 trace 也应返回带 outgoing metadata 的 context")
	}
	if len(md.Get("traceid")) != 0 {
		t.Fatalf("无 trace 上下文时不应注入 traceid，实际 %v", md.Get("traceid"))
	}
}

func TestPropagationCarrier_SetGetKeys(t *testing.T) {
	c := propagationCarrier{md: metadata.MD{}}
	c.Set("traceparent", "00-abc-def-01")
	if c.Get("traceparent") != "00-abc-def-01" {
		t.Fatalf("Get 应返回 Set 的值，实际 %q", c.Get("traceparent"))
	}
	if len(c.Keys()) != 1 || c.Keys()[0] != "traceparent" {
		t.Fatalf("Keys 应包含已设置的 key，实际 %v", c.Keys())
	}
	if c.Get("missing") != "" {
		t.Fatal("不存在的 key 应返回空串")
	}
}
