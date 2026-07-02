// Package jobs OTel 指标定义。
// @author wanlizhan
// @created 2026/05/27
package jobs

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// JobMetricLabel 为 jobs 指标提供统一的标签键。
var JobMetricLabel = struct {
	JobName       attribute.Key
	JobType       attribute.Key
	ServiceName   attribute.Key
	ServiceMethod attribute.Key
	Status        attribute.Key
	Reason        attribute.Key
}{
	JobName:       attribute.Key("job_name"),
	JobType:       attribute.Key("job_type"),
	ServiceName:   attribute.Key("service_name"),
	ServiceMethod: attribute.Key("service_method"),
	Status:        attribute.Key("status"),
	Reason:        attribute.Key("reason"),
}

// JobsMetrics 持有 jobs 模块的全部 OTel 指标。
// 通过 sync.Once 全局初始化一次；指标在 cron_service / cron_hook 关键路径录制。
// 若未配置 OTel SDK 提供者，所有 Record/Add 调用将安全地被丢弃（noop）。
type JobsMetrics struct {
	// 任务执行
	JobExecTotal    metric.Int64Counter     // 任务执行总次数（含跳过、成功、失败、panic、timeout）
	JobExecDuration metric.Float64Histogram // 任务执行耗时（毫秒）
	JobPanicTotal   metric.Int64Counter     // 任务 panic 次数
	JobTimeoutTotal metric.Int64Counter     // 任务超时次数
	JobSkipTotal    metric.Int64Counter     // 任务被跳过（已在运行）次数

	// 调度循环
	UpdateScanTotal    metric.Int64Counter     // updateJobs 扫描次数
	UpdateScanDuration metric.Float64Histogram // updateJobs 耗时（毫秒）
	UpdateScanError    metric.Int64Counter     // updateJobs 失败次数

	// gauge：当前已加载的 service 任务、hook 任务数（异步采样）
	LoadedServiceJobs metric.Int64ObservableGauge
	LoadedHookJobs    metric.Int64ObservableGauge

	// DB
	DBErrorTotal metric.Int64Counter

	// 告警
	AlarmFired      metric.Int64Counter
	AlarmSuppressed metric.Int64Counter

	// HA / 租约
	LeaseAcquireTotal  metric.Int64Counter // 尝试抢租总次数
	LeaseAcquiredTotal metric.Int64Counter // 抢租成功次数
	LeaseMissedTotal   metric.Int64Counter // 未抢到租约（被别人抢）次数
	LeaseRenewTotal    metric.Int64Counter // 续租成功次数
	LeaseRenewLost     metric.Int64Counter // 续租发现被别的副本接管的次数

	// 异步采样回调注册器
	observeProviders   []func(observer metric.Observer)
	observeProvidersMu sync.Mutex

	// localCounters 进程内累计 counter（独立于 OTel exporter）。
	localCounters jobsLocalCounters
}

var (
	jobsMetricsOnce   sync.Once
	jobsMetricsGlobal *JobsMetrics
)

// GetJobsMetrics 获取（懒初始化）jobs 全局指标对象。
func GetJobsMetrics() *JobsMetrics {
	jobsMetricsOnce.Do(func() {
		meter := otel.Meter("github.com/xxzhwl/gaia/components/jobs",
			metric.WithInstrumentationVersion("1.0.0"),
		)
		m := &JobsMetrics{}
		var err error

		m.JobExecTotal, err = meter.Int64Counter("jobs.exec.total",
			metric.WithDescription("Cron job execution events by status"))
		if err != nil {
			otel.Handle(err)
		}

		m.JobExecDuration, err = meter.Float64Histogram("jobs.exec.duration",
			metric.WithDescription("Cron job execution duration in milliseconds"),
			metric.WithUnit("ms"))
		if err != nil {
			otel.Handle(err)
		}

		m.JobPanicTotal, err = meter.Int64Counter("jobs.panic.total",
			metric.WithDescription("Cron job panic events"))
		if err != nil {
			otel.Handle(err)
		}

		m.JobTimeoutTotal, err = meter.Int64Counter("jobs.timeout.total",
			metric.WithDescription("Cron job timeout events"))
		if err != nil {
			otel.Handle(err)
		}

		m.JobSkipTotal, err = meter.Int64Counter("jobs.skip.total",
			metric.WithDescription("Cron job skipped because previous run still in progress"))
		if err != nil {
			otel.Handle(err)
		}

		m.UpdateScanTotal, err = meter.Int64Counter("jobs.update_scan.total",
			metric.WithDescription("Jobs DB sync loop scans"))
		if err != nil {
			otel.Handle(err)
		}

		m.UpdateScanDuration, err = meter.Float64Histogram("jobs.update_scan.duration",
			metric.WithDescription("Jobs DB sync loop duration"),
			metric.WithUnit("ms"))
		if err != nil {
			otel.Handle(err)
		}

		m.UpdateScanError, err = meter.Int64Counter("jobs.update_scan.error.total",
			metric.WithDescription("Jobs DB sync loop errors"))
		if err != nil {
			otel.Handle(err)
		}

		m.DBErrorTotal, err = meter.Int64Counter("jobs.db.error.total",
			metric.WithDescription("Jobs DB operation errors"))
		if err != nil {
			otel.Handle(err)
		}

		m.AlarmFired, err = meter.Int64Counter("jobs.alarm.fired.total",
			metric.WithDescription("Jobs alarms actually fired"))
		if err != nil {
			otel.Handle(err)
		}

		m.AlarmSuppressed, err = meter.Int64Counter("jobs.alarm.suppressed.total",
			metric.WithDescription("Jobs alarms suppressed by throttle/dedupe"))
		if err != nil {
			otel.Handle(err)
		}

		m.LeaseAcquireTotal, err = meter.Int64Counter("jobs.lease.acquire.total",
			metric.WithDescription("Jobs HA lease acquire attempts"))
		if err != nil {
			otel.Handle(err)
		}

		m.LeaseAcquiredTotal, err = meter.Int64Counter("jobs.lease.acquired.total",
			metric.WithDescription("Jobs HA lease successfully acquired"))
		if err != nil {
			otel.Handle(err)
		}

		m.LeaseMissedTotal, err = meter.Int64Counter("jobs.lease.missed.total",
			metric.WithDescription("Jobs HA lease missed (taken by other instance)"))
		if err != nil {
			otel.Handle(err)
		}

		m.LeaseRenewTotal, err = meter.Int64Counter("jobs.lease.renew.total",
			metric.WithDescription("Jobs HA lease renewed"))
		if err != nil {
			otel.Handle(err)
		}

		m.LeaseRenewLost, err = meter.Int64Counter("jobs.lease.renew.lost.total",
			metric.WithDescription("Jobs HA lease lost during renew (taken by other instance)"))
		if err != nil {
			otel.Handle(err)
		}

		m.LoadedServiceJobs, err = meter.Int64ObservableGauge("jobs.loaded.service",
			metric.WithDescription("Currently loaded cron service jobs"))
		if err != nil {
			otel.Handle(err)
		}

		m.LoadedHookJobs, err = meter.Int64ObservableGauge("jobs.loaded.hook",
			metric.WithDescription("Currently loaded cron hook jobs"))
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
		}, m.LoadedServiceJobs, m.LoadedHookJobs)
		if err != nil {
			otel.Handle(err)
		}

		jobsMetricsGlobal = m
	})
	return jobsMetricsGlobal
}

// registerObserveProvider 注册 observable callback；返回的 unregister 用于注销。
func (m *JobsMetrics) registerObserveProvider(p func(observer metric.Observer)) (unregister func()) {
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

// Snapshot 返回当前进程内指标快照值（不依赖 OTel exporter）。
func (m *JobsMetrics) Snapshot() JobsMetricsSnapshot {
	return JobsMetricsSnapshot{
		ExecTotal:       m.localCounters.exec.Load(),
		SuccessTotal:    m.localCounters.success.Load(),
		FailedTotal:     m.localCounters.failed.Load(),
		PanicTotal:      m.localCounters.panicCnt.Load(),
		TimeoutTotal:    m.localCounters.timeoutCnt.Load(),
		SkipTotal:       m.localCounters.skipCnt.Load(),
		UpdateScanTotal: m.localCounters.updateScanCnt.Load(),
		UpdateScanError: m.localCounters.updateScanErr.Load(),
		DBErrorTotal:    m.localCounters.dbError.Load(),
		AlarmFired:      m.localCounters.alarmFired.Load(),
		AlarmSuppressed: m.localCounters.alarmSuppressed.Load(),
		LastSnapshotMs:  time.Now().UnixMilli(),
	}
}

// jobAttrs 构造 job 通用 attribute 集合。
func jobAttrs(j JobBase, status string) []attribute.KeyValue {
	return []attribute.KeyValue{
		JobMetricLabel.JobName.String(j.JobName),
		JobMetricLabel.JobType.String(j.JobType),
		JobMetricLabel.ServiceName.String(j.ServiceName),
		JobMetricLabel.ServiceMethod.String(j.ServiceMethod),
		JobMetricLabel.Status.String(status),
	}
}

// recordJobExec 记录 job 执行计数 + 耗时直方图。
func recordJobExec(ctx context.Context, j JobBase, status string, durationMs int64) {
	m := GetJobsMetrics()
	attrs := jobAttrs(j, status)
	m.JobExecTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.localCounters.exec.Add(1)
	switch status {
	case "success":
		m.localCounters.success.Add(1)
	case "failed":
		m.localCounters.failed.Add(1)
	}
	if durationMs > 0 {
		m.JobExecDuration.Record(ctx, float64(durationMs), metric.WithAttributes(attrs...))
	}
}

// recordJobPanic 记录 job panic。
func recordJobPanic(ctx context.Context, j JobBase) {
	GetJobsMetrics().JobPanicTotal.Add(ctx, 1, metric.WithAttributes(jobAttrs(j, "panic")...))
	GetJobsMetrics().localCounters.panicCnt.Add(1)
}

// recordJobTimeout 记录 job timeout。
func recordJobTimeout(ctx context.Context, j JobBase) {
	GetJobsMetrics().JobTimeoutTotal.Add(ctx, 1, metric.WithAttributes(jobAttrs(j, "timeout")...))
	GetJobsMetrics().localCounters.timeoutCnt.Add(1)
}

// recordJobSkip 记录 job 被跳过。
func recordJobSkip(ctx context.Context, j JobBase) {
	GetJobsMetrics().JobSkipTotal.Add(ctx, 1, metric.WithAttributes(jobAttrs(j, "skip")...))
	GetJobsMetrics().localCounters.skipCnt.Add(1)
}

// recordUpdateScan 记录 updateJobs 扫描事件。
func recordUpdateScan(ctx context.Context, start time.Time, errored bool) {
	m := GetJobsMetrics()
	status := "success"
	if errored {
		status = "failed"
	}
	attrs := metric.WithAttributes(JobMetricLabel.Status.String(status))
	m.UpdateScanTotal.Add(ctx, 1, attrs)
	m.localCounters.updateScanCnt.Add(1)
	m.UpdateScanDuration.Record(ctx, float64(time.Since(start).Microseconds())/1000.0, attrs)
	if errored {
		m.UpdateScanError.Add(ctx, 1)
		m.localCounters.updateScanErr.Add(1)
	}
}

// recordJobDBError 记录 jobs DB 错误。
func recordJobDBError(ctx context.Context, op string) {
	GetJobsMetrics().DBErrorTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("op", op)))
	GetJobsMetrics().localCounters.dbError.Add(1)
}

// recordLeaseAcquire 记录抢租事件。
//
//	acquired==true  -> 抢租成功；acquired==false -> 未抢到（跳过本轮）。
func recordLeaseAcquire(ctx context.Context, j JobBase, acquired bool) {
	m := GetJobsMetrics()
	attrs := metric.WithAttributes(jobAttrs(j, "acquire")...)
	m.LeaseAcquireTotal.Add(ctx, 1, attrs)
	if acquired {
		m.LeaseAcquiredTotal.Add(ctx, 1, attrs)
	} else {
		m.LeaseMissedTotal.Add(ctx, 1, attrs)
	}
}

// recordLeaseRenew 记录续租事件；ok==true 为成功，ok==false 为续租被别的副本接管。
func recordLeaseRenew(ctx context.Context, ok bool) {
	m := GetJobsMetrics()
	if ok {
		m.LeaseRenewTotal.Add(ctx, 1)
	} else {
		m.LeaseRenewLost.Add(ctx, 1)
	}
}

// JobsMetricsSnapshot 是面向 admin / HTTP API 的指标快照（本进程内累计值）。
type JobsMetricsSnapshot struct {
	LoadedServiceJobs int   `json:"loaded_service_jobs"` // 当前加载的 cron_service 任务数
	LoadedHookJobs    int   `json:"loaded_hook_jobs"`    // 当前加载的 cron_hook 任务数
	ExecTotal         int64 `json:"exec_total"`          // 进程内累计执行次数
	SuccessTotal      int64 `json:"success_total"`
	FailedTotal       int64 `json:"failed_total"`
	PanicTotal        int64 `json:"panic_total"`
	TimeoutTotal      int64 `json:"timeout_total"`
	SkipTotal         int64 `json:"skip_total"`
	UpdateScanTotal   int64 `json:"update_scan_total"`
	UpdateScanError   int64 `json:"update_scan_error"`
	DBErrorTotal      int64 `json:"db_error_total"`
	AlarmFired        int64 `json:"alarm_fired_total"`
	AlarmSuppressed   int64 `json:"alarm_suppressed_total"`
	LastSnapshotMs    int64 `json:"last_snapshot_time_ms"`
}

// jobsLocalCounters 进程内累计 counter（独立于 OTel）。
type jobsLocalCounters struct {
	exec            atomic.Int64
	success         atomic.Int64
	failed          atomic.Int64
	panicCnt        atomic.Int64
	timeoutCnt      atomic.Int64
	skipCnt         atomic.Int64
	updateScanCnt   atomic.Int64
	updateScanErr   atomic.Int64
	dbError         atomic.Int64
	alarmFired      atomic.Int64
	alarmSuppressed atomic.Int64
}
