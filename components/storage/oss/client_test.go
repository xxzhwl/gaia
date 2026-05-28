package oss

import (
	"testing"
)

func TestNewClient_MissingEndpoint(t *testing.T) {
	_, err := NewClient(Config{Bucket: "test"})
	if err == nil {
		t.Fatal("缺少 Endpoint 应报错")
	}
}

func TestNewClient_MissingBucket(t *testing.T) {
	_, err := NewClient(Config{Endpoint: "oss-cn-hangzhou.aliyuncs.com"})
	if err == nil {
		t.Fatal("缺少 Bucket 应报错")
	}
}

func TestNewClient_Construction(t *testing.T) {
	cli, err := NewClient(Config{
		Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
		Bucket:          "test-bucket",
		AccessKeyID:     "fake",
		AccessKeySecret: "fake",
	})
	if err != nil {
		t.Fatalf("构造不应失败: %v", err)
	}
	if cli.GetBucket() == nil {
		t.Fatal("GetBucket 返回 nil")
	}
}
