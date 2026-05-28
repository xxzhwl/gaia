package rpcregistry

import "testing"

func TestServiceInstance_Addr(t *testing.T) {
	inst := &ServiceInstance{Host: "192.168.1.1", Port: 8080}
	if got := inst.Addr(); got != "192.168.1.1:8080" {
		t.Fatalf("Addr 应为 host:port，实际 %q", got)
	}
}

// TestNewFromConfig_Disabled Registry.Enable 默认为 false 时返回 (nil, nil)，
// 表示"无注册中心 = 走静态地址"。
func TestNewFromConfig_Disabled(t *testing.T) {
	reg, err := NewFromConfig("NoSuchSchemaForTest")
	if err != nil {
		t.Fatalf("未启用注册中心不应报错，实际 %v", err)
	}
	if reg != nil {
		t.Fatal("未启用注册中心应返回 nil")
	}
}

func TestRegistryTypeConstants(t *testing.T) {
	if RegistryConsul != "consul" || RegistryNacos != "nacos" {
		t.Fatalf("注册中心类型常量值不正确：consul=%q nacos=%q", RegistryConsul, RegistryNacos)
	}
}
