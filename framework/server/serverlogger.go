// Package server 包注释
// @author wanlizhan
// @created 2025-04-09
package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

const detailInfo = `
=========================================
Request: 
	Time:%s
	Url:%s
	Method:%s
	Args:%s
- - - - - - - - - - - - - - - - - - - - -
Response: 
	Time:%s
	Status:%3d
Duration:%13v
=========================================
`

const simpleInfo = `[%v ~ %v] | %3d | %13v | %s | %s`

const (
	FullPathKey      = "CtxFullPath"
	RequestBody      = "CtxRequestBody"
	RequestIdKey     = "CtxRequestId"
	TraceIdKey       = "CtxTraceId"
	AuthorizationKey = "CtxAuthorization"
	DisableLogKey    = "DisableLog"
)

// serverInLogger 包级共享的日志实例，避免每次请求创建新 logger
var serverInLogger *logImpl.DefaultLogger

func init() {
	serverInLogger = logImpl.NewDefaultLogger().SetTitle("ServerIn")
}

// loggerConfig 缓存的日志配置（每个 schema 一份），避免每次请求多次 GetSafeConfXxx 的字符串拼接 + map lookup。
type loggerConfig struct {
	detailMode      bool
	printConsole    bool
	enablePushLog   bool
	logBody         bool
	maxBodyLogBytes int64
}

var (
	loggerConfigCache sync.Map // schema -> *loggerConfig（atomic-pointer 模式）
	loggerCacheTTL    = 30 * time.Second
)

type cachedLoggerConfig struct {
	cfg      atomic.Pointer[loggerConfig]
	loadedAt atomic.Int64 // unix nano
	mu       sync.Mutex
}

// getLoggerConfig 获取缓存的日志配置，30 秒刷新一次。
// 这样高 QPS 接口不再为每个请求做 5 次配置读取。
func getLoggerConfig(schema string) *loggerConfig {
	v, _ := loggerConfigCache.LoadOrStore(schema, &cachedLoggerConfig{})
	c := v.(*cachedLoggerConfig)

	now := time.Now().UnixNano()
	if cur := c.cfg.Load(); cur != nil && now-c.loadedAt.Load() < int64(loggerCacheTTL) {
		return cur
	}

	// 慢路径：刷新（单 flight）
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur := c.cfg.Load(); cur != nil && time.Now().UnixNano()-c.loadedAt.Load() < int64(loggerCacheTTL) {
		return cur
	}
	maxBytes := gaia.GetSafeConfInt64WithDefault(schema+".Logger.MaxBodyLogBytes", 4096)
	if maxBytes <= 0 {
		maxBytes = 4096
	}
	cfg := &loggerConfig{
		detailMode:      gaia.GetSafeConfBool(schema + ".Logger.DetailMode"),
		printConsole:    gaia.GetSafeConfBool(schema + ".Logger.PrintConsole"),
		enablePushLog:   gaia.GetSafeConfBool(schema + ".Logger.EnablePushLog"),
		logBody:         gaia.GetSafeConfBool(schema + ".Logger.LogBody"),
		maxBodyLogBytes: maxBytes,
	}
	c.cfg.Store(cfg)
	c.loadedAt.Store(time.Now().UnixNano())
	return cfg
}

func (s *Server) defaultServerLogger() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		// 探针请求豁免：K8s/Prometheus 每 10s 一次的 livez/readyz/metrics 轮询
		// 会狮子头打出大量无调试价值的 INFO 日志、且多余的 BuildHttpContextTrace
		// 也会拍费 trace ID 生成。这里直接跳过。
		if isProbeRequest(ctx) {
			ctx.Next(c)
			return
		}

		followFrom := ctx.Request.Header.Get(gaia.FollowFromKey)
		traceId := ctx.Request.Header.Get(gaia.TraceIdKey)
		//构建请求日志id
		gaia.TraceF("构建请求trace")
		gaia.BuildHttpContextTrace(traceId, string(ctx.Request.Host()), followFrom)
		ctx.Set(RequestIdKey, gaia.GetContextTrace().Id)
		ctx.Set(TraceIdKey, gaia.GetContextTrace().TraceId)
		//请求开始时间
		startTime := time.Now()
		// 优先注册清理 trace 的 defer，确保不论后续哪条路径 return 都会被调用
		defer gaia.RemoveContextTrace()
		defer func() {
			//请求结束时间
			endTime := time.Now()
			//请求方法
			method := string(ctx.Request.Method())

			if method == http.MethodOptions {
				return
			}

			// 一次性读取本次请求需要的所有日志配置
			cfg := getLoggerConfig(s.schema)

			//请求路径
			path := ctx.URI().String()
			//请求参数
			contentType := string(ctx.Request.Header.ContentType())
			isMultipart := strings.Contains(contentType, "multipart/form-data")

			body := ""
			if isMultipart {
				body = "[文件上传] 内容已隐藏"
				ctx.Set(BanLoggerKey, true)
				ctx.Set(BanLoggerContent, "文件上传请求")
			} else if ctx.GetBool(BanLoggerKey) {
				body = ctx.GetString(BanLoggerContent)
			} else {
				body = sanitizeServerBodyWithCfg(cfg, ctx.Request.Body())
			}

			//返回状态码
			code := ctx.Response.StatusCode()
			//耗时
			dura := endTime.Sub(startTime)
			logger := serverInLogger

			// 根据 statusCode 动态选择 level，4xx Warn / 5xx Error，避免一律记录为 Info
			level := gaia.LogInfoLevel
			switch {
			case code >= 500:
				level = gaia.LogErrorLevel
			case code >= 400:
				level = gaia.LogWarnLevel
			}

			content := ""

			if cfg.detailMode {
				content = fmt.Sprintf(detailInfo, startTime.Format(gaia.DateTimeMillsFormat), path, method, body,
					endTime.Format(gaia.DateTimeMillsFormat), code, dura)
			} else {
				content = fmt.Sprintf(simpleInfo,
					startTime.Format(gaia.DateTimeMillsFormat),
					endTime.Format(gaia.DateTimeMillsFormat),
					code,
					dura,
					method,
					path,
				)
			}

			if cfg.printConsole {
				logger.InLog(level, content)
			}
			if cfg.enablePushLog && !ctx.GetBool(DisabledPushLoggerKey) {
				reqHeader, respHeader := http.Header{}, http.Header{}
				ctx.Request.Header.VisitAll(func(key, value []byte) {
					reqHeader[string(key)] = []string{string(value)}
				})
				ctx.Response.Header.VisitAll(func(key, value []byte) {
					respHeader[string(key)] = []string{string(value)}
				})

				logBody := logImpl.HttpLogModel{
					Url:            path,
					HttpMethod:     method,
					ReqHeader:      logImpl.SanitizeHttpHeaders(reqHeader),
					ReqBody:        body,
					RespHeader:     logImpl.SanitizeHttpHeaders(respHeader),
					RespBody:       sanitizeServerBodyWithCfg(cfg, ctx.Response.BodyBytes()),
					StartTime:      startTime.Format(gaia.DateTimeMillsFormat),
					EndTime:        endTime.Format(gaia.DateTimeMillsFormat),
					StartTimeStamp: startTime.UnixMilli(),
					EndTimeStamp:   endTime.UnixMilli(),
					HttpStatusCode: ctx.Response.StatusCode(),
					Duration:       float64(endTime.Sub(startTime).Milliseconds()),
				}
				logger.InLogBody(level, content, logImpl.NewHTTPAccessLog(logImpl.AccessDirectionInbound, logBody))
			}
		}()
		ctx.Next(c)
	}
}

// sanitizeServerBody 兼容老调用点；高频路径请使用 sanitizeServerBodyWithCfg
func sanitizeServerBody(schema string, body []byte) string {
	return sanitizeServerBodyWithCfg(getLoggerConfig(schema), body)
}

func sanitizeServerBodyWithCfg(cfg *loggerConfig, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if !cfg.logBody {
		return fmt.Sprintf("[REDACTED len=%d]", len(body))
	}
	max := cfg.maxBodyLogBytes
	if int64(len(body)) <= max {
		return string(body)
	}
	return fmt.Sprintf("%s...[truncated %d bytes]", string(body[:int(max)]), int64(len(body))-max)
}
