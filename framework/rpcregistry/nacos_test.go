package rpcregistry

import (
	"testing"

	"github.com/nacos-group/nacos-sdk-go/v2/model"
)

func TestSplitHostPort(t *testing.T) {
	host, port, err := splitHostPort("127.0.0.1:8848")
	if err != nil {
		t.Fatalf("正常地址不应报错：%v", err)
	}
	if host != "127.0.0.1" || port != 8848 {
		t.Fatalf("解析结果错误：host=%q port=%d", host, port)
	}

	if _, _, err := splitHostPort("no-port"); err == nil {
		t.Fatal("缺少端口应报错")
	}
	if _, _, err := splitHostPort("host:notnum"); err == nil {
		t.Fatal("非法端口应报错")
	}
}

func TestFilterUsableNacos(t *testing.T) {
	in := []model.Instance{
		{Ip: "1.1.1.1", Healthy: true, Enable: true, Weight: 1},  // 保留
		{Ip: "2.2.2.2", Healthy: false, Enable: true, Weight: 1}, // 不健康
		{Ip: "3.3.3.3", Healthy: true, Enable: false, Weight: 1}, // 未启用
		{Ip: "4.4.4.4", Healthy: true, Enable: true, Weight: 0},  // 权重 0
		{Ip: "5.5.5.5", Healthy: true, Enable: true, Weight: 10}, // 保留
	}
	out := filterUsableNacos(in)
	if len(out) != 2 {
		t.Fatalf("应只保留 2 个可用实例，实际 %d", len(out))
	}
}

func TestConvertNacosInstances(t *testing.T) {
	in := []model.Instance{
		{
			InstanceId: "id-1",
			Ip:         "10.0.0.1",
			Port:       9090,
			Weight:     8,
			Healthy:    true,
			Metadata:   map[string]string{"version": "1.2.3"},
		},
	}
	out := convertNacosInstances(in)
	if len(out) != 1 {
		t.Fatalf("应转换 1 个实例，实际 %d", len(out))
	}
	got := out[0]
	if got.Host != "10.0.0.1" || got.Port != 9090 {
		t.Fatalf("host/port 转换错误：%s:%d", got.Host, got.Port)
	}
	if got.Version != "1.2.3" {
		t.Fatalf("version 应从 metadata 提取，实际 %q", got.Version)
	}
	if got.Weight != 8 {
		t.Fatalf("weight 转换错误：%v", got.Weight)
	}
	if got.Addr() != "10.0.0.1:9090" {
		t.Fatalf("Addr 错误：%q", got.Addr())
	}
}
