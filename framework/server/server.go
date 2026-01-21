// Package server 包注释
// @author wanlizhan
// @created 2025-04-09
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"sort"
	"strings"
	"time"

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
}

func NewApp(schema string) *Server {
	if schema == "" {
		schema = "Server"
	}

	if _, err := tracer.SetupTracer(context.Background(), schema); err != nil {
		gaia.Error("Failed to setup tracer: " + err.Error())
	}

	var configs []config.Option
	host := gaia.GetSafeConfString(schema + ".Host")
	port := gaia.GetSafeConfString(schema + ".Port")
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
		crtPath := gaia.GetSafeConfString(schema + ".TLS.CrtPath")
		keyPath := gaia.GetSafeConfString(schema + ".TLS.KeyPath")
		c := &tls.Config{}
		pair, err := tls.LoadX509KeyPair(crtPath, keyPath)
		if err != nil {
			gaia.ErrorF("failed to load TLS certs: %v", err)
			return nil
		}
		c.Certificates = append(c.Certificates, pair)
		configs = append(configs, server.WithTLS(c))
	}
	s := &Server{
		server.New(configs...), schema,
	}
	hlog.SetLogger(&ServerLogger{}) //注册日志服务
	s.registerPlugin()
	return s
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

	if gaia.GetSafeConfBool(s.schema + ".Cors.Enable") {
		gaia.Info("启用跨域")
		s.Use(s.corsPlugin())
	} //注册跨域插件

	if gaia.GetSafeConfBool(s.schema + ".Pprof.Enable") {
		gaia.Info("启用pprof")
		pprof.Register(s.Hertz)
	}

	gaia.Info("启用链路追踪")
	s.Use(MakePlugin(tracePlugin)) //注册链路追踪插件

}

func (s *Server) RegisterCommonHandler() {
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

	s.GET("/api/common/generate", MakeHandler(generateCommon))

	s.POST("/api/common/query", MakeHandler(new(CommonQueryModel).CommonQuery))
	s.GET("/api/common/allQuery", MakeHandler(new(CommonQueryModel).GetAllCommonQuerySchema))
	s.GET("/api/common/query", MakeHandler(new(CommonQueryModel).GetQuerySchemaDetail))

	s.POST("/api/common/operate", MakeHandler(new(CommonOperateModel).CommonOperate))
	s.GET("/api/common/operate", MakeHandler(new(CommonOperateModel).GetOperateSchemaDetail))
	s.GET("/api/common/allOperate", MakeHandler(new(CommonOperateModel).GetAllCommonOperateSchema))
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
	arg.C().Next(parentCtx)
	return nil
}
