package mqtt

import (
	"os"
	"testing"
)

func TestNewClient_MissingBroker(t *testing.T) {
	_, err := NewClient(Config{ClientID: "test"})
	if err == nil {
		t.Fatal("缺少 Broker 应报错")
	}
}

func TestNewClient_MissingClientID(t *testing.T) {
	_, err := NewClient(Config{Broker: "tcp://localhost:1883"})
	if err == nil {
		t.Fatal("缺少 ClientID 应报错")
	}
}

func TestNewClient_InvalidBroker(t *testing.T) {
	_, err := NewClient(Config{Broker: "tcp://invalid-host:1883", ClientID: "test"})
	if err == nil {
		t.Fatal("无效 Broker 应报错")
	}
}

func TestIntegration_PubSub(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	broker := os.Getenv("GAIA_TEST_MQTT_BROKER")
	if broker == "" {
		broker = "tcp://localhost:1883"
	}

	cli, err := NewClient(Config{Broker: broker, ClientID: "gaia-test"})
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer cli.Close()

	if !cli.IsConnected() {
		t.Fatal("应该已连接")
	}
	if cli.GetCli() == nil {
		t.Fatal("GetCli 返回 nil")
	}
}
