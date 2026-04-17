// Package rpcserver Kitex Server 实现
// 基于 CloudWeGo Kitex，与 Hertz（HTTP Server）同生态
// 特性：高性能（netpoll）、支持 Thrift/Protobuf、内置服务治理
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"context"
	"net"
	"time"

	"github.com/cloudwego/kitex/pkg/limit"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/pkg/transmeta"
	kitexserver "github.com/cloudwego/kitex/server"
	consul "github.com/kitex-contrib/registry-consul"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/tracer"
)

// KitexServer Kitex 服务器实现
type KitexServer struct {
	baseRpcServer
	server kitexserver.Server
	opts   []kitexserver.Option // 收集 options，在 Run 时构建 server
}

// NewKitexApp 从配置创建 Kitex Server
func NewKitexApp(schema string) *KitexServer {
	if schema == "" {
		schema = "RpcServer"
	}
	host, port := readSchemaConfig(schema)
	return newKitexApp(schema, host, port)
}

// NewKitexAppWithPort 指定端口创建 Kitex Server
func NewKitexAppWithPort(port string) *KitexServer {
	return newKitexApp("RpcServer", "0.0.0.0", port)
}

// DefaultKitexApp 使用默认 schema 创建 Kitex Server
func DefaultKitexApp() *KitexServer {
	return NewKitexApp("")
}

func newKitexApp(schema, host, port string) *KitexServer {
	if _, err := tracer.SetupTracer(context.Background(), schema); err != nil {
		gaia.WarnF("[Kitex] 初始化链路追踪失败: %s", err.Error())
	}

	s := &KitexServer{
		baseRpcServer: newBaseRpcServer(schema, host, port),
	}

	// 构建 Kitex Server Options
	s.opts = s.buildOptions()

	return s
}

// buildOptions 构建 Kitex Server 选项
func (s *KitexServer) buildOptions() []kitexserver.Option {
	addr, _ := net.ResolveTCPAddr("tcp", s.host+":"+s.port)

	opts := []kitexserver.Option{
		kitexserver.WithServiceAddr(addr),

		// 中间件链
		kitexserver.WithMiddleware(KitexRecoveryMiddleware()),
		kitexserver.WithMiddleware(KitexLoggingMiddleware()),
		kitexserver.WithMiddleware(KitexTracingMiddleware()),
		kitexserver.WithMiddleware(KitexRateLimitMiddleware(s.schema)),

		// 元信息传递（支持 HTTP2/TTHeader 的 metadata 透传）
		kitexserver.WithMetaHandler(transmeta.ServerTTHeaderHandler),

		// 服务信息
		kitexserver.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{
			ServiceName: gaia.GetSystemEnName(),
		}),
	}

	// 连接限制
	maxConnections := gaia.GetSafeConfInt64WithDefault(s.schema+".MaxConnections", 10000)
	maxQPS := gaia.GetSafeConfInt64WithDefault(s.schema+".MaxQPS", 10000)
	opts = append(opts, kitexserver.WithLimit(&limit.Option{
		MaxConnections: int(maxConnections),
		MaxQPS:         int(maxQPS),
	}))

	// 读写超时
	readTimeout := gaia.GetSafeConfInt64WithDefault(s.schema+".ReadTimeout", 5)
	opts = append(opts, kitexserver.WithReadWriteTimeout(
		time.Duration(readTimeout)*time.Second,
	))

	// 优雅关闭等待时间
	exitWait := gaia.GetSafeConfInt64WithDefault(s.schema+".GracefulExitTime", 5)
	opts = append(opts, kitexserver.WithExitWaitTime(
		time.Duration(exitWait)*time.Second,
	))

	// Consul 服务注册（Kitex 原生支持）
	if gaia.GetSafeConfBool(s.schema + ".Registry.Enable") {
		consulEndpoint := gaia.GetSafeConfStringWithDefault(
			s.schema+".Registry.Endpoint", "localhost:8500")
		r, err := consul.NewConsulRegister(consulEndpoint)
		if err != nil {
			gaia.ErrorF("[Kitex] 创建 Consul 注册器失败: %v", err)
		} else {
			opts = append(opts, kitexserver.WithRegistry(r))
			gaia.InfoF("[Kitex] 已启用 Consul 服务注册: %s", consulEndpoint)
		}
	}

	return opts
}

// GetOptions 返回已构建的 Kitex Server Options（供用户注册服务时使用）
// 用法：
//
//	kitexApp := rpcserver.NewKitexApp("RpcServer")
//	svr := userservice.NewServer(&UserServiceImpl{}, kitexApp.GetOptions()...)
//	kitexApp.SetKitexServer(svr)
//	kitexApp.Run()
func (s *KitexServer) GetOptions() []kitexserver.Option {
	return s.opts
}

// SetKitexServer 设置 Kitex Server 实例（由用户通过 kitex 生成的代码创建）
func (s *KitexServer) SetKitexServer(svr kitexserver.Server) {
	s.server = svr
}

// RegisterService 实现 IRpcServer
func (s *KitexServer) RegisterService(name, version string) {
	s.addService(name, version)
}

// SetRegistry 实现 IRpcServer
func (s *KitexServer) SetRegistry(registry ServiceRegistry) {
	s.setRegistry(registry)
}

// Protocol 实现 IRpcServer
func (s *KitexServer) Protocol() string {
	return string(RpcTypeKitex)
}

// Run 实现 IRpcServer
func (s *KitexServer) Run() {
	if s.server == nil {
		panic("[Kitex] Server 未设置。请先调用 SetKitexServer() 设置通过 kitex 工具生成的 Server 实例")
	}

	s.printServiceInfo("kitex")

	// 如果有自定义 registry（非 Kitex 原生），也注册
	s.registerToRegistry("kitex")
	s.registerShutdownHook(s.Stop)

	gaia.InfoF("[Kitex] Server 启动成功，监听: %s:%s", s.host, s.port)
	if err := s.server.Run(); err != nil {
		gaia.ErrorF("[Kitex] Server 运行异常: %v", err)
	}
}

// Stop 实现 IRpcServer
func (s *KitexServer) Stop() {
	s.shutdownOnce.Do(func() {
		gaia.Info("[Kitex] Server 正在优雅关闭...")
		s.deregisterFromRegistry("kitex")
		if s.server != nil {
			_ = s.server.Stop()
		}
		close(s.stopChan)
		gaia.Info("[Kitex] Server 已停止")
	})
}

// ===========================
// Kitex 便捷工厂（适用于简单场景）
// ===========================

// BuildKitexServer 一步到位创建 Kitex Server
// handler 是 kitex 生成的 Service Handler 实现
// newServerFunc 是 kitex 生成的 NewServer 函数
//
// 用法：
//
//	svr := rpcserver.BuildKitexServer("RpcServer", func(opts ...kitexserver.Option) kitexserver.Server {
//	    return userservice.NewServer(&UserServiceImpl{}, opts...)
//	})
//	svr.RegisterService("UserService", "1.0.0")
//	svr.Run()
func BuildKitexServer(schema string, newServerFunc func(opts ...kitexserver.Option) kitexserver.Server) *KitexServer {
	kitexApp := NewKitexApp(schema)
	svr := newServerFunc(kitexApp.GetOptions()...)
	kitexApp.SetKitexServer(svr)
	return kitexApp
}
