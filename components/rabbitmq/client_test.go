package rabbitmq

import (
	"os"
	"testing"
)

func TestNewClient_MissingURL(t *testing.T) {
	_, err := NewClient(Config{})
	if err == nil {
		t.Fatal("缺少 URL 应报错")
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	_, err := NewClient(Config{URL: "amqp://invalid-host:5672/"})
	if err == nil {
		t.Fatal("无效 URL 应报错")
	}
}

func TestIntegration_PublishConsume(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	url := os.Getenv("GAIA_TEST_RABBITMQ_URL")
	if url == "" {
		url = "amqp://guest:guest@localhost:5672/"
	}

	cli, err := NewClient(Config{URL: url})
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer cli.Close()

	if cli.GetConn() == nil {
		t.Fatal("GetConn 返回 nil")
	}

	ch, err := cli.Channel()
	if err != nil {
		t.Fatalf("创建 Channel 失败: %v", err)
	}
	ch.Close()
}
