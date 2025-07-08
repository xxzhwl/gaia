// Package server 包注释
// @author wanlizhan
// @created 2025-04-09
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/server/operateProxy"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"time"
)

func init() {
	tracer.SetupTracer(context.Background(), "AsyncTask")
}

type Server struct {
	*server.Hertz
	schema string
}

func NewApp(schema string) *Server {
	if schema == "" {
		schema = "Server"
	}
	var configs []config.Option
	host := gaia.GetSafeConfString(schema + ".Host")
	port := gaia.GetSafeConfString(schema + ".Port")
	configs = append(configs, server.WithHostPorts(host+":"+port))

	gracefulExitTime := gaia.GetSafeConfInt64(schema + ".GracefulExitTime")
	if (gracefulExitTime) > 0 {
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
			panic(err)
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

func (s *Server) registerPlugin() {
	gaia.Info("启用服务日志")
	s.Use(s.defaultServerLogger())

	if gaia.GetSafeConfBool(s.schema + ".Cors.Enable") {
		gaia.Info("启用跨域")
		s.Use(s.corsPlugin())
	} //注册跨域插件
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
	parentCtx, span := tracerInstance.Start(context.Background(), traceName, buildSpanStartOptions(arg)...)

	defer func() {
		span.End()
	}()
	arg.C().Set("ParentContext", parentCtx)
	arg.C().Next(parentCtx)
	return nil
}
