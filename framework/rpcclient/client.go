// Package rpcclient 提供基于 gRPC 的 RPC 客户端。
//
// 特性：
//   - 服务发现 + 客户端负载均衡（对接 rpcregistry + gRPC 原生 round_robin）；
//   - 连接复用（按服务名 / 地址缓存 *grpc.ClientConn）；
//   - 拦截器链：链路追踪、日志、指标、鉴权注入；
//   - 完整可配置：超时、负载均衡策略、KeepAlive、消息大小、TLS（含 mTLS）、重试、auth token。
//
// 使用示例：
//
//	reg, _ := rpcregistry.NewFromConfig("RpcClient")     // 可为 nil（走静态地址）
//	cli := rpcclient.New("RpcClient", reg)
//	defer cli.Close()
//
//	conn, _ := cli.Dial("UserService")                  // 服务发现 + 负载均衡
//	client := pb.NewUserServiceClient(conn)
//
// @author gaia-framework
// @created 2026-06-24
package rpcclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/rpcregistry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/resolver"
)

// clientSeq 用于为每个 GrpcClient 生成唯一 resolver scheme，避免全局 resolver 冲突。
var clientSeq atomic.Uint64

// Config gRPC 客户端配置（从 gaia 配置中心读取）。
type Config struct {
	// 连接 / 调用超时
	DialTimeout time.Duration // 建连超时，默认 10s
	CallTimeout time.Duration // 单次调用默认超时（用于 WithCallTimeout 包装），默认 0（不限制）

	// 负载均衡
	LoadBalancing string        // round_robin | pick_first，默认 round_robin
	ResolveEvery  time.Duration // registry 不支持 Watch 时的轮询间隔，默认 10s

	// KeepAlive
	KeepAliveTime    time.Duration
	KeepAliveTimeout time.Duration

	// 消息大小（MB）
	MaxRecvMsgSizeMB int
	MaxSendMsgSizeMB int

	// 重试（通过 gRPC serviceConfig 的 retryPolicy 生效，需服务端为幂等方法）
	RetryEnable            bool
	RetryMaxAttempts       int      // 最大尝试次数（含首次），gRPC 限制 2~5，默认 3
	RetryInitialBackoff    string   // 首次重试退避，protobuf duration 格式如 "0.1s"
	RetryMaxBackoff        string   // 最大退避，如 "1s"
	RetryBackoffMultiplier float64  // 退避倍数，默认 2.0
	RetryableStatusCodes   []string // 可重试的 gRPC 状态码，默认 ["UNAVAILABLE"]

	// 鉴权
	AuthEnable    bool
	AuthHeaderKey string
	AuthScheme    string
	AuthToken     string

	// TLS
	TLSEnable     bool
	TLSCAPath     string
	TLSCertPath   string // mTLS 客户端证书
	TLSKeyPath    string
	TLSServerName string
	TLSInsecure   bool // 跳过服务端证书校验（仅测试用）

	// 阻塞建连
	BlockOnDial bool // 是否在 Dial 时阻塞直到连接就绪，默认 false
}

// LoadConfig 从 gaia 配置中心加载客户端配置。
func LoadConfig(schema string) Config {
	return Config{
		DialTimeout:            time.Duration(gaia.GetSafeConfInt64WithDefault(schema+".DialTimeout", 10)) * time.Second,
		CallTimeout:            time.Duration(gaia.GetSafeConfInt64WithDefault(schema+".CallTimeout", 0)) * time.Second,
		LoadBalancing:          gaia.GetSafeConfStringWithDefault(schema+".LoadBalancing", "round_robin"),
		ResolveEvery:           time.Duration(gaia.GetSafeConfInt64WithDefault(schema+".ResolveEvery", 10)) * time.Second,
		KeepAliveTime:          time.Duration(gaia.GetSafeConfInt64WithDefault(schema+".KeepAliveTime", 30)) * time.Second,
		KeepAliveTimeout:       time.Duration(gaia.GetSafeConfInt64WithDefault(schema+".KeepAliveTimeout", 10)) * time.Second,
		MaxRecvMsgSizeMB:       int(gaia.GetSafeConfInt64WithDefault(schema+".MaxRecvMsgSize", 4)),
		MaxSendMsgSizeMB:       int(gaia.GetSafeConfInt64WithDefault(schema+".MaxSendMsgSize", 4)),
		RetryEnable:            gaia.GetSafeConfBoolWithDefault(schema+".Retry.Enable", false),
		RetryMaxAttempts:       int(gaia.GetSafeConfInt64WithDefault(schema+".Retry.MaxAttempts", 3)),
		RetryInitialBackoff:    msToDurationStr(gaia.GetSafeConfInt64WithDefault(schema+".Retry.InitialBackoffMs", 100)),
		RetryMaxBackoff:        msToDurationStr(gaia.GetSafeConfInt64WithDefault(schema+".Retry.MaxBackoffMs", 1000)),
		RetryBackoffMultiplier: gaia.GetSafeConfFloat64WithDefault(schema+".Retry.BackoffMultiplier", 2.0),
		RetryableStatusCodes:   gaia.GetSafeConfStringSliceFromStringWithDefault(schema+".Retry.RetryableStatusCodes", []string{"UNAVAILABLE"}),
		AuthEnable:             gaia.GetSafeConfBool(schema + ".Auth.Enable"),
		AuthHeaderKey:          gaia.GetSafeConfStringWithDefault(schema+".Auth.HeaderKey", "authorization"),
		AuthScheme:             gaia.GetSafeConfStringWithDefault(schema+".Auth.Scheme", "Bearer"),
		AuthToken:              gaia.GetSafeConfString(schema + ".Auth.Token"),
		TLSEnable:              gaia.GetSafeConfBool(schema + ".TLS.Enable"),
		TLSCAPath:              gaia.GetSafeConfString(schema + ".TLS.CAPath"),
		TLSCertPath:            gaia.GetSafeConfString(schema + ".TLS.CrtPath"),
		TLSKeyPath:             gaia.GetSafeConfString(schema + ".TLS.KeyPath"),
		TLSServerName:          gaia.GetSafeConfString(schema + ".TLS.ServerName"),
		TLSInsecure:            gaia.GetSafeConfBool(schema + ".TLS.InsecureSkipVerify"),
		BlockOnDial:            gaia.GetSafeConfBool(schema + ".BlockOnDial"),
	}
}

// GrpcClient gRPC 客户端管理器（连接复用 + 负载均衡）。
type GrpcClient struct {
	schema   string
	cfg      Config
	registry rpcregistry.ServiceRegistry

	scheme  string // 本 client 专属 resolver scheme
	builder *registryResolverBuilder

	conns map[string]*grpc.ClientConn
	mu    sync.RWMutex

	closed atomic.Bool
}

// New 创建 gRPC 客户端管理器。registry 为 nil 时走静态地址直连。
func New(schema string, registry rpcregistry.ServiceRegistry) *GrpcClient {
	if schema == "" {
		schema = "RpcClient"
	}
	cfg := LoadConfig(schema)
	c := &GrpcClient{
		schema:   schema,
		cfg:      cfg,
		registry: registry,
		conns:    make(map[string]*grpc.ClientConn),
	}

	if registry != nil {
		// 每个 client 注册唯一 scheme 的 resolver，避免全局冲突
		c.scheme = fmt.Sprintf("%s-%d", Scheme, clientSeq.Add(1))
		c.builder = newRegistryResolverBuilder(c.scheme, registry, cfg.ResolveEvery)
		resolver.Register(c.builder)
	}
	return c
}

// Dial 通过服务名连接（启用服务发现 + 负载均衡）。
// 无 registry 时回退到静态配置 {schema}.Services.{name}.Address 直连。
func (c *GrpcClient) Dial(serviceName string) (*grpc.ClientConn, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("[gRPC] 客户端已关闭")
	}
	if conn := c.getConn(serviceName); conn != nil {
		return conn, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// double-check
	if conn, ok := c.conns[serviceName]; ok {
		return conn, nil
	}

	var target string
	if c.registry != nil {
		// gaia-N:///serviceName → 触发 resolver 走服务发现 + round_robin
		target = fmt.Sprintf("%s:///%s", c.scheme, serviceName)
	} else {
		addr := gaia.GetSafeConfString(c.schema + ".Services." + serviceName + ".Address")
		if addr == "" {
			return nil, fmt.Errorf("[gRPC] 未配置服务地址: %s.Services.%s.Address", c.schema, serviceName)
		}
		target = addr
	}

	conn, err := c.newConn(target)
	if err != nil {
		return nil, fmt.Errorf("[gRPC] 连接失败 [%s -> %s]: %w", serviceName, target, err)
	}
	c.conns[serviceName] = conn
	gaia.InfoF("[gRPC] 客户端已连接: %s -> %s", serviceName, target)
	return conn, nil
}

// DialDirect 直连指定地址（不走服务发现，pick_first）。
func (c *GrpcClient) DialDirect(addr string) (*grpc.ClientConn, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("[gRPC] 客户端已关闭")
	}
	if conn := c.getConn(addr); conn != nil {
		return conn, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if conn, ok := c.conns[addr]; ok {
		return conn, nil
	}

	conn, err := c.newConn(addr)
	if err != nil {
		return nil, fmt.Errorf("[gRPC] 直连失败 [%s]: %w", addr, err)
	}
	c.conns[addr] = conn
	return conn, nil
}

func (c *GrpcClient) getConn(key string) *grpc.ClientConn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conns[key]
}

// Close 关闭所有连接。重复调用安全。
//
// 注意：registry 由调用方传入、可能与 server 端或其它 client 共享，因此 Close 不会
// 关闭 registry —— 其生命周期由创建方负责（自行调用 registry.Close()）。
// 每个 client 在创建时会向 gRPC 全局 resolver 注册一个唯一 scheme 的 builder；
// gRPC resolver 包未提供 Unregister API，故该注册项会保留到进程结束。
// 由于 client 通常为长生命周期单例，此处影响可忽略。
func (c *GrpcClient) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.mu.Lock()
	for name, conn := range c.conns {
		if err := conn.Close(); err != nil {
			gaia.ErrorF("[gRPC] 关闭连接失败 [%s]: %v", name, err)
		}
	}
	c.conns = make(map[string]*grpc.ClientConn)
	c.mu.Unlock()
}

// CloseService 关闭指定服务 / 地址的连接。
func (c *GrpcClient) CloseService(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	conn, ok := c.conns[key]
	if !ok {
		return nil
	}
	err := conn.Close()
	delete(c.conns, key)
	return err
}

func (c *GrpcClient) newConn(target string) (*grpc.ClientConn, error) {
	opts, err := c.buildDialOptions()
	if err != nil {
		return nil, err
	}

	if c.cfg.BlockOnDial {
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.DialTimeout)
		defer cancel()
		//nolint:staticcheck // 阻塞建连需要 DialContext + WithBlock
		return grpc.DialContext(ctx, target, append(opts, grpc.WithBlock())...)
	}
	// 非阻塞：使用推荐的 NewClient（惰性建连）
	return grpc.NewClient(target, opts...)
}

func (c *GrpcClient) buildDialOptions() ([]grpc.DialOption, error) {
	var opts []grpc.DialOption

	// ===== 传输凭证（修复原实现 TLS 启用时未设凭证的 bug）=====
	creds, err := c.buildTransportCredentials()
	if err != nil {
		return nil, err
	}
	opts = append(opts, grpc.WithTransportCredentials(creds))

	// ===== KeepAlive =====
	opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                c.cfg.KeepAliveTime,
		Timeout:             c.cfg.KeepAliveTimeout,
		PermitWithoutStream: true,
	}))

	// ===== 负载均衡 + 重试（serviceConfig）=====
	opts = append(opts, grpc.WithDefaultServiceConfig(c.buildServiceConfig()))

	// ===== 拦截器链 =====
	unary := []grpc.UnaryClientInterceptor{
		tracingInterceptor(),
		metricsInterceptor(),
		loggingInterceptor(c.schema),
	}
	if c.cfg.AuthEnable && c.cfg.AuthToken != "" {
		unary = append(unary, authInterceptor(c.cfg.AuthHeaderKey, c.cfg.AuthScheme, c.cfg.AuthToken))
	}
	opts = append(opts, grpc.WithChainUnaryInterceptor(unary...))

	// ===== 消息大小 =====
	opts = append(opts, grpc.WithDefaultCallOptions(
		grpc.MaxCallRecvMsgSize(c.cfg.MaxRecvMsgSizeMB*1024*1024),
		grpc.MaxCallSendMsgSize(c.cfg.MaxSendMsgSizeMB*1024*1024),
	))

	return opts, nil
}

// buildTransportCredentials 构建传输层凭证。
//   - 未启用 TLS：insecure
//   - 启用 TLS：加载 CA / mTLS 证书 / ServerName / InsecureSkipVerify
func (c *GrpcClient) buildTransportCredentials() (credentials.TransportCredentials, error) {
	if !c.cfg.TLSEnable {
		return insecure.NewCredentials(), nil
	}

	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         c.cfg.TLSServerName,
		InsecureSkipVerify: c.cfg.TLSInsecure, //nolint:gosec // 由配置显式控制，仅测试场景
	}

	// CA：校验服务端证书
	if c.cfg.TLSCAPath != "" {
		caCert, err := os.ReadFile(c.cfg.TLSCAPath)
		if err != nil {
			return nil, fmt.Errorf("[gRPC] 读取 CA 证书失败: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("[gRPC] CA 证书解析失败: %s", c.cfg.TLSCAPath)
		}
		tlsCfg.RootCAs = pool
	}

	// mTLS：客户端证书
	if c.cfg.TLSCertPath != "" && c.cfg.TLSKeyPath != "" {
		cert, err := tls.LoadX509KeyPair(c.cfg.TLSCertPath, c.cfg.TLSKeyPath)
		if err != nil {
			return nil, fmt.Errorf("[gRPC] 加载客户端证书失败: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return credentials.NewTLS(tlsCfg), nil
}

// buildServiceConfig 生成 gRPC 默认 serviceConfig（JSON），包含负载均衡策略，
// 以及（启用时）作用于所有方法的重试策略。
//
// 重试通过 gRPC 内置的 retryPolicy 实现，仅对返回 retryableStatusCodes 中状态码
// 的调用生效。注意：只应对幂等方法开启，非幂等方法重试可能造成重复副作用。
func (c *GrpcClient) buildServiceConfig() string {
	lb := c.cfg.LoadBalancing
	if lb == "" {
		lb = "round_robin"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`{"loadBalancingConfig":[{"%s":{}}]`, lb))

	if c.cfg.RetryEnable {
		maxAttempts := c.cfg.RetryMaxAttempts
		if maxAttempts < 2 {
			maxAttempts = 2 // gRPC 要求 >= 2 才有意义（含首次）
		}
		if maxAttempts > 5 {
			maxAttempts = 5 // gRPC 硬上限
		}
		initBackoff := c.cfg.RetryInitialBackoff
		if initBackoff == "" {
			initBackoff = "0.1s"
		}
		maxBackoff := c.cfg.RetryMaxBackoff
		if maxBackoff == "" {
			maxBackoff = "1s"
		}
		multiplier := c.cfg.RetryBackoffMultiplier
		if multiplier <= 1 {
			multiplier = 2.0
		}

		sb.WriteString(fmt.Sprintf(
			`,"methodConfig":[{"name":[{}],"retryPolicy":{"maxAttempts":%d,"initialBackoff":%q,"maxBackoff":%q,"backoffMultiplier":%g,"retryableStatusCodes":[%s]}}]`,
			maxAttempts, initBackoff, maxBackoff, multiplier, joinRetryCodes(c.cfg.RetryableStatusCodes),
		))
	}

	sb.WriteString("}")
	return sb.String()
}

// joinRetryCodes 把状态码切片转为 serviceConfig 需要的带引号、逗号分隔的 JSON 数组内容。
// 为空时回退到 "UNAVAILABLE"，避免生成非法的空数组（gRPC 会拒绝空 retryableStatusCodes）。
func joinRetryCodes(codes []string) string {
	quoted := make([]string, 0, len(codes))
	for _, code := range codes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code != "" {
			quoted = append(quoted, `"`+code+`"`)
		}
	}
	if len(quoted) == 0 {
		return `"UNAVAILABLE"`
	}
	return strings.Join(quoted, ",")
}

// msToDurationStr 把毫秒数转为 protobuf duration JSON 接受的秒格式（如 100 → "0.1s"，1000 → "1s"）。
// gRPC serviceConfig 的退避字段不接受 "100ms" 形式，必须是以秒为单位的小数。
func msToDurationStr(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%gs", float64(ms)/1000.0)
}
