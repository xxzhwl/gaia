// Package server 包注释
// @author wanlizhan
// @created 2025-04-09
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/hertz-contrib/pprof"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/circuitbreaker"
	"github.com/xxzhwl/gaia/components/ratelimiter"
	"github.com/xxzhwl/gaia/framework/server/operateProxy"
	"github.com/xxzhwl/gaia/framework/tracer"
)

type Server struct {
	*server.Hertz
	schema string
	addr   string

	// cleanupFns 优雅关闭时按"注册逆序"逐个执行的清理回调。
	// 把限流清理、未来要接入的服务注册中心反注册、metrics provider flush
	// 等通通统一到这一根 hook 上，避免每加一个组件就给 Server 加一个字段。
	//
	// 注册顺序：插件按调用 AddCleanup 的先后顺序追加；shutdown 时按 LIFO 执行
	// （后注册的依赖通常先释放：限流器先停 → 业务子系统再退）。
	cleanupMu   sync.Mutex
	cleanupFns  []func()
	cleanupOnce sync.Once // 保证 runCleanups 只执行一次（多次 Shutdown 信号不会重复触发）
}

// ClientAuthType 客户端验证类型映射
var clientAuthTypeMap = map[string]tls.ClientAuthType{
	"NoClientCert":               tls.NoClientCert,
	"RequestClientCert":          tls.RequestClientCert,
	"RequireAnyClientCert":       tls.RequireAnyClientCert,
	"VerifyClientCertIfGiven":    tls.VerifyClientCertIfGiven,
	"RequireAndVerifyClientCert": tls.RequireAndVerifyClientCert,
}

// TLSVersion TLS版本映射
var tlsVersionMap = map[string]uint16{
	"1.0": tls.VersionTLS10,
	"1.1": tls.VersionTLS11,
	"1.2": tls.VersionTLS12,
	"1.3": tls.VersionTLS13,
}

// CipherSuite 加密套件映射
var cipherSuiteMap = map[string]uint16{
	"TLS_RSA_WITH_AES_128_CBC_SHA":                  tls.TLS_RSA_WITH_AES_128_CBC_SHA,
	"TLS_RSA_WITH_AES_256_CBC_SHA":                  tls.TLS_RSA_WITH_AES_256_CBC_SHA,
	"TLS_RSA_WITH_AES_128_GCM_SHA256":               tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	"TLS_RSA_WITH_AES_256_GCM_SHA384":               tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA":          tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
	"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA":          tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
	"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":            tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":            tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
	"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	"TLS_AES_128_GCM_SHA256":                        tls.TLS_AES_128_GCM_SHA256,
	"TLS_AES_256_GCM_SHA384":                        tls.TLS_AES_256_GCM_SHA384,
	"TLS_CHACHA20_POLY1305_SHA256":                  tls.TLS_CHACHA20_POLY1305_SHA256,
}

func NewApp(schema string) *Server {
	if schema == "" {
		schema = "Server"
	}

	port := gaia.GetSafeConfString(schema + ".Port")
	return newApp(schema, port)
}

// NewAppWithPort 创建一个新的Server实例，并指定端口号
func NewAppWithPort(port string) *Server {
	return newApp("", port)
}

func newApp(schema, port string) *Server {
	if schema == "" {
		schema = "Server"
	}
	if _, err := tracer.SetupTracer(context.Background(), schema); err != nil {
		gaia.Error("Failed to setup tracer: " + err.Error())
	}

	var configs []config.Option
	host := gaia.GetSafeConfString(schema + ".Host")
	configs = append(configs, server.WithHostPorts(host+":"+port))

	gracefulExitTime := gaia.GetSafeConfInt64(schema + ".GracefulExitTime")
	if gracefulExitTime > 0 {
		configs = append(configs, server.WithExitWaitTime(time.Duration(gracefulExitTime)*time.Second))
	}
	keepAliveTimeOut := gaia.GetSafeConfInt64(schema + ".KeepAliveTimeout")
	if (keepAliveTimeOut) > 0 {
		configs = append(configs, server.WithKeepAliveTimeout(time.Duration(keepAliveTimeOut)*time.Second))
	}
	readTimeout := gaia.GetSafeConfInt64(schema + ".ReadTimeout")
	if (readTimeout) > 0 {
		configs = append(configs, server.WithReadTimeout(time.Duration(readTimeout)*time.Second))
	}
	idleTimeout := gaia.GetSafeConfInt64(schema + ".IdleTimeout")
	if (idleTimeout) > 0 {
		configs = append(configs, server.WithIdleTimeout(time.Duration(idleTimeout)*time.Second))
	}
	maxRequestBodySize := gaia.GetSafeConfInt64(schema + ".MaxRequestBodySize")
	if (maxRequestBodySize) > 0 {
		configs = append(configs, server.WithMaxRequestBodySize(int(maxRequestBodySize)))
	}

	enableTls := gaia.GetSafeConfBool(schema + ".EnableTLS")
	if enableTls {
		tlsConfig := buildTLSConfig(schema)
		if tlsConfig != nil {
			configs = append(configs, server.WithTLS(tlsConfig))
		}
	}
	s := &Server{
		Hertz:  server.New(configs...),
		schema: schema,
		addr:   host + ":" + port,
	}
	hlog.SetLogger(&ServerLogger{}) //注册日志服务
	s.registerPlugin()
	return s
}

// buildTLSConfig 构建TLS配置
func buildTLSConfig(schema string) *tls.Config {
	crtPath := gaia.GetSafeConfString(schema + ".TLS.CrtPath")
	keyPath := gaia.GetSafeConfString(schema + ".TLS.KeyPath")

	if crtPath == "" || keyPath == "" {
		gaia.Error("TLS证书路径未配置")
		return nil
	}

	c := &tls.Config{}

	// 加载服务器证书
	pair, err := tls.LoadX509KeyPair(crtPath, keyPath)
	if err != nil {
		gaia.ErrorF("加载TLS证书失败: %v", err)
		return nil
	}
	c.Certificates = append(c.Certificates, pair)

	// 加载CA证书（用于客户端证书验证）
	caPath := gaia.GetSafeConfString(schema + ".TLS.CAPath")
	if caPath != "" {
		caCert, err := os.ReadFile(caPath)
		if err != nil {
			gaia.ErrorF("加载CA证书失败: %v", err)
		} else {
			caCertPool := x509.NewCertPool()
			if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
				gaia.Error("解析CA证书失败")
			} else {
				c.ClientCAs = caCertPool
			}
		}
	}

	// 客户端验证模式
	clientAuth := gaia.GetSafeConfString(schema + ".TLS.ClientAuth")
	if clientAuth != "" {
		if authType, ok := clientAuthTypeMap[clientAuth]; ok {
			c.ClientAuth = authType
			gaia.InfoF("TLS客户端验证模式: %s", clientAuth)
		} else {
			gaia.WarnF("未知的TLS客户端验证模式: %s, 使用默认", clientAuth)
		}
	}

	// 加密套件配置
	cipherSuites := gaia.GetSafeConfSlice[string](schema + ".TLS.CipherSuites")
	if len(cipherSuites) > 0 {
		var suites []uint16
		for _, suite := range cipherSuites {
			if cipher, ok := cipherSuiteMap[suite]; ok {
				suites = append(suites, cipher)
			} else {
				gaia.WarnF("未知的加密套件: %s", suite)
			}
		}
		if len(suites) > 0 {
			c.CipherSuites = suites
			gaia.InfoF("TLS加密套件已配置: %d个", len(suites))
		}
	}

	// 最小TLS版本
	minVersion := gaia.GetSafeConfString(schema + ".TLS.MinVersion")
	if minVersion != "" {
		if version, ok := tlsVersionMap[minVersion]; ok {
			c.MinVersion = version
			gaia.InfoF("TLS最小版本: %s", minVersion)
		} else {
			gaia.WarnF("未知的TLS版本: %s", minVersion)
		}
	}

	// 最大TLS版本
	maxVersion := gaia.GetSafeConfString(schema + ".TLS.MaxVersion")
	if maxVersion != "" {
		if version, ok := tlsVersionMap[maxVersion]; ok {
			c.MaxVersion = version
			gaia.InfoF("TLS最大版本: %s", maxVersion)
		} else {
			gaia.WarnF("未知的TLS版本: %s", maxVersion)
		}
	}

	// 跳过证书验证（仅用于开发环境）
	c.InsecureSkipVerify = gaia.GetSafeConfBool(schema + ".TLS.InsecureSkipVerify")
	if c.InsecureSkipVerify {
		gaia.Warn("TLS证书验证已跳过（仅用于开发环境）")
	}

	// 配置曲线偏好
	c.PreferServerCipherSuites = gaia.GetSafeConfBool(schema + ".TLS.PreferServerCipherSuites")

	// 配置曲线
	curvePreferences := gaia.GetSafeConfSlice[string](schema + ".TLS.CurvePreferences")
	if len(curvePreferences) > 0 {
		var curves []tls.CurveID
		for _, curve := range curvePreferences {
			switch curve {
			case "P256":
				curves = append(curves, tls.CurveP256)
			case "P384":
				curves = append(curves, tls.CurveP384)
			case "P521":
				curves = append(curves, tls.CurveP521)
			case "X25519":
				curves = append(curves, tls.X25519)
			default:
				gaia.WarnF("未知的曲线: %s", curve)
			}
		}
		if len(curves) > 0 {
			c.CurvePreferences = curves
		}
	}

	gaia.Info("TLS配置加载成功")
	return c
}

// AddCleanup 注册优雅关闭时要执行的清理回调。
//
// 适用场景（按注册顺序逆序触发）：
//   - 限流器闲置清理协程的 stop 句柄
//   - 服务注册中心（北极星 / Consul / Nacos）的反注册
//   - 自定义连接池的关闭、临时文件清理等
//
// 注意：回调内不应做长耗时操作；如必须，自行包 context.WithTimeout。
// runCleanups 由 sync.Once 保护，多次 SIGTERM/Shutdown 信号不会重复触发。
func (s *Server) AddCleanup(f func()) {
	if f == nil {
		return
	}
	s.cleanupMu.Lock()
	s.cleanupFns = append(s.cleanupFns, f)
	s.cleanupMu.Unlock()
}

// runCleanups 按 LIFO 顺序执行所有已注册的清理回调，且仅执行一次。
func (s *Server) runCleanups() {
	s.cleanupOnce.Do(func() {
		s.cleanupMu.Lock()
		fns := append([]func(){}, s.cleanupFns...)
		s.cleanupMu.Unlock()
		// LIFO：后注册的先释放（通常依赖关系是后注册依赖先注册的资源）
		for i := len(fns) - 1; i >= 0; i-- {
			func(fn func()) {
				defer func() {
					if r := recover(); r != nil {
						gaia.WarnF("cleanup 回调 panic: %v", r)
					}
				}()
				fn()
			}(fns[i])
		}
	})
}

func DefaultApp() *Server {
	return NewApp("")
}

// Run 启动HTTP服务器并显示注册的路由信息
func (s *Server) Run() {
	routes := s.Routes()

	// 步骤1: 排序路由
	s.sortRoutes(routes)

	// 步骤2: 生成格式化的路由信息
	routesInfo := s.formatRoutesInfo(routes)

	// 步骤3: 打印路由信息
	gaia.Info(routesInfo)

	// 步骤4: 注册优雅关闭钩子
	s.registerShutdownHook()

	// 步骤5: 发送启动通知
	sendLifecycleNotify("HTTP", s.addr, s.schema, "启动", map[string]any{
		"route_count": len(routes),
	})

	// 步骤6: 启动服务器
	s.Spin()
}

// registerShutdownHook 注册优雅关闭钩子
func (s *Server) registerShutdownHook() {
	s.OnShutdown = append(s.OnShutdown, func(ctx context.Context) {
		gaia.Info("正在执行注册的清理回调...")
		s.runCleanups()
		gaia.Info("清理回调已全部执行完毕")

		gaia.Info("正在停止追踪系统...")
		traceCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tracer.ShutdownTracer(traceCtx); err != nil {
			gaia.WarnF("关闭追踪系统失败: %s", err.Error())
		}
		gaia.Info("追踪系统已停止")

		sendLifecycleNotify("HTTP", s.addr, s.schema, "停止", map[string]any{
			"cleanup_count": len(s.cleanupFns),
		})

		gaia.Info("正在停止日志服务...")
		if logger := gaia.GetLogger(); logger != nil {
			logger.Stop()
		}
		gaia.Info("日志服务已停止")
	})
}

// sortRoutes 按HTTP方法优先级和路径字母顺序排序路由
func (s *Server) sortRoutes(routes route.RoutesInfo) {
	// 定义HTTP方法优先级
	methodPriority := map[string]int{
		"GET":     1,
		"POST":    2,
		"PUT":     3,
		"DELETE":  4,
		"PATCH":   5,
		"HEAD":    6,
		"OPTIONS": 7,
	}

	// 使用 sort.Slice 进行排序，O(n log n)
	sort.Slice(routes, func(i, j int) bool {
		iPrio := methodPriority[string(routes[i].Method)]
		jPrio := methodPriority[string(routes[j].Method)]

		// 设置默认优先级（未定义的方法放到最后）
		if iPrio == 0 {
			iPrio = 999
		}
		if jPrio == 0 {
			jPrio = 999
		}

		// 优先按方法优先级排序，优先级相同时按路径字母顺序排序
		return iPrio < jPrio || (iPrio == jPrio && string(routes[i].Path) < string(routes[j].Path))
	})
}

// formatRoutesInfo 生成格式化的路由信息表格
func (s *Server) formatRoutesInfo(routes route.RoutesInfo) string {
	// 生成分隔线
	dashLine := s.generateDashLine(80)

	// 初始化结果字符串
	result := "\n" + "=" + dashLine + "=\n"

	// 添加居中标题
	result += "|" + s.centerString("REGISTERED ROUTES ("+fmt.Sprintf("%d", len(routes))+")", 82) + "|\n"
	result += "=" + dashLine + "=\n"

	// 添加表头
	result += "| METHOD   | PATH                                               | HANDLER |\n"
	result += "=" + dashLine + "=\n"

	// 添加每条路由信息
	for _, route := range routes {
		result += s.formatRoute(route)
	}

	// 添加底部分隔线
	result += "=" + dashLine + "=\n"

	return result
}

// 优化生成虚线的实现
func (s *Server) generateDashLine(length int) string {
	return strings.Repeat("-", length)
}

// 优化居中字符串的实现
func (s *Server) centerString(str string, width int) string {
	padding := (width - len(str)) / 2
	if padding <= 0 {
		return str
	}
	return strings.Repeat(" ", padding) + str + strings.Repeat(" ", width-len(str)-padding)
}

// formatRoute 格式化单条路由信息
func (s *Server) formatRoute(route route.RouteInfo) string {
	method := string(route.Method)
	path := string(route.Path)
	handler := route.Handler

	// 获取方法对应的颜色
	color := s.getMethodColor(method)
	resetColor := "\033[0m"
	formattedMethod := color + method + resetColor

	// 限制路径长度显示
	if len(path) > 50 {
		path = path[:47] + "..."
	}

	// 格式化输出
	return fmt.Sprintf("| %-8s | %-50s | %s |\n", formattedMethod, path, handler)
}

// getMethodColor 获取HTTP方法对应的终端颜色代码
func (s *Server) getMethodColor(method string) string {
	colors := map[string]string{
		"GET":     "\033[32m", // 绿色
		"POST":    "\033[34m", // 蓝色
		"PUT":     "\033[33m", // 黄色
		"DELETE":  "\033[31m", // 红色
		"PATCH":   "\033[35m", // 紫色
		"HEAD":    "\033[36m", // 青色
		"OPTIONS": "\033[37m", // 白色
	}

	if color, ok := colors[method]; ok {
		return color
	}
	// 默认无颜色
	return ""
}

func (s *Server) registerPlugin() {
	gaia.Info("启用服务日志")
	s.Use(s.defaultServerLogger())

	// 启用 RequestID 响应头回写：让客户端能从 X-Request-Id 拿到本次请求 ID，
	// 必须在 defaultServerLogger 之后（RequestID 已通过 BuildHttpContextTrace 写入 ctx）。
	// X-Trace-Id 不再由本插件写入，统一由 tracePlugin 以 OTel TraceID 写入。
	gaia.Info("启用 RequestID 响应头回写")
	s.Use(s.requestIdHeaderPlugin())

	// 启用统一错误处理
	gaia.Info("启用统一错误处理")
	s.Use(ErrorHandler())

	// 链路追踪：上提到 metrics/限流/熔断 之前。
	// 原因：被限流/熔断拒绝的请求也需要走 trace——排障时不会出现“留下 5xx
	// 却查不到 span”的黑洞；并且子 span 中的 trace_id 会同步到 gaia.ContextTrace，
	// 与后续设置的 metrics 标签 / 日志中的 trace_id 保持一致。
	gaia.Info("启用链路追踪")
	s.Use(MakePlugin(tracePlugin))

	// 启用 HTTP 指标采集（QPS / 时延 / 在途 / 响应大小）。
	// metrics 系统未启用时 otel.Meter 返回 noop，几乎零开销。
	gaia.Info("启用 HTTP 指标采集")
	s.Use(s.metricsPlugin(initHTTPMetrics()))

	// 安全头中间件（HSTS / X-Frame-Options / CSP 等浏览器侧防护）
	if s.securityHeadersEnabled() {
		gaia.Info("启用安全响应头")
		s.Use(s.securityHeadersPlugin())
	}

	if gaia.GetSafeConfBool(s.schema + ".Cors.Enable") {
		gaia.Info("启用跨域")
		s.Use(s.corsPlugin())
	} //注册跨域插件

	if gaia.GetSafeConfBool(s.schema + ".Pprof.Enable") {
		gaia.Info("启用pprof")
		pprof.Register(s.Hertz)
	}

	// 启用单请求超时控制（业务 handler 超时上限 + 慢请求告警）。
	// 需在限流/熔断之前，满足“超时后立即释放连接”的生产需求。
	if gaia.GetSafeConfBool(s.schema + ".Timeout.Enable") {
		gaia.Info("启用请求超时控制")
		s.Use(s.timeoutPlugin())
	}

	// 启用请求限流
	if gaia.GetSafeConfBool(s.schema + ".RateLimit.Enable") {
		gaia.Info("启用请求限流")
		s.Use(s.rateLimitPlugin())
	}

	// 启用熔断器（按 path 维度独立熔断，保护下游 + 快速失败）。
	// 顺序：限流在熔断之前——先限流避免问题请求污染熔断器统计。
	if gaia.GetSafeConfBool(s.schema + ".CircuitBreaker.Enable") {
		gaia.Info("启用熔断器")
		s.Use(s.circuitBreakerPlugin())
	}

	// gzip 响应压缩：放在所有可观测性中间件之后、业务 handler 之前，
	// 这样 metrics 记录的是“压缩后”的响应字节数，与实际出网报文大小一致。
	if gaia.GetSafeConfBool(s.schema + ".Gzip.Enable") {
		gaia.Info("启用 gzip 压缩")
		s.Use(s.gzipPlugin())
	}

	// 注册 K8s 探针：/livez（进程存活）+ /readyz（依赖就绪）+ /health（兼容老调用）
	s.registerProbeRoutes()
	// 注册健康检查（向后兼容：/health 等价于 /livez）
	s.registerHealthCheck()
	// 按需在主端口挂载 Prometheus /metrics（默认关闭，由独立端口暴露）
	s.registerMetricsRoute()
}

// registerHealthCheck 注册健康检查接口（向后兼容老调用方）。
// 新代码应使用 /livez（进程级存活）或 /readyz（依赖级就绪），见 registerProbeRoutes。
func (s *Server) registerHealthCheck() {
	s.GET("/health", MakeHandler(func(req Request) (map[string]any, error) {
		return map[string]any{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		}, nil
	}))
}

// rateLimitPlugin 请求限流插件
//
// 直接复用 components/ratelimiter 的本地令牌桶 + 按 IP+Path 隔离 + 闲置自动清理实现。
// 配置项:
//   - <schema>.RateLimit.Capacity: 令牌桶突发容量（默认 100）
//   - <schema>.RateLimit.Rate:     令牌生成速率（默认 50/秒）
//   - <schema>.RateLimit.IdleTTL:  key 闲置回收时间（秒），<=0 时不清理；默认 1800（30 分钟）
func (s *Server) rateLimitPlugin() app.HandlerFunc {
	capacity := gaia.GetSafeConfInt64WithDefault(s.schema+".RateLimit.Capacity", 100)
	if capacity <= 0 {
		capacity = 100
	}
	rate := gaia.GetSafeConfFloat64WithDefault(s.schema+".RateLimit.Rate", 50.0)
	if rate <= 0 {
		rate = 50.0
	}
	idleTTLSec := gaia.GetSafeConfInt64WithDefault(s.schema+".RateLimit.IdleTTL", 1800)

	opt := ratelimiter.MiddlewareOption{
		OnReject: func(_ context.Context, c *app.RequestContext, _ string) {
			c.JSON(http.StatusTooManyRequests, Response{
				Code: http.StatusTooManyRequests,
				Msg:  "请求过于频繁，请稍后再试",
			})
		},
	}
	if idleTTLSec > 0 {
		opt.IdleTTL = time.Duration(idleTTLSec) * time.Second
		// CleanupInterval 留空，由组件自行选择 IdleTTL/3（最小 1 分钟）
	}

	mw, stop := ratelimiter.HertzMiddlewareByKeyWithCleanup(
		ratelimiter.KeyByIPAndPath(),
		func(_ string) ratelimiter.Limiter {
			return ratelimiter.NewLocalLimiter(rate, int(capacity))
		},
		opt,
	)
	// 把 stop 句柄挂到统一的 cleanupFns 上；shutdown 时由 runCleanups 触发。
	if stop != nil {
		s.AddCleanup(stop)
	}

	// 包一层探针豁免：/livez|/readyz 不占限流名额，避免 K8s 高频探活拍费额度。
	return func(c context.Context, ctx *app.RequestContext) {
		if isProbeRequest(ctx) {
			ctx.Next(c)
			return
		}
		mw(c, ctx)
	}
}

// circuitBreakerPlugin 熔断器插件
//
// 调用 components/circuitbreaker 提供的 HertzMiddleware，默认按路由模板（FullPath）隔离。
// 配置项:
//   - <schema>.CircuitBreaker.FailureThreshold:    失败率阈值（0~1，默认 0.5）
//   - <schema>.CircuitBreaker.MinRequests:         评估最少请求数（默认 20）
//   - <schema>.CircuitBreaker.ConsecutiveFailures: 连续失败阈值（0=关闭该判据）
//   - <schema>.CircuitBreaker.OpenTimeoutSeconds:  Open 状态持续时间（默认 10s）
//   - <schema>.CircuitBreaker.HalfOpenMaxCalls:    HalfOpen 探活并发额度（默认 1）
//   - <schema>.CircuitBreaker.CountWindowSeconds:  统计滑窗时长（默认 10s）
//   - <schema>.CircuitBreaker.KeyDimension:        “path”（默认）| “method_path”
func (s *Server) circuitBreakerPlugin() app.HandlerFunc {
	prefix := s.schema + ".CircuitBreaker."
	settings := circuitbreaker.Settings{
		Name:                s.schema,
		FailureThreshold:    gaia.GetSafeConfFloat64WithDefault(prefix+"FailureThreshold", 0.5),
		MinRequests:         uint32(gaia.GetSafeConfInt64WithDefault(prefix+"MinRequests", 20)),
		ConsecutiveFailures: uint32(gaia.GetSafeConfInt64WithDefault(prefix+"ConsecutiveFailures", 0)),
		OpenTimeout:         time.Duration(gaia.GetSafeConfInt64WithDefault(prefix+"OpenTimeoutSeconds", 10)) * time.Second,
		HalfOpenMaxCalls:    uint32(gaia.GetSafeConfInt64WithDefault(prefix+"HalfOpenMaxCalls", 1)),
		CountWindow:         time.Duration(gaia.GetSafeConfInt64WithDefault(prefix+"CountWindowSeconds", 10)) * time.Second,
		OnStateChange: func(name string, from, to circuitbreaker.State) {
			gaia.WarnF("熔断器状态变化: name=%s %s -> %s", name, from, to)
		},
	}

	var keyFn circuitbreaker.HTTPKeyFunc
	switch gaia.GetSafeConfStringWithDefault(prefix+"KeyDimension", "path") {
	case "method_path":
		keyFn = circuitbreaker.KeyByMethodAndPath()
	default:
		keyFn = circuitbreaker.KeyByPath()
	}

	cbMw := circuitbreaker.HertzMiddleware(circuitbreaker.HTTPSettings{
		Settings: settings,
		KeyFn:    keyFn,
		OnReject: func(_ context.Context, c *app.RequestContext, key string, err error) {
			c.JSON(http.StatusServiceUnavailable, Response{
				Code: http.StatusServiceUnavailable,
				Msg:  fmt.Sprintf("熔断器已触发：%s (%s)", key, err.Error()),
			})
		},
	})

	// 探针豁免：探针请求不该被熔断器统计，否则探针本身并发起伏会在应用启动期
	// 推高失败率，可能误触发熔断（尤其是以 path 为维度独立熔断时）。
	return func(c context.Context, ctx *app.RequestContext) {
		if isProbeRequest(ctx) {
			ctx.Next(c)
			return
		}
		cbMw(c, ctx)
	}
}

func (s *Server) RegisterCommonHandler(routeGroup *route.RouterGroup, middlewares ...app.HandlerFunc) {
	if err := gaia.MkDirAll(DefaultCommonOperateFolder, 0755); err != nil {
		gaia.Error(err.Error())
		return
	}
	if err := gaia.MkDirAll(DefaultCommonQueryFolder, 0755); err != nil {
		gaia.Error(err.Error())
		return
	}
	// 使用工厂方法注册：每次请求都会生成独立的 DefaultWriter 实例，
	// 避免并发请求间 SetContext / SetDbSchema 互相覆盖（数据竞争）。
	defaultWriterFactory := func() operateProxy.OperateModel { return &DefaultWriter{} }
	operateProxy.RegisterOperateModelFactory("default", defaultWriterFactory)
	operateProxy.RegisterOperateModelFactory("Default", defaultWriterFactory)
	// 同时保留单例注册以兼容老的查询接口（GetAllCommonOperateSchema 等）；
	// 真正执行 Insert/Update/Delete 时工厂优先级更高。
	operateProxy.RegisterOperateModel("default", &DefaultWriter{})
	operateProxy.RegisterOperateModel("Default", &DefaultWriter{})

	// 注册公共路由
	commonGroup := routeGroup.Group("/common")
	commonGroup.Use(middlewares...)

	commonGroup.GET("generate", MakeHandler(generateCommon))

	commonGroup.POST("/query", MakeHandler(new(CommonQueryModel).CommonQuery))
	commonGroup.GET("/allQuery", MakeHandler(new(CommonQueryModel).GetAllCommonQuerySchema))
	commonGroup.GET("/query", MakeHandler(new(CommonQueryModel).GetQuerySchemaDetail))

	commonGroup.POST("/operate", MakeHandler(new(CommonOperateModel).CommonOperate))
	commonGroup.GET("/operate", MakeHandler(new(CommonOperateModel).GetOperateSchemaDetail))
	commonGroup.GET("/allOperate", MakeHandler(new(CommonOperateModel).GetAllCommonOperateSchema))
}

// hertzHeaderCarrier 将 Hertz 的 RequestHeader 适配为 OTel propagation.TextMapCarrier。
// 主要用于从上游请求中提取 traceparent / tracestate / baggage，
// 以保证跨服务调用链路能够联贯。
type hertzHeaderCarrier struct {
	c *app.RequestContext
}

func (h hertzHeaderCarrier) Get(key string) string {
	return string(h.c.GetHeader(key))
}

func (h hertzHeaderCarrier) Set(key, value string) {
	h.c.Response.Header.Set(key, value)
}

func (h hertzHeaderCarrier) Keys() []string {
	keys := make([]string, 0)
	h.c.Request.Header.VisitAll(func(k, _ []byte) {
		keys = append(keys, string(k))
	})
	return keys
}

// buildSpanStartOptions 按 OTel HTTP 语义约定填充 span 属性。
func buildSpanStartOptions(c Request) []trace.SpanStartOption {
	req := &c.c.Request
	clientIP := c.c.ClientIP()

	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(string(req.Method())),
		semconv.HTTPRoute(c.c.FullPath()),
		semconv.URLPath(string(req.URI().Path())),
		semconv.URLFull(string(req.URI().FullURI())),
		semconv.UserAgentOriginal(string(req.Header.UserAgent())),
		attribute.String("client.address", clientIP),
		attribute.String(RequestIdKey, c.c.GetString(RequestIdKey)),
		attribute.String(TraceIdKey, c.c.GetString(TraceIdKey)),
		attribute.String(FullPathKey, string(req.URI().FullURI())),
	}
	if host := string(req.Host()); host != "" {
		attrs = append(attrs, semconv.ServerAddress(host))
	}
	if q := string(req.URI().QueryString()); q != "" {
		attrs = append(attrs, semconv.URLQuery(q))
	}

	return []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	}
}

// tracePlugin 链路追踪中间件：
//  1. 从请求 header 提取上游 SpanContext（W3C TraceContext / Baggage）；
//  2. 创建 server span 并使用 OTel HTTP 语义约定填充属性；
//  3. 请求完成后补充 http.status_code 并根据状态码设置 span.Status；
//  4. 捕获 panic 并记录到 span，保证 trace 可见；
//  5. 在响应 header 中回写 trace-id，方便客户端排查。
func tracePlugin(arg Request) error {
	// tracer.GetTracer() 内部已做兜底，未初始化时会回退到全局 otel.Tracer("default")，
	// framework.Init() 启动阶段也已 SetupTracer，这里无需再 lazy init。
	tracerInstance := tracer.GetTracer()

	// 1) 提取上游 trace 上下文，保证跨服务调用链路联贯。
	carrier := hertzHeaderCarrier{c: arg.c}
	parentCtx := otel.GetTextMapPropagator().Extract(arg.TraceContext, carrier)

	// 2) 使用上游 ctx 启动 server span。
	traceName := fmt.Sprintf("%s %s", string(arg.C().Method()), arg.C().FullPath())
	spanCtx, span := tracerInstance.Start(parentCtx, traceName, buildSpanStartOptions(arg)...)

	// 3) 在响应 header 回写 trace 上下文（traceparent / tracestate），方便客户端排查。
	otel.GetTextMapPropagator().Inject(spanCtx, carrier)
	if sc := span.SpanContext(); sc.HasTraceID() {
		arg.c.Response.Header.Set("X-Trace-Id", sc.TraceID().String())
	}

	// 4) 同步到 gaia ContextTrace，旧代码逻辑保持一致。
	gaiaCtx := gaia.GetContextTrace()
	createdGaiaCtx := false
	if gaiaCtx == nil {
		gaia.BuildContextTrace()
		gaiaCtx = gaia.GetContextTrace()
		createdGaiaCtx = true
	}
	if gaiaCtx != nil {
		gaiaCtx.ParentContext = spanCtx
		// 关键：把 OTel TraceID 反向同步到 gaia.ContextTrace.TraceId
		// 让"响应头 X-Trace-Id"、"日志里的 trace_id"、"链路平台上的 traceID"
		// 这三个客户端会去对照查询的字段保持完全一致；否则 gaia 自有 TraceID
		// 与 OTel TraceID 是两套体系，前后台对不上。
		if sc := span.SpanContext(); sc.HasTraceID() {
			otelTraceID := sc.TraceID().String()
			gaiaCtx.TraceId = otelTraceID
			// 同步覆盖 ctx 中的 TraceIdKey，业务侧 ctx.GetString(TraceIdKey)
			// 也能拿到与响应头一致的 ID
			arg.c.Set(TraceIdKey, otelTraceID)
		}
	}

	defer func() {
		// 5) panic 记录到 span。MakePlugin 会二次 recover，这里先记录到 trace。
		if r := recover(); r != nil {
			span.RecordError(fmt.Errorf("panic: %v", r), trace.WithStackTrace(true))
			span.SetStatus(codes.Error, fmt.Sprintf("panic: %v", r))
			span.End()
			if createdGaiaCtx {
				gaia.RemoveContextTrace()
			}
			panic(r) // 交给上层 ErrorHandler / MakePlugin 统一响应
		}

		// 6) 记录响应状态码并设置 Span Status。
		status := arg.c.Response.StatusCode()
		span.SetAttributes(semconv.HTTPResponseStatusCode(status))
		switch {
		case status >= 500:
			span.SetStatus(codes.Error, http.StatusText(status))
		case status >= 400:
			// 4xx 在 OTel 规范中不被视为 server error，保持 Unset 即可。
		}
		span.End()

		if createdGaiaCtx {
			gaia.RemoveContextTrace()
		}
	}()

	arg.C().Set("ParentContext", spanCtx)
	arg.C().Next(spanCtx)
	return nil
}

// sendLifecycleNotify 发送服务生命周期通知
func sendLifecycleNotify(protocol, addr, schema, action string, fields map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			gaia.WarnF("sendLifecycleNotify panic: %v", r)
		}
	}()
	if err := gaia.SendLifecycleNotify(gaia.LifecycleNotifyInfo{
		Kind:     "service",
		Name:     protocol,
		Action:   action,
		Status:   lifecycleStatus(action),
		Protocol: protocol,
		Address:  addr,
		Schema:   schema,
		Fields:   fields,
	}); err != nil {
		gaia.WarnF("[%s] 发送%s通知失败: %s", protocol, action, err.Error())
	}
}

func lifecycleStatus(action string) string {
	switch action {
	case "启动", "恢复":
		return "running"
	case "停止":
		return "stopped"
	default:
		return action
	}
}
