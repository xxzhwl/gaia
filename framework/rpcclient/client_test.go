package rpcclient

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
)

func TestNew_DefaultSchema(t *testing.T) {
	c := New("", nil)
	if c.schema != "RpcClient" {
		t.Fatalf("空 schema 应回退为 RpcClient，实际 %q", c.schema)
	}
	if c.conns == nil {
		t.Fatal("conns 应被初始化")
	}
	if c.registry != nil {
		t.Fatal("registry 应为 nil")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	cfg := LoadConfig("NonExistSchemaForTest")
	if cfg.DialTimeout != 10*time.Second {
		t.Fatalf("DialTimeout 默认应为 10s，实际 %v", cfg.DialTimeout)
	}
	if cfg.LoadBalancing != "round_robin" {
		t.Fatalf("LoadBalancing 默认应为 round_robin，实际 %q", cfg.LoadBalancing)
	}
	if cfg.KeepAliveTime != 30*time.Second {
		t.Fatalf("KeepAliveTime 默认应为 30s，实际 %v", cfg.KeepAliveTime)
	}
	if cfg.MaxRecvMsgSizeMB != 4 || cfg.MaxSendMsgSizeMB != 4 {
		t.Fatalf("消息大小默认应为 4MB，实际 recv=%d send=%d", cfg.MaxRecvMsgSizeMB, cfg.MaxSendMsgSizeMB)
	}
	if cfg.RetryBackoffMultiplier != 2.0 {
		t.Fatalf("RetryBackoffMultiplier 默认应为 2.0，实际 %v", cfg.RetryBackoffMultiplier)
	}
	if len(cfg.RetryableStatusCodes) != 1 || cfg.RetryableStatusCodes[0] != "UNAVAILABLE" {
		t.Fatalf("RetryableStatusCodes 默认应为 [UNAVAILABLE]，实际 %v", cfg.RetryableStatusCodes)
	}
}

func TestBuildServiceConfig_NoRetry(t *testing.T) {
	c := &GrpcClient{cfg: Config{LoadBalancing: "round_robin"}}
	sc := c.buildServiceConfig()

	var m map[string]any
	if err := json.Unmarshal([]byte(sc), &m); err != nil {
		t.Fatalf("serviceConfig 应为合法 JSON：%v（%s）", err, sc)
	}
	if strings.Contains(sc, "retryPolicy") {
		t.Fatalf("未启用重试时不应包含 retryPolicy：%s", sc)
	}
	if !strings.Contains(sc, `"round_robin"`) {
		t.Fatalf("应包含负载均衡策略：%s", sc)
	}
}

func TestBuildServiceConfig_WithRetry_ClampsAttempts(t *testing.T) {
	c := &GrpcClient{cfg: Config{
		LoadBalancing:          "round_robin",
		RetryEnable:            true,
		RetryMaxAttempts:       10, // 超过 gRPC 上限，应被钳制为 5
		RetryInitialBackoff:    "0.1s",
		RetryMaxBackoff:        "1s",
		RetryBackoffMultiplier: 2,
		RetryableStatusCodes:   []string{"UNAVAILABLE", "ABORTED"},
	}}
	sc := c.buildServiceConfig()

	var m map[string]any
	if err := json.Unmarshal([]byte(sc), &m); err != nil {
		t.Fatalf("serviceConfig 应为合法 JSON：%v（%s）", err, sc)
	}
	if !strings.Contains(sc, "retryPolicy") {
		t.Fatalf("启用重试时应包含 retryPolicy：%s", sc)
	}
	if !strings.Contains(sc, `"maxAttempts":5`) {
		t.Fatalf("maxAttempts 应被钳制为 5：%s", sc)
	}
	if !strings.Contains(sc, `"ABORTED"`) {
		t.Fatalf("应包含配置的可重试状态码：%s", sc)
	}
}

func TestMsToDurationStr(t *testing.T) {
	cases := map[int64]string{
		100:  "0.1s",
		1000: "1s",
		1500: "1.5s",
		0:    "0s",
		-5:   "0s",
	}
	for in, want := range cases {
		if got := msToDurationStr(in); got != want {
			t.Errorf("msToDurationStr(%d)=%q，期望 %q", in, got, want)
		}
	}
}

func TestJoinRetryCodes(t *testing.T) {
	if got := joinRetryCodes(nil); got != `"UNAVAILABLE"` {
		t.Errorf("空切片应回退为 UNAVAILABLE，实际 %s", got)
	}
	if got := joinRetryCodes([]string{"  unavailable ", "aborted"}); got != `"UNAVAILABLE","ABORTED"` {
		t.Errorf("应规整为大写去空格，实际 %s", got)
	}
	if got := joinRetryCodes([]string{"", "  "}); got != `"UNAVAILABLE"` {
		t.Errorf("全空应回退为 UNAVAILABLE，实际 %s", got)
	}
}

func TestBuildTransportCredentials_Insecure(t *testing.T) {
	c := &GrpcClient{cfg: Config{TLSEnable: false}}
	creds, err := c.buildTransportCredentials()
	if err != nil {
		t.Fatalf("非 TLS 不应报错：%v", err)
	}
	if creds.Info().SecurityProtocol != insecure.NewCredentials().Info().SecurityProtocol {
		t.Fatal("未启用 TLS 时应返回 insecure 凭证")
	}
}

func TestBuildTransportCredentials_BadCA(t *testing.T) {
	c := &GrpcClient{cfg: Config{TLSEnable: true, TLSCAPath: "/path/does/not/exist/ca.pem"}}
	if _, err := c.buildTransportCredentials(); err == nil {
		t.Fatal("CA 路径不存在应报错")
	}
}

func TestDial_AfterClose(t *testing.T) {
	c := New("RpcClient", nil)
	c.Close()
	if _, err := c.Dial("svc"); err == nil {
		t.Fatal("已关闭的 client 调用 Dial 应报错")
	}
	if _, err := c.DialDirect("127.0.0.1:9090"); err == nil {
		t.Fatal("已关闭的 client 调用 DialDirect 应报错")
	}
}

func TestDial_NoRegistryNoStaticAddr(t *testing.T) {
	c := New("NoSuchSchemaForTest", nil)
	if _, err := c.Dial("UnknownService"); err == nil {
		t.Fatal("无 registry 且无静态地址配置时应报错")
	}
}

func TestCloseService_NotExist(t *testing.T) {
	c := New("RpcClient", nil)
	if err := c.CloseService("not-exist"); err != nil {
		t.Fatalf("关闭不存在的连接应返回 nil，实际 %v", err)
	}
}

func TestClose_Idempotent(t *testing.T) {
	c := New("RpcClient", nil)
	c.Close()
	c.Close() // 重复调用不应 panic
	if !c.closed.Load() {
		t.Fatal("Close 后 closed 应为 true")
	}
}
