// Package rpcserver gRPC 服务端日志拦截器。
//
// 对齐 framework/server/serverlogger.go 的能力：
//   - 跨服务链路续接：从 incoming metadata 读取上游传入的 TraceId / FollowFrom，
//     复用 gaia.BuildHttpContextTrace 续接，使日志体系的 TraceId 跨服务连续；
//   - detail / simple 两种输出模式；
//   - 请求 / 响应 payload 记录（可脱敏、按长度截断）；
//   - 按 gRPC 状态码动态分级（OK→Info，客户端错→Warn，服务端错→Error）；
//   - 可选推送远程日志（统一写入 in_log，protocol=grpc）。
//
// 配置（schema 默认 RpcServer）：
//
//	{schema}.Logger.DetailMode        bool   详细模式，输出 Req/Resp/Peer 等
//	{schema}.Logger.PrintConsole      bool   控制台 / 文件输出，默认 true
//	{schema}.Logger.EnablePushLog     bool   推送远程日志（ES），默认 false
//	{schema}.Logger.LogBody           bool   是否记录消息体，默认 false（仅记录占位）
//	{schema}.Logger.MaxBodyLogBytes   int64  消息体最大记录字节数，默认 4096
//
// @author gaia-framework
// @created 2026-06-24
package rpcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// metadata 中传递 gaia 链路信息的 key（gRPC 会把 key 统一转小写）。
// 客户端 rpcclient 注入、服务端在此读取，二者必须保持一致。
const (
	mdTraceIDKey    = "traceid"
	mdFollowFromKey = "followfrom"
)

const grpcDetailInfo = `
=========================================
gRPC Request:
	Time:%s
	Method:%s
	Peer:%s
	Req:%s
- - - - - - - - - - - - - - - - - - - - -
gRPC Response:
	Time:%s
	Code:%s
	Resp:%s
Duration:%13v
=========================================
`

const grpcSimpleInfo = `[%v ~ %v] | %s | %13v | %s | %s`

// loggerOptions gRPC 日志配置（拦截器创建时读取一次，避免每请求多次读配置）。
type loggerOptions struct {
	detailMode      bool
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
		detailMode: gaia.GetSafeConfBool(schema + ".Logger.DetailMode"),
		// 默认开启控制台输出：不配置时也应有访问日志，避免静默
		printConsole:    gaia.GetSafeConfBoolWithDefault(schema+".Logger.PrintConsole", true),
		enablePushLog:   gaia.GetSafeConfBool(schema + ".Logger.EnablePushLog"),
		logBody:         gaia.GetSafeConfBool(schema + ".Logger.LogBody"),
		maxBodyLogBytes: maxBytes,
	}
}

// grpcCodeToLevel 按 gRPC 状态码映射日志级别。
//   - OK                         → Info
//   - 客户端类错误（参数/鉴权/限流等） → Warn
//   - 服务端类错误（Internal/Unavailable 等） → Error
func grpcCodeToLevel(code grpccodes.Code) gaia.LogLevel {
	switch code {
	case grpccodes.OK:
		return gaia.LogInfoLevel
	case grpccodes.Canceled, grpccodes.InvalidArgument, grpccodes.NotFound,
		grpccodes.AlreadyExists, grpccodes.PermissionDenied, grpccodes.Unauthenticated,
		grpccodes.FailedPrecondition, grpccodes.OutOfRange, grpccodes.ResourceExhausted:
		return gaia.LogWarnLevel
	default:
		// Unknown / DeadlineExceeded / Internal / Unavailable / DataLoss / Aborted / Unimplemented ...
		return gaia.LogErrorLevel
	}
}

// buildGrpcTrace 从 incoming metadata 续接 gaia 链路。
//
// 复用 gaia.BuildHttpContextTrace（与 HTTP serverlogger 同一函数）以读取上游 TraceId，
// 保证跨服务 TraceId 连续。底层 ContextType 会被标记为 Http，但不影响日志与链路功能。
func buildGrpcTrace(ctx context.Context, peerAddr string) {
	traceID, followFrom := grpcTraceFromMD(ctx)
	gaia.BuildHttpContextTrace(traceID, peerAddr, followFrom)
}

// grpcTraceFromMD 从 incoming metadata 读取 gaia 链路信息。
func grpcTraceFromMD(ctx context.Context) (traceID, followFrom string) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ""
	}
	if v := md.Get(mdTraceIDKey); len(v) > 0 {
		traceID = v[0]
	}
	if v := md.Get(mdFollowFromKey); len(v) > 0 {
		followFrom = v[0]
	}
	return
}

// mdToHeader 把 incoming metadata 转为 http.Header 并复用统一脱敏（用于远程日志）。

type ctxKey string

const ctxKeyDisablePushLog ctxKey = "gaia-disable-push-log"

// DisablePushLog 由 handler 在 handler 逻辑中决定是否需要跳过本次请求的远程日志推送。
func DisablePushLog(ctx context.Context) {
	if setter, ok := ctx.Value(ctxKeyDisablePushLog).(*bool); ok {
		*setter = true
	}
}

// mdToHeader 把 incoming metadata 转为 http.Header 并复用统一脱敏（用于远程日志）。
func mdToHeader(ctx context.Context) http.Header {
	md, ok := metadata.FromIncomingContext(ctx)
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

// sanitizeRpcPayload 序列化并按配置脱敏 / 截断 gRPC 消息体。
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

// writeGrpcLog 统一组装并输出一条 gRPC 访问日志。
func writeGrpcLog(opts loggerOptions, ctx context.Context, fullMethod, kind, peerAddr string,
	req, resp any, err error, start, end time.Time) {

	code := status.Code(err)
	level := grpcCodeToLevel(code)
	dura := end.Sub(start)

	var reqStr, respStr string
	if opts.detailMode {
		reqStr = sanitizeRpcPayload(opts, req)
		respStr = sanitizeRpcPayload(opts, resp)
	}
	remoteReqStr, remoteRespStr := reqStr, respStr
	if opts.enablePushLog {
		remoteReqStr = sanitizeRpcPayloadForRemote(opts, req)
		remoteRespStr = sanitizeRpcPayloadForRemote(opts, resp)
	}

	var content string
	if opts.detailMode {
		content = fmt.Sprintf(grpcDetailInfo,
			start.Format(gaia.DateTimeMillsFormat), fullMethod, peerAddr, reqStr,
			end.Format(gaia.DateTimeMillsFormat), code.String(), respStr, dura)
	} else {
		content = fmt.Sprintf(grpcSimpleInfo,
			start.Format(gaia.DateTimeMillsFormat),
			end.Format(gaia.DateTimeMillsFormat),
			code.String(), dura, kind, fullMethod)
	}
	errStr := ""
	if err != nil {
		errStr = status.Convert(err).Message()
		content += " | err=" + errStr
	}

	if opts.printConsole {
		rpcLogger.InLog(level, content)
	}
	if opts.enablePushLog && !pushDisabled(ctx) {
		rpcLogger.InLogBody(level, content, logImpl.NewGRPCAccessLog(logImpl.AccessDirectionInbound, logImpl.GrpcLogBaseModel{
			FullMethod:     fullMethod,
			Kind:           kind,
			Peer:           peerAddr,
			Code:           code.String(),
			Metadata:       mdToHeader(ctx),
			ReqBody:        remoteReqStr,
			RespBody:       remoteRespStr,
			StartTime:      start.Format(gaia.DateTimeMillsFormat),
			EndTime:        end.Format(gaia.DateTimeMillsFormat),
			StartTimeStamp: start.UnixMilli(),
			EndTimeStamp:   end.UnixMilli(),
			Duration:       float64(dura.Milliseconds()),
			Err:            errStr,
		}))
	}
}

// pushDisabled 检查是否跳过本次请求的远程日志推送。
func pushDisabled(ctx context.Context) bool {
	v, ok := ctx.Value(ctxKeyDisablePushLog).(*bool)
	return ok && v != nil && *v
}

// GrpcLoggingUnaryInterceptor 一元日志拦截器：续接链路、记录详情、按 code 分级、可选推送远程日志。
func GrpcLoggingUnaryInterceptor(schema string) grpc.UnaryServerInterceptor {
	opts := loadLoggerOptions(schema)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		peerAddr := grpcExtractClientIP(ctx)
		buildGrpcTrace(ctx, peerAddr)
		defer gaia.RemoveContextTrace()

		disabled := false
		ctx = context.WithValue(ctx, ctxKeyDisablePushLog, &disabled)

		start := time.Now()
		resp, err = handler(ctx, req)
		end := time.Now()

		writeGrpcLog(opts, ctx, info.FullMethod, "unary", peerAddr, req, resp, err, start, end)
		return resp, err
	}
}

// GrpcLoggingStreamInterceptor 流式日志拦截器（流式无单条 req/resp，payload 记空）。
func GrpcLoggingStreamInterceptor(schema string) grpc.StreamServerInterceptor {
	opts := loadLoggerOptions(schema)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		peerAddr := grpcExtractClientIP(ctx)
		buildGrpcTrace(ctx, peerAddr)
		defer gaia.RemoveContextTrace()

		disabled := false
		ctx = context.WithValue(ctx, ctxKeyDisablePushLog, &disabled)
		wrappedStream := &pushDisableStream{ServerStream: ss, ctx: ctx}

		start := time.Now()
		err := handler(srv, wrappedStream)
		end := time.Now()

		writeGrpcLog(opts, ctx, info.FullMethod, "stream", peerAddr, nil, nil, err, start, end)
		return err
	}
}

type pushDisableStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *pushDisableStream) Context() context.Context {
	return s.ctx
}
