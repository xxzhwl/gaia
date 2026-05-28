package rpcserver

import (
	"context"
	"strings"
	"testing"

	"github.com/xxzhwl/gaia"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
)

func TestLoadLoggerOptions_Defaults(t *testing.T) {
	opts := loadLoggerOptions("NonExistSchemaForTest")
	if !opts.printConsole {
		t.Fatal("printConsole 默认应为 true（避免静默无日志）")
	}
	if opts.detailMode || opts.enablePushLog || opts.logBody {
		t.Fatal("detailMode/enablePushLog/logBody 默认应为 false")
	}
	if opts.maxBodyLogBytes != 4096 {
		t.Fatalf("maxBodyLogBytes 默认应为 4096，实际 %d", opts.maxBodyLogBytes)
	}
}

func TestGrpcCodeToLevel(t *testing.T) {
	cases := map[grpccodes.Code]gaia.LogLevel{
		grpccodes.OK:                gaia.LogInfoLevel,
		grpccodes.InvalidArgument:   gaia.LogWarnLevel,
		grpccodes.Unauthenticated:   gaia.LogWarnLevel,
		grpccodes.ResourceExhausted: gaia.LogWarnLevel,
		grpccodes.NotFound:          gaia.LogWarnLevel,
		grpccodes.Internal:          gaia.LogErrorLevel,
		grpccodes.Unavailable:       gaia.LogErrorLevel,
		grpccodes.DataLoss:          gaia.LogErrorLevel,
		grpccodes.Unknown:           gaia.LogErrorLevel,
	}
	for code, want := range cases {
		if got := grpcCodeToLevel(code); got != want {
			t.Errorf("grpcCodeToLevel(%s)=%v，期望 %v", code, got, want)
		}
	}
}

func TestSanitizeRpcPayload(t *testing.T) {
	// nil → 空
	if s := sanitizeRpcPayload(loggerOptions{logBody: true}, nil); s != "" {
		t.Fatalf("nil 应返回空串，实际 %q", s)
	}
	// logBody=false → 占位
	if s := sanitizeRpcPayload(loggerOptions{logBody: false}, map[string]int{"a": 1}); s != "[REDACTED]" {
		t.Fatalf("logBody=false 应返回 [REDACTED]，实际 %q", s)
	}
	// 正常 JSON
	s := sanitizeRpcPayload(loggerOptions{logBody: true, maxBodyLogBytes: 4096}, map[string]int{"a": 1})
	if !strings.Contains(s, `"a":1`) {
		t.Fatalf("应输出 JSON，实际 %q", s)
	}
	// 截断
	long := strings.Repeat("x", 100)
	out := sanitizeRpcPayload(loggerOptions{logBody: true, maxBodyLogBytes: 10}, long)
	if !strings.Contains(out, "truncated") {
		t.Fatalf("超长应被截断，实际 %q", out)
	}
}

func TestGrpcTraceFromMD(t *testing.T) {
	// 无 metadata
	if id, ff := grpcTraceFromMD(context.Background()); id != "" || ff != "" {
		t.Fatalf("无 metadata 应返回空，实际 id=%q ff=%q", id, ff)
	}
	// 有 metadata
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(mdTraceIDKey, "trace-123", mdFollowFromKey, "upstream-svc"))
	id, ff := grpcTraceFromMD(ctx)
	if id != "trace-123" || ff != "upstream-svc" {
		t.Fatalf("应读出上游链路信息，实际 id=%q ff=%q", id, ff)
	}
}

func TestMdToHeader_Sanitizes(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "Bearer secret", "x-biz", "v1"))
	h := mdToHeader(ctx)
	// metadata 的 key 为小写，转换后保留原始小写 key（贴近 HTTP/2 wire 格式），
	// 故直接按 map 访问而非用 http.Header.Get（后者会做 canonical 规范化）。
	if got := h["authorization"]; len(got) == 0 || got[0] != "[REDACTED]" {
		t.Fatalf("敏感头应被脱敏，实际 %v", h["authorization"])
	}
	if got := h["x-biz"]; len(got) == 0 || got[0] != "v1" {
		t.Fatalf("普通头应保留，实际 %v", h["x-biz"])
	}
}
