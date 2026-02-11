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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/server/operateProxy"
	"github.com/xxzhwl/gaia/framework/tracer"
)

type Server struct {
	*server.Hertz
	schema string

	// 限流相关字段
	rateLimiters     map[string]*rateLimiter
	rateLimitMu      sync.RWMutex
	rateLimitCleanup *time.Ticker
	stopCleanup      chan struct{}
}

// TLSConfig 扩展的TLS配置结构体
type TLSConfig struct {
	Enable             bool     // 是否启用TLS
	CertPath           string   // 证书路径
	KeyPath            string   // 私钥路径
	CAPath             string   // CA证书路径（用于客户端证书验证）
	ClientAuth         string   // 客户端验证模式: NoClientCert, RequestClientCert, RequireAnyClientCert, VerifyClientCertIfGiven, RequireAndVerifyClientCert
	CipherSuites       []string // 支持的加密套件
	MinVersion         string   // 最小TLS版本: 1.0, 1.1, 1.2, 1.3
	MaxVersion         string   // 最大TLS版本: 1.0, 1.1, 1.2, 1.3
	InsecureSkipVerify bool     // 跳过证书验证（仅用于开发环境）
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

// rateLimiter 令牌桶限流器
type rateLimiter struct {
	capacity   int        // 令牌桶容量
	rate       float64    // 令牌生成速率（个/秒）
	tokens     float64    // 当前令牌数
	lastRefill time.Time  // 上次令牌填充时间
	lastAccess time.Time  // 最后访问时间（用于清理）
	mu         sync.Mutex // 互斥锁
}

// newRateLimiter 创建新的限流器
func newRateLimiter(capacity int, rate float64) *rateLimiter {
	now := time.Now()
	return &rateLimiter{
		capacity:   capacity,
		rate:       rate,
		tokens:     float64(capacity),
		lastRefill: now,
		lastAccess: now,
	}
}

// allow 检查是否允许请求通过
func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 更新最后访问时间
	rl.lastAccess = time.Now()

	// 计算自上次填充以来的时间
	now := time.Now()
	duration := now.Sub(rl.lastRefill)

	// 填充令牌
	newTokens := duration.Seconds() * rl.rate
	rl.tokens = min(float64(rl.capacity), rl.tokens+newTokens)
	rl.lastRefill = now

	// 检查是否有足够的令牌
	if rl.tokens >= 1 {
		rl.tokens--
		return true
	}

	return false
}

// min 返回两个浮点数中的较小值
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
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
		Hertz:        server.New(configs...),
		schema:       schema,
		rateLimiters: make(map[string]*rateLimiter),
		rateLimitMu:  sync.RWMutex{},
		stopCleanup:  make(chan struct{}),
	}
	hlog.SetLogger(&ServerLogger{}) //注册日志服务
	s.registerPlugin()
	s.startRateLimiterCleanup() // 启动限流器清理协程
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

// startRateLimiterCleanup 启动限流器定期清理协程
func (s *Server) startRateLimiterCleanup() {
	// 每10分钟清理一次长时间未使用的限流器
	s.rateLimitCleanup = time.NewTicker(10 * time.Minute)
	go func() {
		for {
			select {
			case <-s.rateLimitCleanup.C:
				s.cleanupRateLimiters()
			case <-s.stopCleanup:
				return
			}
		}
	}()
}

// cleanupRateLimiters 清理长时间未使用的限流器
func (s *Server) cleanupRateLimiters() {
	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	now := time.Now()
	inactiveThreshold := 30 * time.Minute // 30分钟未使用则清理

	for key, rl := range s.rateLimiters {
		rl.mu.Lock()
		lastAccess := rl.lastAccess
		rl.mu.Unlock()

		if now.Sub(lastAccess) > inactiveThreshold {
			delete(s.rateLimiters, key)
			gaia.DebugF("清理长时间未使用的限流器: %s", key)
		}
	}

	if len(s.rateLimiters) > 0 {
		gaia.InfoF("限流器清理完成，当前活跃限流器数量: %d", len(s.rateLimiters))
	}
}

// stopRateLimiterCleanup 停止限流器清理协程
func (s *Server) stopRateLimiterCleanup() {
	if s.rateLimitCleanup != nil {
		s.rateLimitCleanup.Stop()
		close(s.stopCleanup)
	}
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

	// 步骤5: 启动服务器
	s.Spin()
}

// registerShutdownHook 注册优雅关闭钩子
func (s *Server) registerShutdownHook() {
	s.OnShutdown = append(s.OnShutdown, func(ctx context.Context) {
		gaia.Info("正在停止限流器清理协程...")
		s.stopRateLimiterCleanup()
		gaia.Info("限流器清理协程已停止")

		gaia.Info("正在停止日志服务...")
		// 停止日志服务，确保所有日志都被刷新
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

	// 冒泡排序实现
	// 使用sort.Slice进行排序，效率更高
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
	return ""
	// 默认无颜色
}

func (s *Server) registerPlugin() {
	gaia.Info("启用服务日志")
	s.Use(s.defaultServerLogger())

	// 启用统一错误处理
	gaia.Info("启用统一错误处理")
	s.Use(ErrorHandler())

	if gaia.GetSafeConfBool(s.schema + ".Cors.Enable") {
		gaia.Info("启用跨域")
		s.Use(s.corsPlugin())
	} //注册跨域插件

	if gaia.GetSafeConfBool(s.schema + ".Pprof.Enable") {
		gaia.Info("启用pprof")
		pprof.Register(s.Hertz)
	}

	// 启用请求限流
	if gaia.GetSafeConfBool(s.schema + ".RateLimit.Enable") {
		gaia.Info("启用请求限流")
		s.Use(s.rateLimitPlugin())
	}

	gaia.Info("启用链路追踪")
	s.Use(MakePlugin(tracePlugin)) //注册链路追踪插件

	// 注册健康检查接口
	s.registerHealthCheck()

}

// registerHealthCheck 注册健康检查接口
func (s *Server) registerHealthCheck() {
	s.GET("/health", MakeHandler(func(req Request) (map[string]any, error) {
		return map[string]any{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		}, nil
	}))
}

// rateLimitPlugin 请求限流插件
func (s *Server) rateLimitPlugin() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// 获取限流配置
		capacity := int(gaia.GetSafeConfInt64WithDefault(s.schema+".RateLimit.Capacity", 100))
		if capacity <= 0 {
			capacity = 100 // 默认容量100
		}

		rate := gaia.GetSafeConfFloat64WithDefault(s.schema+".RateLimit.Rate", 50.0)
		if rate <= 0 {
			rate = 50.0 // 默认速率50个/秒
		}

		// 使用路径作为限流键
		path := string(c.FullPath())
		if path == "" {
			path = string(c.Request.RequestURI())
		}

		// 获取或创建限流器
		rl := s.getRateLimiter(path, capacity, rate)

		// 检查是否允许请求通过
		if !rl.allow() {
			c.JSON(http.StatusTooManyRequests, Response{
				Code: 429,
				Msg:  "请求过于频繁，请稍后再试",
				Data: nil,
				Ext:  nil,
			})
			c.Abort()
			return
		}

		c.Next(ctx)
	}
}

// getRateLimiter 获取或创建限流器
func (s *Server) getRateLimiter(key string, capacity int, rate float64) *rateLimiter {
	s.rateLimitMu.RLock()
	rl, ok := s.rateLimiters[key]
	s.rateLimitMu.RUnlock()

	if ok {
		return rl
	}

	s.rateLimitMu.Lock()
	defer s.rateLimitMu.Unlock()

	// 双重检查
	if rl, ok := s.rateLimiters[key]; ok {
		return rl
	}

	// 创建新的限流器
	rl = newRateLimiter(capacity, rate)
	s.rateLimiters[key] = rl
	return rl
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

func buildSpanStartOptions(c Request) []trace.SpanStartOption {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String(RequestIdKey, c.c.GetString(RequestIdKey))),
		trace.WithAttributes(attribute.String(TraceIdKey, c.c.GetString(TraceIdKey))),
		trace.WithAttributes(attribute.String(FullPathKey, c.c.URI().String())),
		trace.WithAttributes(attribute.String(RequestBody, c.c.GetString(RequestBody))),
		trace.WithAttributes(attribute.String(AuthorizationKey, string(c.c.GetHeader("Authorization")))),
	}

	return opts
}

func tracePlugin(arg Request) error {
	traceName := fmt.Sprintf("%s-%s", string(arg.C().Method()), arg.C().FullPath())
	tracerInstance := tracer.GetTracer()
	if tracerInstance == nil {
		_, err := tracer.SetupTracer(arg.TraceContext, gaia.GetSystemEnName())
		if err != nil {
			gaia.Error(err.Error())
			return nil
		}
		tracerInstance = tracer.GetTracer()
	}
	parentCtx, span := tracerInstance.Start(arg.TraceContext, traceName, buildSpanStartOptions(arg)...)

	defer func() {
		span.End()
	}()
	arg.C().Set("ParentContext", parentCtx)

	gaiaCtx := gaia.GetContextTrace()
	if gaiaCtx == nil {
		gaia.BuildContextTrace()
		gaiaCtx = gaia.GetContextTrace()
		defer func() {
			gaia.RemoveContextTrace()
		}()
	}
	gaiaCtx.ParentContext = parentCtx

	arg.C().Next(parentCtx)
	return nil
}
