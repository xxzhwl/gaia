// Package rpcserver gRPC 客户端封装
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// GrpcClient gRPC 客户端管理器
type GrpcClient struct {
	baseRpcClient
	conns map[string]*grpc.ClientConn
	mu    sync.RWMutex
}

// NewGrpcClient 创建 gRPC 客户端管理器
func NewGrpcClient(schema string, registry ServiceRegistry) *GrpcClient {
	if schema == "" {
		schema = "RpcClient"
	}
	return &GrpcClient{
		baseRpcClient: baseRpcClient{registry: registry, schema: schema},
		conns:         make(map[string]*grpc.ClientConn),
	}
}

// Dial 通过服务名连接
func (c *GrpcClient) Dial(serviceName string) (*grpc.ClientConn, error) {
	c.mu.RLock()
	if conn, ok := c.conns[serviceName]; ok {
		c.mu.RUnlock()
		return conn, nil
	}
	c.mu.RUnlock()

	target, err := c.resolveTarget(serviceName)
	if err != nil {
		return nil, fmt.Errorf("[gRPC] 解析服务地址失败 [%s]: %w", serviceName, err)
	}

	conn, err := c.dial(target)
	if err != nil {
		return nil, fmt.Errorf("[gRPC] 连接失败 [%s -> %s]: %w", serviceName, target, err)
	}

	c.mu.Lock()
	c.conns[serviceName] = conn
	c.mu.Unlock()

	gaia.InfoF("[gRPC] 客户端已连接: %s -> %s", serviceName, target)
	return conn, nil
}

// DialDirect 直连到指定地址
func (c *GrpcClient) DialDirect(addr string) (*grpc.ClientConn, error) {
	c.mu.RLock()
	if conn, ok := c.conns[addr]; ok {
		c.mu.RUnlock()
		return conn, nil
	}
	c.mu.RUnlock()

	conn, err := c.dial(addr)
	if err != nil {
		return nil, fmt.Errorf("[gRPC] 直连失败 [%s]: %w", addr, err)
	}

	c.mu.Lock()
	c.conns[addr] = conn
	c.mu.Unlock()
	return conn, nil
}

// Close 关闭所有连接
func (c *GrpcClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name, conn := range c.conns {
		if err := conn.Close(); err != nil {
			gaia.ErrorF("[gRPC] 关闭连接失败 [%s]: %v", name, err)
		}
	}
	c.conns = make(map[string]*grpc.ClientConn)
}

// CloseService 关闭指定服务的连接
func (c *GrpcClient) CloseService(serviceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[serviceName]
	if !ok {
		return nil
	}
	err := conn.Close()
	delete(c.conns, serviceName)
	return err
}

func (c *GrpcClient) dial(target string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return grpc.DialContext(ctx, target, c.buildDialOptions()...)
}

func (c *GrpcClient) buildDialOptions() []grpc.DialOption {
	var opts []grpc.DialOption

	if !gaia.GetSafeConfBool(c.schema + ".TLS.Enable") {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	keepAliveTime := gaia.GetSafeConfInt64WithDefault(c.schema+".KeepAliveTime", 30)
	keepAliveTimeout := gaia.GetSafeConfInt64WithDefault(c.schema+".KeepAliveTimeout", 10)
	opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                time.Duration(keepAliveTime) * time.Second,
		Timeout:             time.Duration(keepAliveTimeout) * time.Second,
		PermitWithoutStream: true,
	}))

	opts = append(opts, grpc.WithChainUnaryInterceptor(
		grpcClientTracingInterceptor(),
		grpcClientLoggingInterceptor(),
	))

	maxRecvSize := gaia.GetSafeConfInt64WithDefault(c.schema+".MaxRecvMsgSize", 4)
	maxSendSize := gaia.GetSafeConfInt64WithDefault(c.schema+".MaxSendMsgSize", 4)
	opts = append(opts, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(int(maxRecvSize)*1024*1024),
		grpc.MaxCallSendMsgSize(int(maxSendSize)*1024*1024),
	))

	return opts
}

// ===== gRPC 客户端拦截器 =====

func grpcClientTracingInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		tr := tracer.GetTracer()
		if tr == nil {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		ctx, span := tr.Start(ctx, method, oteltrace.WithSpanKind(oteltrace.SpanKindClient))
		defer span.End()
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return err
	}
}

func grpcClientLoggingInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		startTime := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		duration := time.Since(startTime)
		if err != nil {
			rpcLogger.WarnF("[gRPC-Client] %s | %v | ERROR: %v", method, duration, err)
		} else {
			rpcLogger.DebugF("[gRPC-Client] %s | %v | OK", method, duration)
		}
		return err
	}
}
