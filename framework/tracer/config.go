// Package tracer 追踪系统配置加载层。
//
// 设计目标（与 framework/metrics 完全对齐）：
//  1. 多 exporter 后端：otlphttp（默认） / otlpgrpc / stdout / none，
//     可对接 Jaeger / Grafana Tempo / SigNoz / OTel Collector 等任意系统；
//  2. 配置驱动：所有参数通过 gaia.GetSafeConf*WithDefault 读取，无硬编码；
//  3. 环境变量可覆盖，便于多副本/容器场景；
//  4. 失败降级：exporter 创建失败时仅 warn，全局保持 NoopTracer，不影响业务进程。
//
// 配置示例（config.yml）：
//
//	Framework:
//	  Tracer:
//	    Enabled: true
//	    Exporter: otlphttp           # otlphttp | otlpgrpc | stdout | none
//	    ServiceName: gaia-server     # 不填则取 gaia.GetSystemEnName()
//	    Endpoint: "localhost:4318"   # otlphttp 默认 4318，otlpgrpc 默认 4317
//	    Insecure: true
//	    URLPath: "/v1/traces"        # 仅 otlphttp 生效，留空走默认
//	    Headers:                     # 可选鉴权头（如 SigNoz Cloud 的 signoz-access-token）
//	      authorization: "Bearer xxx"
//	    SampleRate: 1.0
//	    BatchTimeoutSec: 1
//	    MaxQueueSize: 1024
//	    MaxExportBatchSize: 512
//
// @author wanlizhan
// @created 2026-06-01
package tracer

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
)

// Exporter 表示 trace 导出后端类型。
type Exporter string

const (
	// ExporterOTLPHTTP 通过 OTLP/HTTP 推送（默认，端口 4318，兼容 Jaeger/Tempo/SigNoz/Collector）。
	ExporterOTLPHTTP Exporter = "otlphttp"
	// ExporterOTLPGRPC 通过 OTLP/gRPC 推送（端口 4317，性能更高）。
	ExporterOTLPGRPC Exporter = "otlpgrpc"
	// ExporterStdout 输出到标准输出，仅用于本地调试。
	ExporterStdout Exporter = "stdout"
	// ExporterNone 完全不上报（NoopTracer），等价于关闭追踪。
	ExporterNone Exporter = "none"
)

// 旧配置项（向后兼容）：
// （已移除，统一使用 Framework.Tracer.* 新配置族）

// Config 追踪系统运行时配置。
type Config struct {
	Enabled     bool
	Exporter    Exporter
	ServiceName string

	// 通用 OTLP
	Endpoint string            // host:port
	Insecure bool              // true 时使用 HTTP 而非 HTTPS
	URLPath  string            // 仅 otlphttp 生效（默认 /v1/traces）
	Headers  map[string]string // 可选鉴权头

	// 采样
	SampleRate float64 // [0,1]，1=全采样

	// 批处理
	BatchTimeout       time.Duration
	MaxQueueSize       int
	MaxExportBatchSize int
}

// LoadConfig 从 gaia 配置中心 + 环境变量加载 tracer 配置。
//
// 支持的环境变量（优先级高于配置中心）：
//
//	GAIA_TRACER_ENABLED          覆盖 Framework.Tracer.Enabled
//	GAIA_TRACER_EXPORTER         覆盖 Framework.Tracer.Exporter
//	GAIA_TRACER_SERVICE_NAME     覆盖 Framework.Tracer.ServiceName
//	GAIA_TRACER_ENDPOINT         覆盖 Framework.Tracer.Endpoint
//	GAIA_TRACER_SAMPLE_RATE      覆盖 Framework.Tracer.SampleRate
func LoadConfig(serviceName string) Config {
	return loadConfig(serviceName)
}

// loadConfig 内部实现，集中处理默认值、旧配置兼容、环境变量覆盖。
func loadConfig(serviceName string) Config {
	// ---- ServiceName ----
	if strings.TrimSpace(serviceName) == "" {
		serviceName = gaia.GetSystemEnName()
	}
	if strings.TrimSpace(serviceName) == "" {
		serviceName = "gaia-app"
	}
	finalServiceName := gaia.GetSafeConfStringWithDefault("Framework.Tracer.ServiceName", serviceName)
	if v := os.Getenv("GAIA_TRACER_SERVICE_NAME"); v != "" {
		finalServiceName = v
	}

	// ---- Enabled ----
	enabled := gaia.GetSafeConfBoolWithDefault("Framework.Tracer.Enabled", true)
	if v := os.Getenv("GAIA_TRACER_ENABLED"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			enabled = true
		case "0", "false", "no", "off":
			enabled = false
		}
	}

	// ---- Exporter 类型 ----
	exporterStr := strings.ToLower(strings.TrimSpace(
		gaia.GetSafeConfStringWithDefault("Framework.Tracer.Exporter", string(ExporterOTLPHTTP)),
	))
	if v := os.Getenv("GAIA_TRACER_EXPORTER"); v != "" {
		exporterStr = strings.ToLower(strings.TrimSpace(v))
	}
	exp := Exporter(exporterStr)
	switch exp {
	case ExporterOTLPHTTP, ExporterOTLPGRPC, ExporterStdout, ExporterNone:
	default:
		gaia.WarnF("未知的 Framework.Tracer.Exporter=%q，回退为 otlphttp", exporterStr)
		exp = ExporterOTLPHTTP
	}

	// ---- Endpoint ----
	defaultEndpoint := "localhost:4318"
	if exp == ExporterOTLPGRPC {
		defaultEndpoint = "localhost:4317"
	}
	endpoint := gaia.GetSafeConfStringWithDefault("Framework.Tracer.Endpoint", defaultEndpoint)
	if v := os.Getenv("GAIA_TRACER_ENDPOINT"); v != "" {
		endpoint = v
	}

	// ---- 采样率 ----
	sampleRate := gaia.GetSafeConfFloat64WithDefault("Framework.Tracer.SampleRate", 1.0)
	if v := os.Getenv("GAIA_TRACER_SAMPLE_RATE"); v != "" {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			sampleRate = f
		}
	}

	// ---- 批处理 ----
	batchTimeout := time.Second * time.Duration(
		gaia.GetSafeConfInt64WithDefault("Framework.Tracer.BatchTimeoutSec", 1))
	maxQueueSize := int(gaia.GetSafeConfInt64WithDefault("Framework.Tracer.MaxQueueSize", 1024))
	maxBatch := int(gaia.GetSafeConfInt64WithDefault("Framework.Tracer.MaxExportBatchSize", 512))

	// ---- 可选 headers / urlpath ----
	urlPath := gaia.GetSafeConfStringWithDefault("Framework.Tracer.URLPath", "")
	headers := gaia.GetSafeConfMapT[string]("Framework.Tracer.Headers")

	return Config{
		Enabled:            enabled,
		Exporter:           exp,
		ServiceName:        finalServiceName,
		Endpoint:           endpoint,
		Insecure:           gaia.GetSafeConfBoolWithDefault("Framework.Tracer.Insecure", true),
		URLPath:            urlPath,
		Headers:            headers,
		SampleRate:         sampleRate,
		BatchTimeout:       batchTimeout,
		MaxQueueSize:       maxQueueSize,
		MaxExportBatchSize: maxBatch,
	}
}


