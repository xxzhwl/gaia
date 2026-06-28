// Package rpcclient gRPC 客户端拦截器：链路追踪、日志、指标、鉴权注入。
//
// @author gaia-framework
// @created 2026-06-24
package rpcclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/rpcmetric"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

var clientLogger *logImpl.DefaultLogger

func init() {
	clientLogger = logImpl.NewDefaultLogger().SetTitle("RpcClient")
}

// ===== 链路追踪 =====

func tracingInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		tr := tracer.GetTracer()
		if tr == nil {
			// 即使未启用 OTel，也注入 gaia 链路信息，保证服务端日志 TraceId 续接
			return invoker(injectGaiaTrace(ctx), method, req, reply, cc, opts...)
		}
		ctx, span := tr.Start(ctx, method, oteltrace.WithSpanKind(oteltrace.SpanKindClient))
		defer span.End()

		// 把 OTel + gaia 链路上下文通过 metadata 透传到服务端
		ctx = injectTraceContext(ctx)

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		return err
	}
}

// injectTraceContext 把 OTel span 上下文 + gaia 链路信息注入到 outgoing metadata。
func injectTraceContext(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.MD{}
	} else {
		md = md.Copy()
	}
	otel.GetTextMapPropagator().Inject(ctx, propagationCarrier{md: md})
	setGaiaTraceMD(md)
	return metadata.NewOutgoingContext(ctx, md)
}

// injectGaiaTrace 仅注入 gaia 链路信息（不依赖 OTel），用于 tracer 未启用时。
func injectGaiaTrace(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.MD{}
	} else {
		md = md.Copy()
	}
	setGaiaTraceMD(md)
	return metadata.NewOutgoingContext(ctx, md)
}

// setGaiaTraceMD 把当前 gaia 链路的 TraceId 与来源系统名写入 metadata，
// 供服务端续接（key 与服务端 rpcserver 约定一致：traceid / followfrom）。
func setGaiaTraceMD(md metadata.MD) {
	gc := gaia.GetContextTrace()
	if gc == nil {
		return
	}
	if gc.TraceId != "" {
		md.Set("traceid", gc.TraceId)
	}
	if name := gaia.GetSystemEnName(); name != "" {
		md.Set("followfrom", name)
	}
}

// propagationCarrier 适配 OTel TextMapCarrier 到 gRPC metadata。
type propagationCarrier struct{ md metadata.MD }

func (c propagationCarrier) Get(key string) string {
	vals := c.md.Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}
func (c propagationCarrier) Set(key, value string) { c.md.Set(key, value) }
func (c propagationCarrier) Keys() []string {
	keys := make([]string, 0, len(c.md))
	for k := range c.md {
		keys = append(keys, k)
	}
	return keys
}

// ===== 日志 =====

type loggerOptions struct {
	printConsole    bool
	enablePushLog   bool
	logBody         bool
	maxBodyLogBytes int64
}

func loadLoggerOptions(schema string) loggerOptions {
	maxBytes := gaia.GetSafeConfInt64WithDefault(schema+".Logger.MaxBodyLogBytes", 4096)
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	return loggerOptions{
		printConsole:    gaia.GetSafeConfBoolWithDefault(schema+".Logger.PrintConsole", true),
		enablePushLog:   gaia.GetSafeConfBool(schema + ".Logger.EnablePushLog"),
		logBody:         gaia.GetSafeConfBool(schema + ".Logger.LogBody"),
		maxBodyLogBytes: maxBytes,
	}
}

func loggingInterceptor(schema string) grpc.UnaryClientInterceptor {
	loggerOpts := loadLoggerOptions(schema)
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		end := time.Now()
		writeGrpcClientLog(loggerOpts, ctx, method, targetOf(cc), req, reply, err, start, end)
		return err
	}
}

func writeGrpcClientLog(opts loggerOptions, ctx context.Context, method, target string, req, resp any, err error, start, end time.Time) {
	code := grpcstatus.Code(err)
	level := grpcClientCodeToLevel(code)
	dur := end.Sub(start)

	var reqStr, respStr string
	if opts.enablePushLog {
		reqStr = sanitizeRpcPayloadForRemote(opts, req)
		respStr = sanitizeRpcPayloadForRemote(opts, resp)
	}

	content := fmt.Sprintf("[gRPC-Client] [%s ~ %s] | %s | %13v | %s | %s",
		start.Format(gaia.DateTimeMillsFormat),
		end.Format(gaia.DateTimeMillsFormat),
		code.String(), dur, target, method)

	errStr := ""
	if err != nil {
		errStr = grpcstatus.Convert(err).Message()
		content += " | err=" + errStr
	}

	if opts.printConsole {
		clientLogger.OutLog(level, content)
	}
	skip := skipPushLog(ctx)
	if opts.enablePushLog && !skip {
		clientLogger.OutLogBody(level, content, logImpl.NewGRPCAccessLog(logImpl.AccessDirectionOutbound, logImpl.GrpcLogBaseModel{
			FullMethod:     method,
			Kind:           "unary",
			Peer:           target,
			Code:           code.String(),
			Metadata:       outgoingMDToHeader(ctx),
			ReqBody:        reqStr,
			RespBody:       respStr,
			StartTime:      start.Format(gaia.DateTimeMillsFormat),
			EndTime:        end.Format(gaia.DateTimeMillsFormat),
			StartTimeStamp: start.UnixMilli(),
			EndTimeStamp:   end.UnixMilli(),
			Duration:       float64(dur.Milliseconds()),
			Err:            errStr,
		}))
	}
}

func grpcClientCodeToLevel(code grpccodes.Code) gaia.LogLevel {
	switch code {
	case grpccodes.OK:
		return gaia.LogInfoLevel
	case grpccodes.Canceled, grpccodes.InvalidArgument, grpccodes.NotFound,
		grpccodes.AlreadyExists, grpccodes.PermissionDenied, grpccodes.Unauthenticated,
		grpccodes.FailedPrecondition, grpccodes.OutOfRange, grpccodes.ResourceExhausted:
		return gaia.LogWarnLevel
	default:
		return gaia.LogErrorLevel
	}
}

const skipPushLogMDKey = "x-gaia-skip-push-log"

// WithSkipPushLog 标记 context 跳过本次 rpcclient 出站日志推送。
// 通过 outgoing metadata 传递标记，避免 context key 跨包类型隔离问题。
func WithSkipPushLog(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, skipPushLogMDKey, "1")
}

// skipPushLog 检查是否应该跳过远程日志推送。
func skipPushLog(ctx context.Context) bool {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return false
	}
	return len(md.Get(skipPushLogMDKey)) > 0
}

func sanitizeRpcPayload(opts loggerOptions, msg any) string {
	return sanitizeRpcPayloadString(opts, serializeRpcPayload(msg))
}

func sanitizeRpcPayloadForRemote(opts loggerOptions, msg any) string {
	return logImpl.RemoteLogBodyFromString(opts.logBody, opts.maxBodyLogBytes, serializeRpcPayload(msg))
}

func serializeRpcPayload(msg any) string {
	if msg == nil {
		return ""
	}
	if b, err := json.Marshal(msg); err == nil {
		return string(b)
	}
	return fmt.Sprintf("%+v", msg)
}

func sanitizeRpcPayloadString(opts loggerOptions, s string) string {
	if s == "" {
		return ""
	}
	if !opts.logBody {
		return "[REDACTED]"
	}
	if int64(len(s)) > opts.maxBodyLogBytes {
		return fmt.Sprintf("%s...[truncated %d bytes]", s[:opts.maxBodyLogBytes], int64(len(s))-opts.maxBodyLogBytes)
	}
	return s
}

func outgoingMDToHeader(ctx context.Context) http.Header {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return nil
	}
	h := make(http.Header, len(md))
	for k, vs := range md {
		cp := make([]string, len(vs))
		copy(cp, vs)
		h[k] = cp
	}
	return logImpl.SanitizeHttpHeaders(h)
}

func targetOf(cc *grpc.ClientConn) string {
	if cc == nil {
		return ""
	}
	return cc.Target()
}

// ===== 鉴权注入 =====

// authInterceptor 在每次调用时把 token 注入到 metadata。
func authInterceptor(headerKey, scheme, token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if token != "" {
			val := token
			if scheme != "" {
				val = scheme + " " + token
			}
			ctx = metadata.AppendToOutgoingContext(ctx, headerKey, val)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// ===== 指标 =====

var (
	clientMetricsOnce sync.Once
	clientMetrics     *grpcClientMetrics
)

type grpcClientMetrics struct {
	// rec 持有 server/client 通用的 requestsTotal + duration（见 framework/rpcmetric）。
	rec rpcmetric.Recorder
}

func getClientMetrics() *grpcClientMetrics {
	clientMetricsOnce.Do(func() {
		meter := otel.Meter("github.com/xxzhwl/gaia/framework/rpcclient",
			metric.WithInstrumentationVersion("1.0.0"),
		)
		m := &grpcClientMetrics{}
		var err error
		m.rec.RequestsTotal, err = meter.Int64Counter("rpc.client.requests.total",
			metric.WithDescription("Total number of gRPC client requests by method and status code"),
		)
		otel.Handle(err)
		m.rec.Duration, err = meter.Float64Histogram("rpc.client.duration",
			metric.WithDescription("gRPC client call duration in milliseconds"),
			metric.WithUnit("ms"),
		)
		otel.Handle(err)
		clientMetrics = m
	})
	return clientMetrics
}

func metricsInterceptor() grpc.UnaryClientInterceptor {
	m := getClientMetrics()
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		code := grpcstatus.Code(err).String()
		m.rec.Record(ctx, method, code, start)
		return err
	}
}
