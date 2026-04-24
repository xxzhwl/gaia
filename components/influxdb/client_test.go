package influxdb

import (
	"testing"
)

func TestNewClient_MissingURL(t *testing.T) {
	_, err := NewClient(Config{Token: "test"})
	if err == nil {
		t.Fatal("缺少 URL 应报错")
	}
}

func TestNewClient_MissingToken(t *testing.T) {
	_, err := NewClient(Config{URL: "http://localhost:8086"})
	if err == nil {
		t.Fatal("缺少 Token 应报错")
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	_, err := NewClient(Config{URL: "http://invalid-host:8086", Token: "test"})
	if err == nil {
		t.Fatal("无效 URL 应报错")
	}
}
