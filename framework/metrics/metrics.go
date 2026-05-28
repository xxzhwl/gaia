// Package metrics 提供 OTel Metrics 的 SDK 层一站式接入。
//
// 设计目标（与 framework/tracer 完全对齐）：
//  1. 业务侧零代码：只要 framework.Init() 跑过、配置打开，全部 components/* 里
//     已埋的 otel.Meter() 指标自动开始上报；
//  2. 多后端：支持 prometheus（默认，本地拉取）/ otlp（push 到 Collector）/ disabled；
//  3. 配置驱动：所有参数通过 gaia.GetSafeConf*WithDefault 读取，无硬编码；
//  4. 失败降级：exporter / provider 创建失败时仅 warn，全局保持 noop，不影响业务进程。
//
// 配置示例（config.yml）：
//
//	Framework:
//	  Metrics:
//	    Enabled: true
//	    Backend: prometheus            # prometheus | otlp | disabled
//	    ServiceName: gaia-server       # 不填则取 gaia.GetSystemEnName()
//	    Prometheus:
//	      ListenAddr: ":9100"          # /metrics 暴露端口（空字符串则不起 HTTP 服务）
//	      Path: "/metrics"
//	    OTLP:
//	      Endpoint: "localhost:4317"
//	      Insecure: true
//	      PushIntervalSec: 10
//
// @author wanlizhan
// @created 2026/05/28
package metrics

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xxzhwl/gaia"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Backend 表示指标后端类型。
type Backend string

const (
	// BackendPrometheus 暴露 /metrics 给 Prometheus 拉取（默认）。
	BackendPrometheus Backend = "prometheus"
	// BackendOTLP 通过 OTLP gRPC 推送到 OpenTelemetry Collector。
	BackendOTLP Backend = "otlp"
	// BackendDisabled 完全不上报（保持 noop，等价于不调用 Setup）。
	BackendDisabled Backend = "disabled"
)

// Config 指标系统运行时配置。
type Config struct {
	Enabled     bool
	Backend     Backend
	ServiceName string

	// Prometheus
	PromListenAddr string // ":9100"，为空则不起 HTTP 服务（仅注册 exporter，由业务自行 mount）
	PromPath       string // "/metrics"

	// OTLP
	OTLPEndpoint     string
	OTLPInsecure     bool
	OTLPPushInterval time.Duration
}

var (
	mu             sync.RWMutex
	isInitialized  bool
	shutdownFunc   func(context.Context) error
	currentBackend Backend
	promServer     *http.Server // 仅 prometheus backend 起 HTTP 时使用
)

// Setup 初始化指标系统并注册到全局 OTel MeterProvider。
//
//	serviceName: 服务名（用于 resource.ServiceName 标签），为空时回退到 gaia.GetSystemEnName()。
//
// 配置全部从 gaia 配置中心 + 环境变量加载。环境变量优先级高于配置中心，便于多副本场景。
// 支持的环境变量见 [LoadConfig]。
//
// 返回的 shutdown 用于优雅关闭（flush 残留指标 + 关闭 HTTP 服务），可与 tracer.ShutdownTracer 一并调用。
// 重复调用是幂等的：第二次起直接返回首次的 shutdown 句柄。
func Setup(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	cfg := LoadConfig(serviceName)
	return SetupWithConfig(ctx, cfg)
}

// SetupWithConfig 使用显式 Config 初始化指标系统，绕过配置中心。
//
// 适用场景：业务方有自己的配置加载逻辑、或多副本下需要根据运行时参数动态决定端口。
// 重复调用幂等。
func SetupWithConfig(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	mu.Lock()
	defer mu.Unlock()

	if isInitialized {
		return shutdownFunc, nil
	}

	// 配置关闭 / Backend=disabled：保持 noop，不报错。
	if !cfg.Enabled || cfg.Backend == BackendDisabled {
		gaia.InfoF("指标系统未启用（Enabled=%v, Backend=%s），保持 NoopMeterProvider", cfg.Enabled, cfg.Backend)
		shutdownFunc = func(context.Context) error { return nil }
		isInitialized = true
		currentBackend = BackendDisabled
		return shutdownFunc, nil
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		gaia.WarnF("metrics 构建 resource 失败，将使用最小 resource: %v", err)
		res = resource.Default()
	}

	mp, mpShutdown, backendShutdown, err := buildMeterProvider(ctx, cfg, res)
	if err != nil {
		gaia.WarnF("初始化指标系统失败，将保持 NoopMeterProvider: %v", err)
		shutdownFunc = func(context.Context) error { return nil }
		isInitialized = true
		currentBackend = BackendDisabled
		return shutdownFunc, nil
	}

	otel.SetMeterProvider(mp)
	currentBackend = cfg.Backend
	isInitialized = true

	shutdownFunc = func(shutdownCtx context.Context) error {
		var errs []error
		if backendShutdown != nil {
			if e := backendShutdown(shutdownCtx); e != nil {
				errs = append(errs, fmt.Errorf("backend shutdown: %w", e))
			}
		}
		if mpShutdown != nil {
			if e := mpShutdown(shutdownCtx); e != nil {
				errs = append(errs, fmt.Errorf("meter provider shutdown: %w", e))
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		gaia.Info("指标系统已关闭")
		return nil
	}

	gaia.InfoF("指标系统初始化成功: Service=%s, Backend=%s", cfg.ServiceName, cfg.Backend)
	return shutdownFunc, nil
}

// Shutdown 关闭指标系统。重复调用安全。
func Shutdown(ctx context.Context) error {
	mu.Lock()
	defer mu.Unlock()

	if !isInitialized || shutdownFunc == nil {
		return nil
	}
	err := shutdownFunc(ctx)
	isInitialized = false
	shutdownFunc = nil
	currentBackend = ""
	promServer = nil
	return err
}

// IsInitialized 指标系统是否已初始化。
func IsInitialized() bool {
	mu.RLock()
	defer mu.RUnlock()
	return isInitialized
}

// CurrentBackend 当前生效的后端类型；未初始化时返回空串。
func CurrentBackend() Backend {
	mu.RLock()
	defer mu.RUnlock()
	return currentBackend
}

// LoadConfig 从 gaia 配置中心 + 环境变量加载 metrics 运行时配置。
// 环境变量优先级高于配置中心，便于多副本场景临时覆盖（如端口）。
//
// 支持的环境变量：
//
//	GAIA_METRICS_ENABLED         覆盖 Framework.Metrics.Enabled        (true/false/1/0)
//	GAIA_METRICS_BACKEND         覆盖 Framework.Metrics.Backend        (prometheus/otlp/disabled)
//	GAIA_METRICS_SERVICE_NAME    覆盖 Framework.Metrics.ServiceName
//	GAIA_METRICS_LISTEN_ADDR     覆盖 Framework.Metrics.Prometheus.ListenAddr  (如 :9101)
//	GAIA_METRICS_PATH            覆盖 Framework.Metrics.Prometheus.Path
//	GAIA_METRICS_OTLP_ENDPOINT   覆盖 Framework.Metrics.OTLP.Endpoint
func LoadConfig(serviceName string) Config {
	return loadConfig(serviceName)
}

// loadConfig 从 gaia 配置中心加载配置（带默认值与环境变量覆盖）。
func loadConfig(serviceName string) Config {
	if strings.TrimSpace(serviceName) == "" {
		serviceName = gaia.GetSystemEnName()
	}
	if strings.TrimSpace(serviceName) == "" {
		serviceName = "gaia-app"
	}

	enabled := gaia.GetSafeConfBoolWithDefault("Framework.Metrics.Enabled", false)
	if v := os.Getenv("GAIA_METRICS_ENABLED"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			enabled = true
		case "0", "false", "no", "off":
			enabled = false
		}
	}

	backendStr := strings.ToLower(strings.TrimSpace(
		gaia.GetSafeConfStringWithDefault("Framework.Metrics.Backend", string(BackendPrometheus)),
	))
	if v := os.Getenv("GAIA_METRICS_BACKEND"); v != "" {
		backendStr = strings.ToLower(strings.TrimSpace(v))
	}
	backend := Backend(backendStr)
	switch backend {
	case BackendPrometheus, BackendOTLP, BackendDisabled:
	default:
		gaia.WarnF("未知的 Framework.Metrics.Backend=%q，回退为 prometheus", backendStr)
		backend = BackendPrometheus
	}

	finalServiceName := gaia.GetSafeConfStringWithDefault("Framework.Metrics.ServiceName", serviceName)
	if v := os.Getenv("GAIA_METRICS_SERVICE_NAME"); v != "" {
		finalServiceName = v
	}

	listenAddr := gaia.GetSafeConfStringWithDefault("Framework.Metrics.Prometheus.ListenAddr", ":9100")
	if v := os.Getenv("GAIA_METRICS_LISTEN_ADDR"); v != "" {
		listenAddr = v
	}

	promPath := gaia.GetSafeConfStringWithDefault("Framework.Metrics.Prometheus.Path", "/metrics")
	if v := os.Getenv("GAIA_METRICS_PATH"); v != "" {
		promPath = v
	}

	otlpEndpoint := gaia.GetSafeConfStringWithDefault("Framework.Metrics.OTLP.Endpoint", "localhost:4317")
	if v := os.Getenv("GAIA_METRICS_OTLP_ENDPOINT"); v != "" {
		otlpEndpoint = v
	}

	return Config{
		Enabled:        enabled,
		Backend:        backend,
		ServiceName:    finalServiceName,
		PromListenAddr: listenAddr,
		PromPath:       promPath,
		OTLPEndpoint:   otlpEndpoint,
		OTLPInsecure:   gaia.GetSafeConfBoolWithDefault("Framework.Metrics.OTLP.Insecure", true),
		OTLPPushInterval: time.Second * time.Duration(gaia.GetSafeConfInt64WithDefault(
			"Framework.Metrics.OTLP.PushIntervalSec", 10)),
	}
}

// buildResource 构造 OTel Resource（service.name / env / version）。
func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion("1.0.0"),
			semconv.DeploymentEnvironment(gaia.GetSafeConfStringWithDefault("Environment", "development")),
			attribute.String("backend", string(cfg.Backend)),
		),
	)
}

// buildMeterProvider 根据 backend 构造对应的 MeterProvider。
//
//	返回值：
//	  mp              - 全局 MeterProvider
//	  mpShutdown      - mp 的 Shutdown 方法（用于 flush）
//	  backendShutdown - backend 自身的 cleanup（如 Prometheus HTTP server 关闭）
func buildMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (
	mp *sdkmetric.MeterProvider,
	mpShutdown func(context.Context) error,
	backendShutdown func(context.Context) error,
	err error,
) {
	switch cfg.Backend {
	case BackendPrometheus:
		return buildPrometheusProvider(cfg, res)
	case BackendOTLP:
		return buildOTLPProvider(ctx, cfg, res)
	default:
		return nil, nil, nil, fmt.Errorf("unsupported backend: %s", cfg.Backend)
	}
}

// buildPrometheusProvider 构造 Prometheus pull 模式的 MeterProvider，并按需启动 /metrics HTTP 服务。
func buildPrometheusProvider(cfg Config, res *resource.Resource) (
	*sdkmetric.MeterProvider, func(context.Context) error, func(context.Context) error, error,
) {
	exporter, err := otelprom.New()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)

	var backendShutdown func(context.Context) error
	if addr := strings.TrimSpace(cfg.PromListenAddr); addr != "" {
		path := cfg.PromPath
		if path == "" {
			path = "/metrics"
		}
		mux := http.NewServeMux()
		mux.Handle(path, promhttp.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		// 提前监听以便在 Setup 返回前发现端口冲突。
		ln, lerr := net.Listen("tcp", addr)
		if lerr != nil {
			gaia.WarnF("metrics: 监听 %s 失败，将仅注册 exporter（业务可自行 mount %s）: %v", addr, path, lerr)
		} else {
			srv := &http.Server{
				Handler:           mux,
				ReadHeaderTimeout: 5 * time.Second,
			}
			promServer = srv
			go func() {
				if e := srv.Serve(ln); e != nil && !errors.Is(e, http.ErrServerClosed) {
					gaia.WarnF("metrics: /metrics HTTP 服务异常退出: %v", e)
				}
			}()
			gaia.InfoF("metrics: Prometheus /metrics 已暴露在 http://%s%s", addr, path)
			backendShutdown = func(ctx context.Context) error {
				return srv.Shutdown(ctx)
			}
		}
	}

	return mp, mp.Shutdown, backendShutdown, nil
}

// Handler 返回一个标准的 promhttp.Handler，方便上游把 /metrics 挂到自己的 mux/router 上。
//
// 仅在 Backend=prometheus 时有意义；其他 backend 下 Handler 仍可调用，但内容为空。
func Handler() http.Handler {
	return promhttp.Handler()
}
