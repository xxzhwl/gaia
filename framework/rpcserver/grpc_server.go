// Package rpcserver gRPC Server 实现
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/tracer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// GrpcServer gRPC 服务器实现
type GrpcServer struct {
	baseRpcServer
	server       *grpc.Server
	listener     net.Listener
	healthServer *health.Server
}

// NewGrpcApp 从配置创建 gRPC Server
func NewGrpcApp(schema string) *GrpcServer {
	if schema == "" {
		schema = "RpcServer"
	}
	host, port := readSchemaConfig(schema)
	return newGrpcApp(schema, host, port)
}

// NewGrpcAppWithPort 指定端口创建 gRPC Server
func NewGrpcAppWithPort(port string) *GrpcServer {
	return newGrpcApp("RpcServer", "0.0.0.0", port)
}

// DefaultGrpcApp 使用默认 schema 创建 gRPC Server
func DefaultGrpcApp() *GrpcServer {
	return NewGrpcApp("")
}

func newGrpcApp(schema, host, port string) *GrpcServer {
	if _, err := tracer.SetupTracer(context.Background(), schema); err != nil {
		gaia.WarnF("[gRPC] 初始化链路追踪失败: %s", err.Error())
	}

	opts := buildGrpcServerOptions(schema)
	s := &GrpcServer{
		baseRpcServer: newBaseRpcServer(schema, host, port),
		server:        grpc.NewServer(opts...),
		healthServer:  health.NewServer(),
	}

	healthpb.RegisterHealthServer(s.server, s.healthServer)

	if gaia.IsEnvDev() {
		reflection.Register(s.server)
		gaia.Info("[gRPC] 已启用 gRPC 反射（开发环境）")
	}

	return s
}

// GrpcServer 返回底层 gRPC Server（用于注册 Protobuf Service）
func (s *GrpcServer) GrpcServer() *grpc.Server {
	return s.server
}

// RegisterService 实现 IRpcServer
func (s *GrpcServer) RegisterService(name, version string) {
	s.addService(name, version)
	s.healthServer.SetServingStatus(name, healthpb.HealthCheckResponse_SERVING)
}

// SetRegistry 实现 IRpcServer
func (s *GrpcServer) SetRegistry(registry ServiceRegistry) {
	s.setRegistry(registry)
}

// Protocol 实现 IRpcServer
func (s *GrpcServer) Protocol() string {
	return string(RpcTypeGrpc)
}

// Run 实现 IRpcServer
func (s *GrpcServer) Run() {
	addr := s.host + ":" + s.port
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		panic(fmt.Sprintf("[gRPC] 无法监听地址 %s: %v", addr, err))
	}
	s.listener = lis

	s.printServiceInfo("grpc")
	s.registerToRegistry("grpc")
	s.registerShutdownHook(s.Stop)

	gaia.InfoF("[gRPC] Server 启动成功，监听: %s", addr)
	if err := s.server.Serve(lis); err != nil {
		gaia.ErrorF("[gRPC] Server 运行异常: %v", err)
	}
}

// Stop 实现 IRpcServer
func (s *GrpcServer) Stop() {
	s.shutdownOnce.Do(func() {
		gaia.Info("[gRPC] Server 正在优雅关闭...")
		for _, svc := range s.services {
			s.healthServer.SetServingStatus(svc.Name, healthpb.HealthCheckResponse_NOT_SERVING)
		}
		s.deregisterFromRegistry("grpc")
		s.server.GracefulStop()
		close(s.stopChan)
		gaia.Info("[gRPC] Server 已停止")
	})
}

// ForceStop 强制停止
func (s *GrpcServer) ForceStop() {
	s.server.Stop()
}

// ===========================
// gRPC Server Options 构建
// ===========================

func buildGrpcServerOptions(schema string) []grpc.ServerOption {
	var opts []grpc.ServerOption

	// 拦截器链
	opts = append(opts,
		grpc.ChainUnaryInterceptor(
			GrpcRecoveryUnaryInterceptor(),
			GrpcLoggingUnaryInterceptor(),
			GrpcTracingUnaryInterceptor(),
			GrpcRateLimitUnaryInterceptor(schema),
		),
		grpc.ChainStreamInterceptor(
			GrpcRecoveryStreamInterceptor(),
			GrpcLoggingStreamInterceptor(),
			GrpcTracingStreamInterceptor(),
			GrpcRateLimitStreamInterceptor(schema),
		),
	)

	// KeepAlive
	keepAliveTime := gaia.GetSafeConfInt64WithDefault(schema+".KeepAliveTime", 30)
	keepAliveTimeout := gaia.GetSafeConfInt64WithDefault(schema+".KeepAliveTimeout", 10)
	opts = append(opts,
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    time.Duration(keepAliveTime) * time.Second,
			Timeout: time.Duration(keepAliveTimeout) * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	// 消息大小
	maxRecvSize := gaia.GetSafeConfInt64WithDefault(schema+".MaxRecvMsgSize", 4)
	maxSendSize := gaia.GetSafeConfInt64WithDefault(schema+".MaxSendMsgSize", 4)
	opts = append(opts,
		grpc.MaxRecvMsgSize(int(maxRecvSize)*1024*1024),
		grpc.MaxSendMsgSize(int(maxSendSize)*1024*1024),
	)

	// 并发流
	maxStreams := gaia.GetSafeConfInt64WithDefault(schema+".MaxConcurrentStreams", 100)
	opts = append(opts, grpc.MaxConcurrentStreams(uint32(maxStreams)))

	// TLS
	if gaia.GetSafeConfBool(schema + ".TLS.Enable") {
		if tlsCreds := buildGrpcTLSConfig(schema); tlsCreds != nil {
			opts = append(opts, grpc.Creds(tlsCreds))
		}
	}

	return opts
}

func buildGrpcTLSConfig(schema string) credentials.TransportCredentials {
	certPath := gaia.GetSafeConfString(schema + ".TLS.CrtPath")
	keyPath := gaia.GetSafeConfString(schema + ".TLS.KeyPath")
	if certPath == "" || keyPath == "" {
		gaia.Warn("[gRPC] TLS 证书路径未配置")
		return nil
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		gaia.ErrorF("[gRPC] 加载 TLS 证书失败: %v", err)
		return nil
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	caPath := gaia.GetSafeConfString(schema + ".TLS.CAPath")
	if caPath != "" {
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			gaia.ErrorF("[gRPC] 加载 CA 证书失败: %v", err)
		} else {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(caCert) {
				tlsConfig.ClientCAs = pool
				tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
				gaia.Info("[gRPC] TLS: 已启用 mTLS 双向认证")
			}
		}
	}

	return credentials.NewTLS(tlsConfig)
}
