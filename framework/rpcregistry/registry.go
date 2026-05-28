// Package rpcregistry 提供 RPC 服务注册 / 发现的抽象与多实现。
//
// 设计目标：
//   - rpcserver 与 rpcclient 共用同一套注册中心抽象，避免双向依赖；
//   - 支持多后端：Consul、Nacos（均实现 ServiceRegistry）；
//   - 客户端侧提供 Resolver，可对接 gRPC 原生负载均衡（round_robin 等）。
//
// 典型用法（server 侧）：
//
//	reg, _ := rpcregistry.NewFromConfig("RpcServer")
//	server.SetRegistry(reg)
//
// 典型用法（client 侧）：
//
//	reg, _ := rpcregistry.NewFromConfig("RpcClient")
//	cli := rpcclient.New("RpcClient", reg)
//
// @author gaia-framework
// @created 2026-06-24
package rpcregistry

import (
	"fmt"
	"strings"

	"github.com/xxzhwl/gaia"
)

// ServiceRegistry 服务注册 / 发现接口。
// Consul / Nacos 均实现此接口，保持上层 API 一致。
type ServiceRegistry interface {
	// Register 注册服务实例到注册中心。
	Register(serviceName, host, port, version string) error

	// Deregister 从注册中心注销服务实例。
	Deregister(serviceName string) error

	// Discover 发现指定服务的健康实例列表。
	Discover(serviceName string) ([]*ServiceInstance, error)

	// Watch 订阅服务实例变更。返回的 stop 用于取消订阅。
	// 不支持订阅的实现可返回 (nil, ErrWatchUnsupported)，调用方需做降级（轮询 Discover）。
	Watch(serviceName string, onChange func(instances []*ServiceInstance)) (stop func(), err error)

	// Close 释放注册中心客户端资源。
	Close() error
}

// ServiceInstance 服务实例信息。
type ServiceInstance struct {
	ServiceID string
	Host      string
	Port      int
	Version   string
	Weight    float64           // 权重，用于加权负载均衡；缺省 1
	Healthy   bool              // 是否健康
	Metadata  map[string]string // 业务元数据
}

// Addr 返回 host:port 形式的地址。
func (s *ServiceInstance) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
}

// ErrWatchUnsupported 表示当前注册中心实现不支持 Watch。
var ErrWatchUnsupported = fmt.Errorf("rpcregistry: watch not supported by this backend")

// RegistryType 注册中心类型。
type RegistryType string

const (
	RegistryConsul RegistryType = "consul"
	RegistryNacos  RegistryType = "nacos"
)

// NewFromConfig 根据 schema 配置创建注册中心。
//
// 读取配置：
//
//	{schema}.Registry.Enable   bool    是否启用注册中心（false 返回 nil,nil，调用方走静态地址）
//	{schema}.Registry.Type     string  consul | nacos（默认 consul）
//
// 当 Enable=false 时返回 (nil, nil)，便于调用方按"无注册中心 = 直连静态配置"语义处理。
func NewFromConfig(schema string) (ServiceRegistry, error) {
	if !gaia.GetSafeConfBool(schema + ".Registry.Enable") {
		return nil, nil
	}
	t := RegistryType(strings.ToLower(strings.TrimSpace(
		gaia.GetSafeConfStringWithDefault(schema+".Registry.Type", string(RegistryConsul)),
	)))
	switch t {
	case RegistryConsul:
		return NewConsulRegistryFromConfig(schema)
	case RegistryNacos:
		return NewNacosRegistryFromConfig(schema)
	default:
		return nil, fmt.Errorf("rpcregistry: 未知的注册中心类型 %q（支持 consul | nacos）", t)
	}
}
