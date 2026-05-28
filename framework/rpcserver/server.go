// Package rpcserver 提供基于 gRPC 的 RPC 服务端实现。
//
// 客户端逻辑见 framework/rpcclient，
// 服务注册 / 发现见 framework/rpcregistry。
//
// 使用示例:
//
//	app := rpcserver.NewGrpcApp("RpcServer")
//	pb.RegisterUserServiceServer(app.GrpcServer(), &UserServiceImpl{})
//	app.RegisterService("UserService", "1.0.0")
//
//	if reg, _ := rpcregistry.NewFromConfig("RpcServer"); reg != nil {
//	    app.SetRegistry(reg)
//	}
//
//	// 可选：注册业务清理回调（停后台 worker、flush 审计/缓冲、关连接池等）。
//	// 触发时机为 GracefulStop 之后、flush tracer/metrics 之前，按 LIFO 逆序执行，
//	// 故清理过程产生的日志/trace/metric 仍能被导出。可多次注册。
//	app.AddCleanup(func() {
//	    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	    defer cancel()
//	    _ = mgr.Close(ctx)
//	})
//
//	app.Run()
//
// @author gaia-framework
// @created 2026-04-17
// @refactored 2026-06-24
package rpcserver

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/metrics"
	"github.com/xxzhwl/gaia/framework/rpcregistry"
	"github.com/xxzhwl/gaia/framework/tracer"
)

// ServiceDesc 服务描述信息。
type ServiceDesc struct {
	Name    string // 服务名称（如 "UserService"）
	Version string // 服务版本
}

// ===========================
// 包级共享 Logger
// ===========================

var rpcLogger *logImpl.DefaultLogger

func init() {
	rpcLogger = logImpl.NewDefaultLogger().SetTitle("RpcServer")
}

// ===========================
// gRPC Server 基础结构
// ===========================

// baseRpcServer gRPC Server 通用基础字段。
type baseRpcServer struct {
	schema   string
	host     string
	port     string
	services []ServiceDesc
	registry rpcregistry.ServiceRegistry

	shutdownOnce sync.Once
	stopChan     chan struct{}

	// cleanup 回调：业务侧通过 AddCleanup 注册，shutdown 时按 LIFO 执行
	// （在 GracefulStop 之后、关闭追踪/指标之前）。语义与 framework/server (HTTP) 对齐。
	cleanupMu   sync.Mutex
	cleanupFns  []func()
	cleanupOnce sync.Once
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

func (b *baseRpcServer) setRegistry(registry rpcregistry.ServiceRegistry) {
	b.registry = registry
}

// registerToRegistry 注册到服务发现中心。
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

// deregisterFromRegistry 从服务发现中心注销。
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

// printServiceInfo 打印注册的服务信息。
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

// registerShutdownHook 注册系统信号处理。
func (b *baseRpcServer) registerShutdownHook(stopFunc func()) {
	go func() {
		defer gaia.CatchPanic()
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigChan)

		<-sigChan
		stopFunc()
	}()
}

// AddCleanup 注册优雅关闭时要执行的业务清理回调（按注册顺序 LIFO 逆序触发）。
//
// 触发时机：GracefulStop 之后、关闭追踪/指标系统之前——因此回调内产生的
// 日志/trace/metric 仍能被正常导出，避免"清理动作的观测数据丢失"。
//
// 适用场景：业务后台 worker 的停止、审计/缓冲区 flush、自定义连接池关闭等。
// 与 framework/server (HTTP) 的 AddCleanup 语义一致。多次调用 Stop 仅执行一次。
func (b *baseRpcServer) AddCleanup(f func()) {
	if f == nil {
		return
	}
	b.cleanupMu.Lock()
	b.cleanupFns = append(b.cleanupFns, f)
	b.cleanupMu.Unlock()
}

// runCleanups 按 LIFO 顺序执行所有已注册的清理回调，且仅执行一次。
// 单个回调 panic 不影响其余回调与后续 shutdown 流程。
func (b *baseRpcServer) runCleanups() {
	b.cleanupOnce.Do(func() {
		b.cleanupMu.Lock()
		fns := append([]func(){}, b.cleanupFns...)
		b.cleanupMu.Unlock()
		// LIFO：后注册的先释放（通常后注册者依赖先注册的资源）。
		for i := len(fns) - 1; i >= 0; i-- {
			func(fn func()) {
				defer gaia.CatchPanic()
				fn()
			}(fns[i])
		}
	})
}

func shutdownRpcTracer() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tracer.ShutdownTracer(ctx); err != nil {
		gaia.WarnF("[RPC] 关闭追踪系统失败: %s", err.Error())
	}
}

// shutdownRpcMetrics 优雅关闭指标系统（flush 残留指标 + 关闭 /metrics HTTP 服务）。
// 与 shutdownRpcTracer 对称；未初始化时为 noop，重复调用安全。
func shutdownRpcMetrics() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := metrics.Shutdown(ctx); err != nil {
		gaia.WarnF("[RPC] 关闭指标系统失败: %s", err.Error())
	}
}

// ===========================
// 辅助函数
// ===========================

// readSchemaConfig 读取 schema 的 host/port 配置。
func readSchemaConfig(schema string) (host, port string) {
	host = gaia.GetSafeConfStringWithDefault(schema+".Host", "0.0.0.0")
	port = gaia.GetSafeConfStringWithDefault(schema+".Port", "9090")
	return
}
