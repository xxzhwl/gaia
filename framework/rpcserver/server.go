// Package rpcserver 提供 RPC 服务器/客户端抽象层
// 支持 gRPC 和 Kitex 两种实现，用户通过工厂函数选择
//
// 使用示例:
//
//	// 方式 A: gRPC（跨语言、标准化）
//	rpcApp := rpcserver.NewGrpcApp("RpcServer")
//
//	// 方式 B: Kitex（Go 生态、高性能、内置治理）
//	rpcApp := rpcserver.NewKitexApp("RpcServer")
//
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

// ===========================
// 通用接口
// ===========================

// IRpcServer RPC 服务器通用接口
// gRPC 和 Kitex 都实现此接口，保持 API 一致
type IRpcServer interface {
	// RegisterService 注册服务描述（用于日志和服务发现）
	RegisterService(name, version string)

	// SetRegistry 设置服务注册中心
	SetRegistry(registry ServiceRegistry)

	// Run 启动 RPC 服务器（阻塞）
	Run()

	// Stop 优雅停止 RPC 服务器
	Stop()

	// Protocol 返回协议类型（"grpc" 或 "kitex"）
	Protocol() string
}

// IRpcClient RPC 客户端通用接口
type IRpcClient interface {
	// Dial 通过服务名连接（自动服务发现）
	Dial(serviceName string) (any, error)

	// DialDirect 直连到指定地址
	DialDirect(addr string) (any, error)

	// Close 关闭所有连接
	Close()

	// CloseService 关闭指定服务的连接
	CloseService(serviceName string) error
}

// ===========================
// 通用类型
// ===========================

// ServiceDesc 服务描述信息
type ServiceDesc struct {
	Name    string // 服务名称（如 "UserService"）
	Version string // 服务版本
}

// RpcType RPC 协议类型
type RpcType string

const (
	RpcTypeGrpc  RpcType = "grpc"
	RpcTypeKitex RpcType = "kitex"
)

// ===========================
// 包级共享 Logger
// ===========================

var rpcLogger *logImpl.DefaultLogger

func init() {
	rpcLogger = logImpl.NewDefaultLogger().SetTitle("RpcServer")
}

// ===========================
// 通用基础结构（嵌入到 gRPC/Kitex 实现中）
// ===========================

// baseRpcServer 通用 RPC Server 基础字段
type baseRpcServer struct {
	schema   string
	host     string
	port     string
	services []ServiceDesc
	registry ServiceRegistry

	shutdownOnce sync.Once
	stopChan     chan struct{}
}

func newBaseRpcServer(schema, host, port string) baseRpcServer {
	return baseRpcServer{
		schema:   schema,
		host:     host,
		port:     port,
		stopChan: make(chan struct{}),
	}
}

func (b *baseRpcServer) addService(name, version string) {
	b.services = append(b.services, ServiceDesc{Name: name, Version: version})
}

func (b *baseRpcServer) setRegistry(registry ServiceRegistry) {
	b.registry = registry
}

// registerToRegistry 注册到服务发现中心
func (b *baseRpcServer) registerToRegistry(protocol string) {
	if b.registry == nil {
		return
	}
	for _, svc := range b.services {
		if err := b.registry.Register(svc.Name, b.host, b.port, svc.Version); err != nil {
			gaia.ErrorF("[%s] 服务注册失败 [%s]: %v", protocol, svc.Name, err)
		} else {
			gaia.InfoF("[%s] 服务已注册: %s -> %s:%s", protocol, svc.Name, b.host, b.port)
		}
	}
}

// deregisterFromRegistry 从服务发现中心注销
func (b *baseRpcServer) deregisterFromRegistry(protocol string) {
	if b.registry == nil {
		return
	}
	for _, svc := range b.services {
		if err := b.registry.Deregister(svc.Name); err != nil {
			gaia.ErrorF("[%s] 服务注销失败 [%s]: %v", protocol, svc.Name, err)
		}
	}
}

// printServiceInfo 打印注册的服务信息
func (b *baseRpcServer) printServiceInfo(protocol string) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n========== RPC Server (%s) ==========\n", strings.ToUpper(protocol)))
	sb.WriteString(fmt.Sprintf("  Address : %s:%s\n", b.host, b.port))
	sb.WriteString(fmt.Sprintf("  Schema  : %s\n", b.schema))
	sb.WriteString(fmt.Sprintf("  Protocol: %s\n", protocol))
	sb.WriteString(fmt.Sprintf("  Services: %d\n", len(b.services)))
	for _, svc := range b.services {
		sb.WriteString(fmt.Sprintf("    - %s (v%s)\n", svc.Name, svc.Version))
	}
	sb.WriteString("==========================================\n")
	gaia.Info(sb.String())
}

// registerShutdownHook 注册系统信号处理
func (b *baseRpcServer) registerShutdownHook(stopFunc func()) {
	go func() {
		defer gaia.CatchPanic()
		sigChan := make(chan os.Signal, 1)
		<-sigChan
		stopFunc()
	}()
}

// ===========================
// 通用客户端基础
// ===========================

// baseRpcClient 通用 RPC Client 基础字段
type baseRpcClient struct {
	registry ServiceRegistry
	schema   string
}

// resolveTarget 解析服务目标地址（通用逻辑）
func (b *baseRpcClient) resolveTarget(serviceName string) (string, error) {
	if b.registry == nil {
		addr := gaia.GetSafeConfString(b.schema + ".Services." + serviceName + ".Address")
		if addr == "" {
			return "", fmt.Errorf("未配置服务地址: %s.Services.%s.Address", b.schema, serviceName)
		}
		return addr, nil
	}

	instances, err := b.registry.Discover(serviceName)
	if err != nil {
		return "", err
	}
	if len(instances) == 0 {
		return "", fmt.Errorf("服务 %s 无可用实例", serviceName)
	}

	// TODO: 后续可扩展为加权轮询、一致性哈希等策略
	inst := instances[0]
	return fmt.Sprintf("%s:%d", inst.Host, inst.Port), nil
}

// ===========================
// 辅助函数
// ===========================

// readSchemaConfig 读取 schema 的 host/port 配置
func readSchemaConfig(schema string) (host, port string) {
	host = gaia.GetSafeConfStringWithDefault(schema+".Host", "0.0.0.0")
	port = gaia.GetSafeConfStringWithDefault(schema+".Port", "9090")
	return
}
