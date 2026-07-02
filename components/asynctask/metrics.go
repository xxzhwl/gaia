// Package asynctask 注释
// @author wanlizhan
// @created 2026/05/27
package asynctask

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// MetricLabel 为 asynctask 指标提供统一的标签键，避免散落字符串。
// 注意：刻意不暴露 task_name 这种业务侧高基数字段作为指标标签，避免 Prometheus 时序爆炸。
// 想按具体任务定位，请走 admin API / DB 查询，而不是通过 metrics 标签过滤。
var MetricLabel = struct {
	Theme       attribute.Key
	ServiceName attribute.Key
	MethodName  attribute.Key
	Status      attribute.Key
	Reason      attribute.Key
	Phase       attribute.Key
}{
	Theme:       attribute.Key("theme"),
	ServiceName: attribute.Key("service_name"),
	MethodName:  attribute.Key("method_name"),
	Status:      attribute.Key("status"),
	Reason:      attribute.Key("reason"),
	Phase:       attribute.Key("phase"),
}

// AsyncTaskMetrics 持有 asynctask 模块的全部 OTel 指标。
// 通过 sync.Once 全局初始化一次；指标在 scheduler / executor / worker 等关键路径录制。
// 若未配置 OTel SDK 提供者，所有 Record/Add 调用将安全地被丢弃（noop），不引入额外依赖。
type AsyncTaskMetrics struct {
	// 任务执行
	TaskExecTotal    metric.Int64Counter     // 任务执行总次数（成功+失败+重试）
	TaskExecDuration metric.Float64Histogram // 任务执行耗时（毫秒）
	TaskWaitDuration metric.Float64Histogram // 任务从创建到首次执行的等待耗时（毫秒）
	TaskRetryTotal   metric.Int64Counter     // 任务进入重试状态的次数
	TaskPanicTotal   metric.Int64Counter     // 任务执行 panic 次数

	// 调度器
	ScanTotal       metric.Int64Counter // 扫描数据库次数
	ScanDuration    metric.Float64Histogram
	QueueDepth      metric.Int64ObservableGauge // 队列当前深度（异步采样）
	QueueCapacity   metric.Int64ObservableGauge // 队列容量
	QueueDropTotal  metric.Int64Counter         // 因队列满而丢弃入队的次数
	QueueWaitMillis metric.Float64Histogram     // 任务在内存队列里等待被拉取的毫秒数

	// Worker
	WorkerScaleUp   metric.Int64Counter
	WorkerScaleDown metric.Int64Counter
	WorkerTotal     metric.Int64ObservableGauge
	WorkerRunning   metric.Int64ObservableGauge

	// 心跳
	HeartbeatDeadDetected metric.Int64Counter // 检测到心跳失活并被回收的任务数

	// DB
	DBErrorTotal metric.Int64Counter // DB 操作错误次数

	// 告警
	AlarmFired      metric.Int64Counter // 实际触发告警的次数
	AlarmSuppressed metric.Int64Counter // 因去重/限流被压制的告警次数

	// 异步采样回调注册器
	registerObserveOnce sync.Once
	observeProviders    []func(observer metric.Observer)
	observeProvidersMu  sync.Mutex

	// localCounters 进程内累计 counter（独立于 OTel exporter）。
	localCounters localCounters
}

var (
	asyncMetricsOnce   sync.Once
	asyncMetricsGlobal *AsyncTaskMetrics
)

// GetMetrics 获取（懒初始化）asynctask 全局指标对象。
// 该函数线程安全，多次调用返回同一实例。
func GetMetrics() *AsyncTaskMetrics {
	asyncMetricsOnce.Do(func() {
		meter := otel.Meter("github.com/xxzhwl/gaia/components/asynctask",
			metric.WithInstrumentationVersion("1.0.0"),
		)

		m := &AsyncTaskMetrics{}
		var err error

		m.TaskExecTotal, err = meter.Int64Counter("asynctask.exec.total",
			metric.WithDescription("Async task execution attempts by status"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TaskExecDuration, err = meter.Float64Histogram("asynctask.exec.duration",
			metric.WithDescription("Async task execution duration in milliseconds"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TaskWaitDuration, err = meter.Float64Histogram("asynctask.wait.duration",
			metric.WithDescription("Async task wait duration from create to first run in milliseconds"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TaskRetryTotal, err = meter.Int64Counter("asynctask.retry.total",
			metric.WithDescription("Async task retry events"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.TaskPanicTotal, err = meter.Int64Counter("asynctask.panic.total",
			metric.WithDescription("Async task panic events"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.ScanTotal, err = meter.Int64Counter("asynctask.scan.total",
			metric.WithDescription("Scheduler DB scan attempts"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.ScanDuration, err = meter.Float64Histogram("asynctask.scan.duration",
			metric.WithDescription("Scheduler DB scan duration in milliseconds"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.QueueDropTotal, err = meter.Int64Counter("asynctask.queue.drop.total",
			metric.WithDescription("Tasks dropped because in-memory queue is full"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.QueueWaitMillis, err = meter.Float64Histogram("asynctask.queue.wait",
			metric.WithDescription("Time a task waits in the in-memory queue before being pulled"),
			metric.WithUnit("ms"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.WorkerScaleUp, err = meter.Int64Counter("asynctask.worker.scale_up.total",
			metric.WithDescription("Worker scale up events"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.WorkerScaleDown, err = meter.Int64Counter("asynctask.worker.scale_down.total",
			metric.WithDescription("Worker scale down events"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.HeartbeatDeadDetected, err = meter.Int64Counter("asynctask.heartbeat.dead.total",
			metric.WithDescription("Tasks recovered because heartbeat is dead"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.DBErrorTotal, err = meter.Int64Counter("asynctask.db.error.total",
			metric.WithDescription("Database operation errors"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.AlarmFired, err = meter.Int64Counter("asynctask.alarm.fired.total",
			metric.WithDescription("Alarms actually fired"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.AlarmSuppressed, err = meter.Int64Counter("asynctask.alarm.suppressed.total",
			metric.WithDescription("Alarms suppressed by throttle/dedupe"),
		)
		if err != nil {
			otel.Handle(err)
		}

		// Observable gauges 通过 callback 异步采样所有 scheduler 的状态
		m.QueueDepth, err = meter.Int64ObservableGauge("asynctask.queue.depth",
			metric.WithDescription("Current depth of the in-memory task queue"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.QueueCapacity, err = meter.Int64ObservableGauge("asynctask.queue.capacity",
			metric.WithDescription("Capacity of the in-memory task queue"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.WorkerTotal, err = meter.Int64ObservableGauge("asynctask.worker.total",
			metric.WithDescription("Total worker goroutines"),
		)
		if err != nil {
			otel.Handle(err)
		}

		m.WorkerRunning, err = meter.Int64ObservableGauge("asynctask.worker.running",
			metric.WithDescription("Running worker goroutines"),
		)
		if err != nil {
			otel.Handle(err)
		}

		_, err = meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
			m.observeProvidersMu.Lock()
			providers := make([]func(observer metric.Observer), len(m.observeProviders))
			copy(providers, m.observeProviders)
			m.observeProvidersMu.Unlock()
			for _, p := range providers {
				p(observer)
			}
			return nil
		}, m.QueueDepth, m.QueueCapacity, m.WorkerTotal, m.WorkerRunning)
		if err != nil {
			otel.Handle(err)
		}

		asyncMetricsGlobal = m
	})
	return asyncMetricsGlobal
}

// registerObserveProvider 注册一个 observable callback，scheduler 启动时调用，停止时调用 unregister。
func (m *AsyncTaskMetrics) registerObserveProvider(p func(observer metric.Observer)) (unregister func()) {
	m.observeProvidersMu.Lock()
	defer m.observeProvidersMu.Unlock()
	m.observeProviders = append(m.observeProviders, p)
	idx := len(m.observeProviders) - 1
	return func() {
		m.observeProvidersMu.Lock()
		defer m.observeProvidersMu.Unlock()
		if idx >= 0 && idx < len(m.observeProviders) {
			m.observeProviders[idx] = func(observer metric.Observer) {}
		}
	}
}

// recordExecDuration 记录任务执行耗时直方图。
// 业务侧高基数字段（如 task_name）刻意不进入标签，避免 Prometheus 时序爆炸。
func recordExecDuration(ctx context.Context, theme, status string, ms int64) {
	m := GetMetrics()
	attrs := []attribute.KeyValue{
		MetricLabel.Theme.String(theme),
		MetricLabel.Status.String(status),
	}
	m.TaskExecTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.TaskExecDuration.Record(ctx, float64(ms), metric.WithAttributes(attrs...))
}

// recordWaitDuration 记录任务等待时延（首次执行）。
func recordWaitDuration(ctx context.Context, theme string, ms int64) {
	if ms <= 0 {
		return
	}
	m := GetMetrics()
	m.TaskWaitDuration.Record(ctx, float64(ms), metric.WithAttributes(
		MetricLabel.Theme.String(theme),
	))
}

// recordRetry 记录重试。
func recordRetry(ctx context.Context, theme string) {
	GetMetrics().TaskRetryTotal.Add(ctx, 1, metric.WithAttributes(
		MetricLabel.Theme.String(theme),
	))
	GetMetrics().localCounters.retry.Add(1)
}

// recordPanic 记录 panic。
func recordPanic(ctx context.Context, theme, phase string) {
	GetMetrics().TaskPanicTotal.Add(ctx, 1, metric.WithAttributes(
		MetricLabel.Theme.String(theme),
		MetricLabel.Phase.String(phase),
	))
	GetMetrics().localCounters.panic.Add(1)
}

// recordDBError 记录 DB 错误。
func recordDBError(ctx context.Context, theme, op string) {
	GetMetrics().DBErrorTotal.Add(ctx, 1, metric.WithAttributes(
		MetricLabel.Theme.String(theme),
		attribute.String("op", op),
	))
	GetMetrics().localCounters.dbError.Add(1)
}

// recordQueueDrop 队列满时记录丢弃。
func recordQueueDrop(ctx context.Context, theme string) {
	GetMetrics().QueueDropTotal.Add(ctx, 1, metric.WithAttributes(MetricLabel.Theme.String(theme)))
	GetMetrics().localCounters.queueDrop.Add(1)
}

// recordQueueWait 入队时延（毫秒）。
func recordQueueWait(ctx context.Context, theme string, enqueueAt time.Time) {
	if enqueueAt.IsZero() {
		return
	}
	ms := time.Since(enqueueAt).Milliseconds()
	if ms < 0 {
		ms = 0
	}
	GetMetrics().QueueWaitMillis.Record(ctx, float64(ms), metric.WithAttributes(MetricLabel.Theme.String(theme)))
}

// recordHeartbeatDead 心跳失活回收。
func recordHeartbeatDead(ctx context.Context, theme string, n int) {
	if n <= 0 {
		return
	}
	GetMetrics().HeartbeatDeadDetected.Add(ctx, int64(n),
		metric.WithAttributes(MetricLabel.Theme.String(theme)))
	GetMetrics().localCounters.heartbeatDead.Add(int64(n))
}

// recordWorkerScale 扩缩容。
func recordWorkerScale(ctx context.Context, theme string, up bool, n int) {
	if n <= 0 {
		return
	}
	m := GetMetrics()
	attrs := metric.WithAttributes(MetricLabel.Theme.String(theme))
	if up {
		m.WorkerScaleUp.Add(ctx, int64(n), attrs)
		m.localCounters.workerUp.Add(int64(n))
	} else {
		m.WorkerScaleDown.Add(ctx, int64(n), attrs)
		m.localCounters.workerDown.Add(int64(n))
	}
}

// recordScan 扫描计数 + 耗时。
func recordScan(ctx context.Context, theme string, start time.Time, errored bool) {
	m := GetMetrics()
	status := "success"
	if errored {
		status = "failed"
	}
	attrs := metric.WithAttributes(
		MetricLabel.Theme.String(theme),
		MetricLabel.Status.String(status),
	)
	m.ScanTotal.Add(ctx, 1, attrs)
	m.ScanDuration.Record(ctx, float64(time.Since(start).Microseconds())/1000.0, attrs)
}

// Snapshot 返回当前进程内指标快照值（不依赖 OTel exporter）。
func (m *AsyncTaskMetrics) Snapshot() AsyncTaskMetricsValues {
	return AsyncTaskMetricsValues{
		RetryCount:      m.localCounters.retry.Load(),
		PanicCount:      m.localCounters.panic.Load(),
		QueueDropCount:  m.localCounters.queueDrop.Load(),
		HeartbeatDead:   m.localCounters.heartbeatDead.Load(),
		DBErrorCount:    m.localCounters.dbError.Load(),
		WorkerScaleUp:   m.localCounters.workerUp.Load(),
		WorkerScaleDown: m.localCounters.workerDown.Load(),
		AlarmFired:      m.localCounters.alarmFired.Load(),
		AlarmSuppressed: m.localCounters.alarmSuppress.Load(),
	}
}

// AsyncTaskMetricsValues 指标快照值（无锁可安全复制）。
type AsyncTaskMetricsValues struct {
	RetryCount      int64
	PanicCount      int64
	QueueDropCount  int64
	HeartbeatDead   int64
	DBErrorCount    int64
	WorkerScaleUp   int64
	WorkerScaleDown int64
	AlarmFired      int64
	AlarmSuppressed int64
}

// MetricsSnapshot 是面向 admin / HTTP API 的指标快照（本进程内累计值），
// 不依赖 OTel exporter，方便简单场景直接读取。
type MetricsSnapshot struct {
	Theme         string `json:"theme"`
	QueueDepth    int    `json:"queue_depth"`
	QueueCapacity int    `json:"queue_capacity"`
	QueueUsage    string `json:"queue_usage"` // 百分比字符串，便于直接展示

	Status SchedulerStatusInfo `json:"status"`

	// 进程内累计，便于不接 OTel 时也能观测
	RetryCount       int64 `json:"retry_count"`
	PanicCount       int64 `json:"panic_count"`
	QueueDropCount   int64 `json:"queue_drop_count"`
	HeartbeatDead    int64 `json:"heartbeat_dead_count"`
	DBErrorCount     int64 `json:"db_error_count"`
	WorkerScaleUp    int64 `json:"worker_scale_up_count"`
	WorkerScaleDown  int64 `json:"worker_scale_down_count"`
	AlarmFiredCount  int64 `json:"alarm_fired_count"`
	AlarmSuppressed  int64 `json:"alarm_suppressed_count"`
	LastSnapshotTime int64 `json:"last_snapshot_time_ms"` // unix ms
}

// 进程内累计 counter（独立于 OTel，保证 admin 接口在无 exporter 时也能用）
type localCounters struct {
	retry         atomic.Int64
	panic         atomic.Int64
	queueDrop     atomic.Int64
	heartbeatDead atomic.Int64
	dbError       atomic.Int64
	workerUp      atomic.Int64
	workerDown    atomic.Int64
	alarmFired    atomic.Int64
	alarmSuppress atomic.Int64
}
