// Package server 可观测性 & 探针中间件。
//
// 集中放置三类与 server.go 主流程解耦的"运维侧"能力：
//  1. RequestID 响应头回写（X-Request-Id / X-Trace-Id）—— 让客户端无需读 body
//     就能拿到 trace 上下文做问题定位；
//  2. HTTP 指标采集中间件（QPS / 时延直方图 / 在途请求 / 响应字节）——
//     直接复用 OTel Meter，配合 framework/metrics 一键暴露给 Prometheus；
//  3. K8s 探针：/livez（进程级）+ /readyz（依赖级）—— readyz 调用
//     framework.RunReadinessProbes 实时探活所有 Required 组件。
//
// 设计原则：
//   - 中间件失败不阻塞业务：metrics 未启用时 otel.Meter 返回 noop，零开销；
//   - 探针失败可降级：HTTP 指标实例化失败仅 warn，业务继续运行；
//   - 与 server.go 完全解耦：仅通过 (s *Server) 方法暴露给主流程。
//
// @author wanlizhan
// @created 2026-06-01
package server

import (
	"context"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework"
	"github.com/xxzhwl/gaia/framework/metrics"
)

// ============================================================================
// 1) RequestID 响应头回写中间件
// ============================================================================

// requestIdHeaderPlugin 把 defaultServerLogger 里设好的 RequestID 回写到响应头。
//
// **只写 X-Request-Id，不写 X-Trace-Id**——X-Trace-Id 统一由 tracePlugin 用
// OTel TraceID 写入。原因：gaia 自有的 ContextTrace.TraceId 与 OTel TraceID
// 是两套独立体系，如果两者都写 X-Trace-Id 会导致客户端拿到的 trace 与 Jaeger
// /Tempo 里的 span 对不上。统一以 OTel TraceID 为准后，tracePlugin 还会把它
// 反向同步给 gaia.ContextTrace.TraceId，让日志、响应头、链路平台三处一致。
//
// 客户端凭 X-Request-Id 定位本次请求的服务端日志，凭 X-Trace-Id（由
// tracePlugin 注入）跳到链路平台查完整调用链。
func (s *Server) requestIdHeaderPlugin() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		if rid := ctx.GetString(RequestIdKey); rid != "" {
			ctx.Response.Header.Set("X-Request-Id", rid)
		}
		ctx.Next(c)
	}
}

// ============================================================================
// 2) HTTP 指标采集中间件
// ============================================================================

// httpServerMetrics 持有本进程内 HTTP 服务侧的全部指标。
type httpServerMetrics struct {
	// RequestsTotal 总请求数，按 method/route/status 三维度。
	RequestsTotal metric.Int64Counter
	// RequestDuration 请求处理耗时直方图（毫秒），便于 Prometheus 计算 P50/P95/P99。
	RequestDuration metric.Float64Histogram
	// InFlight 在途请求数，UpDownCounter，反映瞬时并发。
	InFlight metric.Int64UpDownCounter
	// ResponseSize 响应字节数直方图，用于流量预估与异常大包检测。
	ResponseSize metric.Int64Histogram
}

// httpMetricAttr HTTP 指标的标签 key，集中常量化避免拼写漂移。
var httpMetricAttr = struct {
	Method attribute.Key
	Route  attribute.Key
	Status attribute.Key
}{
	Method: attribute.Key("http.method"),
	Route:  attribute.Key("http.route"),
	Status: attribute.Key("http.status_code"),
}

// initHTTPMetrics 创建 HTTP 指标实例。metrics 系统若未启用，
// otel.Meter 会返回 noop meter，所有 Add/Record 都是零开销空操作。
func initHTTPMetrics() *httpServerMetrics {
	meter := otel.Meter("github.com/xxzhwl/gaia/framework/server",
		metric.WithInstrumentationVersion("1.0.0"),
	)

	m := &httpServerMetrics{}
	var err error

	m.RequestsTotal, err = meter.Int64Counter("http.server.requests.total",
		metric.WithDescription("Total HTTP requests by method, route and status code"),
	)
	otel.Handle(err)

	m.RequestDuration, err = meter.Float64Histogram("http.server.request.duration",
		metric.WithDescription("HTTP request processing time"),
		metric.WithUnit("ms"),
	)
	otel.Handle(err)

	m.InFlight, err = meter.Int64UpDownCounter("http.server.in_flight",
		metric.WithDescription("Number of in-flight HTTP requests"),
	)
	otel.Handle(err)

	m.ResponseSize, err = meter.Int64Histogram("http.server.response.size",
		metric.WithDescription("HTTP response body size in bytes"),
		metric.WithUnit("By"),
	)
	otel.Handle(err)

	return m
}

// metricsPlugin HTTP 指标采集中间件。
//
// 每个请求记录：
//   - 入口 +1 in_flight，出口 -1（含 panic 路径，由 defer 保证）
//   - 出口写入 requests_total / request_duration / response_size
//
// route 标签优先取 FullPath（如 /api/users/:id，对应 Hertz 路由模板）；
// 取不到时回退到原始 path，避免高基数（user-id 这种）撑爆指标后端。
//
// 探针请求短路：K8s/Prometheus 高频轮询 /livez|/readyz|/metrics|/health 时
// 不进入指标统计——这些请求会污染 SLO（让 P99 看起来异常低、QPS 异常高），
// 并且其本身已是观测体系的一部分，没有再被指标采集器采集的必要。
func (s *Server) metricsPlugin(m *httpServerMetrics) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		if isProbeRequest(ctx) {
			ctx.Next(c)
			return
		}

		start := time.Now()
		method := string(ctx.Request.Method())

		// In-flight + 1（不带 route 标签，避免基数）
		m.InFlight.Add(c, 1, metric.WithAttributes(
			httpMetricAttr.Method.String(method),
		))

		defer func() {
			// 1) in_flight -1：放在最前确保 panic 路径也能正确收口
			m.InFlight.Add(c, -1, metric.WithAttributes(
				httpMetricAttr.Method.String(method),
			))

			// 2) 路由标签：FullPath 是路由模板（如 /api/users/:id），适合做 label。
			route := ctx.FullPath()
			if route == "" {
				// 未匹配路由（404）时 FullPath 为空，统一标 "unmatched" 防基数爆炸
				route = "unmatched"
			}
			status := ctx.Response.StatusCode()
			attrs := metric.WithAttributes(
				httpMetricAttr.Method.String(method),
				httpMetricAttr.Route.String(route),
				httpMetricAttr.Status.Int(status),
			)

			m.RequestsTotal.Add(c, 1, attrs)
			m.RequestDuration.Record(c,
				float64(time.Since(start).Microseconds())/1000.0, // 转 ms 保留小数
				attrs,
			)
			// 优先记录"压缩前 body 大小"——衡量业务负载量比衡量出网字节量
			// 更利于做容量规划。gzipPlugin 在压缩前会把原始长度写到 ctx 中。
			// 取不到（gzip 未启用 / 命中跳过条件）时回退到当前 body 长度。
			var size int64
			if rawAny, ok := ctx.Get(rawResponseSizeKey); ok {
				if raw, ok2 := rawAny.(int); ok2 {
					size = int64(raw)
				}
			}
			if size == 0 {
				size = int64(len(ctx.Response.Body()))
			}
			if size > 0 {
				m.ResponseSize.Record(c, size, attrs)
			}
		}()

		ctx.Next(c)
	}
}

// ============================================================================
// 3) K8s 探针：livez & readyz
// ============================================================================

// registerProbeRoutes 注册 K8s 风格的探针路由：
//
//	GET /livez   — 进程存活探针：只要 HTTP 服务在跑，永远 200。K8s 的 livenessProbe
//	               用它判断是否需要 kill -9。绝不依赖任何下游，否则会"自杀式重启"。
//	GET /readyz  — 就绪探针：Required 组件全部探活通过才返回 200，否则 503。
//	               K8s 的 readinessProbe 用它决定是否把 Pod 从 Service Endpoints 摘除。
//
// 同时保留原有的 /health（向后兼容）。/livez 和 /readyz 直接用 ctx.JSON
// 写出，不走 MakeHandler 的统一响应包装——理由：
//   - K8s kubelet 探针解析的是 HTTP 状态码 + 极简 body，统一响应外面再裹一层
//     {code,msg,data} 反而让监控脚本/livenessProbe 写起来更费劲；
//   - 探针请求量大、链路要求最低，绕开 MakeHandler 也少了反射 + JSON 二次封装的开销。
func (s *Server) registerProbeRoutes() {
	// /livez：常量返回，零依赖，零分配
	s.GET("/livez", func(c context.Context, ctx *app.RequestContext) {
		ctx.JSON(http.StatusOK, map[string]any{
			"status": "alive",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// /readyz：调用 framework.RunReadinessProbes 实时探活
	s.GET("/readyz", func(c context.Context, ctx *app.RequestContext) {
		// 单次 readyz 整体超时 5s，由配置 Server.Probe.ReadyTimeoutSec 调整
		timeoutSec := gaia.GetSafeConfInt64WithDefault(s.schema+".Probe.ReadyTimeoutSec", 5)
		if timeoutSec <= 0 {
			timeoutSec = 5
		}
		probeCtx, cancel := context.WithTimeout(c, time.Duration(timeoutSec)*time.Second)
		defer cancel()

		report := framework.RunReadinessProbes(probeCtx)
		statusCode := http.StatusOK
		if !report.Ready {
			statusCode = http.StatusServiceUnavailable
		}
		ctx.JSON(statusCode, report)
	})
}

// ============================================================================
// 4) /metrics 端点：把 framework/metrics 的 Prometheus handler 挂到主 server
// ============================================================================

// registerMetricsRoute 在主 HTTP 服务上挂 /metrics 端点。
//
// 默认情况下 framework/metrics 在独立端口（如 :9100）暴露 /metrics，
// 但有些部署形态（云原生 sidecar 抓取、限定单端口的 PaaS）需要把 /metrics
// 复用主端口，这里通过 <schema>.Metrics.ExposeOnMainPort=true 开关启用。
//
// 路径默认 /metrics，可通过 <schema>.Metrics.Path 自定义；不与已有路由冲突。
func (s *Server) registerMetricsRoute() {
	if !gaia.GetSafeConfBoolWithDefault(s.schema+".Metrics.ExposeOnMainPort", false) {
		return
	}
	if !metrics.IsInitialized() {
		gaia.Warn("已开启 ExposeOnMainPort 但 metrics 未初始化，跳过挂载 /metrics")
		return
	}
	if metrics.CurrentBackend() != metrics.BackendPrometheus {
		gaia.WarnF("Metrics.ExposeOnMainPort 仅在 prometheus backend 下生效，当前 backend=%s",
			metrics.CurrentBackend())
		return
	}

	path := gaia.GetSafeConfStringWithDefault(s.schema+".Metrics.Path", "/metrics")
	promHandler := metrics.Handler()
	s.GET(path, func(c context.Context, ctx *app.RequestContext) {
		// hertz → net/http adapter：用 ResponseWriter 把 promhttp.Handler 的输出原样转发。
		// 这里用最小封装而不是 hertz-contrib/adaptor，避免引入额外依赖。
		w := newPromResponseWriter(ctx)
		r, err := http.NewRequestWithContext(c,
			string(ctx.Request.Method()), string(ctx.Request.URI().FullURI()), nil)
		if err != nil {
			gaia.WarnF("构造 prom http.Request 失败: %v", err)
			ctx.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		// 透传必要的 Accept 头让 promhttp 选择 text/plain 或 protobuf 格式
		ctx.Request.Header.VisitAll(func(k, v []byte) {
			r.Header.Add(string(k), string(v))
		})
		promHandler.ServeHTTP(w, r)
	})
	gaia.InfoF("已在主端口暴露 Prometheus 指标: %s", path)
}

// promResponseWriter 是把 http.ResponseWriter 的写操作桥接到 hertz RequestContext 的最小适配器。
// 仅满足 promhttp.Handler 实际使用的接口子集；不实现 Hijacker/Flusher。
type promResponseWriter struct {
	ctx     *app.RequestContext
	headers http.Header
	written bool
}

func newPromResponseWriter(ctx *app.RequestContext) *promResponseWriter {
	return &promResponseWriter{ctx: ctx, headers: http.Header{}}
}

func (w *promResponseWriter) Header() http.Header { return w.headers }

func (w *promResponseWriter) WriteHeader(statusCode int) {
	if w.written {
		return
	}
	w.written = true
	for k, vs := range w.headers {
		for _, v := range vs {
			w.ctx.Response.Header.Add(k, v)
		}
	}
	w.ctx.SetStatusCode(statusCode)
}

func (w *promResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ctx.Write(b)
}
