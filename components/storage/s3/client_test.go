package s3

import (
	"testing"
)

func TestNewClient_MissingRegion(t *testing.T) {
	_, err := NewClient(Config{Bucket: "test"})
	if err == nil {
		t.Fatal("缺少 Region 应报错")
	}
}

func TestNewClient_MissingBucket(t *testing.T) {
	_, err := NewClient(Config{Region: "us-east-1"})
	if err == nil {
		t.Fatal("缺少 Bucket 应报错")
	}
}

func TestNewClient_WithEndpoint(t *testing.T) {
	// 用无效凭证测试构造逻辑（不应 panic）
	cli, err := NewClient(Config{
		Region:          "us-east-1",
		Bucket:          "test",
		AccessKeyID:     "fake",
		SecretAccessKey: "fake",
		Endpoint:        "http://localhost:9000",
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("构造不应失败（仅在请求时才验证凭证）: %v", err)
	}
	if cli.GetCli() == nil {
		t.Fatal("GetCli 返回 nil")
	}
}
