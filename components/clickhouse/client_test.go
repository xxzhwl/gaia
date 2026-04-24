package clickhouse

import (
	"context"
	"os"
	"testing"
)

func TestNewClient_MissingAddrs(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("缺少 Addrs 应报错")
	}
}

func TestNewClient_InvalidAddr(t *testing.T) {
	_, err := NewClient(Config{Addrs: []string{"invalid-host:9000"}, Database: "default"})
	if err == nil {
		t.Fatal("无效地址应报错")
	}
}

func TestIntegration_Query(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	cli, err := NewClient(Config{
		Addrs:    []string{"127.0.0.1:9000"},
		Database: "default",
	})
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer cli.Close()

	if err := cli.Ping(context.Background()); err != nil {
		t.Fatalf("Ping 失败: %v", err)
	}

	if cli.GetConn() == nil {
		t.Fatal("GetConn 返回 nil")
	}
}
