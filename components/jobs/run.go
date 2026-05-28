// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
	otelmetric "go.opentelemetry.io/otel/metric"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/gexit"
)

type RunJob struct {
	exitContext context.Context

	instanceLogger *logImpl.DefaultLogger
	cronJobLogger  *logImpl.DefaultLogger
	cronHookLogger *logImpl.DefaultLogger

	currentCronServiceJobMap map[int64]cronJob
	currentCronHookJobMap    map[int64]cronJob
	currentServiceJobIds     []int64
	currentHookJobIds        []int64
	cronScheduler            *cron.Cron

	dbSchema string

	once sync.Once

	schedulerRunning atomic.Bool

	// 并发安全锁
	jobMapLock sync.RWMutex // 保护 currentCronServiceJobMap 和 currentCronHookJobMap
	jobIdsLock sync.RWMutex // 保护 currentServiceJobIds 和 currentHookJobIds

	// Hook 仅作用于本 RunJob 实例的事件钩子（与全局 RegisterJobHook 共存）。
	Hook JobHook

	// 告警去重器；nil 表示直接发送，不做聚合。
	alarmThrottle *JobAlarmThrottle

	// 进程内累计 counter（独立于 OTel exporter）。
	counters jobsLocalCounters

	// observable gauge 注销函数
	observeUnregister func()

	// runningWg 跟踪所有正在执行的任务 goroutine + 心跳 goroutine，用于优雅退出。
	runningWg sync.WaitGroup

	// haDisabled true 时退化为旧的单副本行为（不抢锁，不续租）。
	// 默认开启 HA。
	haDisabled bool

	// stopWaitTimeout Stop() 等待任务收尾的最大时长，0 取默认值。
	stopWaitTimeout time.Duration
}

func NewRunJob() *RunJob {
	return &RunJob{
		exitContext:    gexit.GetExitContext(),
		dbSchema:       "Framework.Mysql",
		instanceLogger: logImpl.NewDefaultLogger().SetTitle("Jobs"),
		cronJobLogger:  logImpl.NewDefaultLogger().SetTitle("CronJob"),
		cronHookLogger: logImpl.NewDefaultLogger().SetTitle("CronHook"),
		cronScheduler: cron.New(cron.WithSeconds(), cron.WithChain(
			cron.DelayIfStillRunning(CronLogger{logger: logImpl.NewDefaultLogger().SetTitle(
				"DelayIfStillRunning")}))),
		currentCronServiceJobMap: make(map[int64]cronJob),
		currentCronHookJobMap:    make(map[int64]cronJob),
		currentServiceJobIds:     make([]int64, 0),
		currentHookJobIds:        make([]int64, 0),
	}
}

// Bootstrap 运行 jobs 相关表的自动迁移。
func (r *RunJob) Bootstrap(ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return fmt.Errorf("jobs bootstrap db: %w", err)
	}
	if err := db.GetGormDb().WithContext(ctx).AutoMigrate(
		&job{},
		&jobRecord{},
	); err != nil {
		return fmt.Errorf("jobs migrate tables: %w", err)
	}
	return nil
}

func (r *RunJob) WithDbSchema(dbSchema string) *RunJob {
	r.dbSchema = dbSchema
	return r
}

// Run Jobs服务启动
// 这是一是个for循环常驻服务，要不断执行
func (r *RunJob) Run() {
	r.once.Do(r.run)
}

// StartAsync 异步启动 Jobs 调度循环，适合上层 admin handler 调用。
func (r *RunJob) StartAsync() {
	started := false
	r.once.Do(func() {
		started = true
		go r.run()
	})
	if !started {
		r.Resume()
	}
}

func (r *RunJob) run() {
	gaia.BuildContextTrace()
	defer gaia.RemoveContextTrace()
	banList := gaia.GetSafeConfStringSliceFromString("Jobs.BanEnvList")
	//禁止启动的环境
	if gaia.InList(gaia.GetEnvFlag(), banList) {
		gaia.Log(gaia.LogWarnLevel, fmt.Sprintf("Jobs is ban in current env[%s]", gaia.GetEnvFlag()))
		return
	}

	// 注册 OTel observable gauge 异步采样回调
	r.observeUnregister = GetJobsMetrics().registerObserveProvider(r.observeGauges)

	// 启动前清理同机上一代进程残留的租约，避免本机重启后任务长时间卡 Running。
	if !r.haDisabled {
		if err := r.reclaimMyOrphanLeases(context.Background()); err != nil {
			r.instanceLogger.ErrorF("回收本机旧租约失败: %s", err.Error())
		} else {
			r.instanceLogger.InfoF("jobs HA enabled, instance_id=%s", GetInstanceId())
		}
	}

	r.cronScheduler.Start()
	r.schedulerRunning.Store(true)

	// Auto-register built-in cron services as disabled jobs
	if err := r.registerBuiltinJobs(context.Background()); err != nil {
		r.instanceLogger.WarnF("auto register builtin jobs failed: %s", err.Error())
	}

	r.sendComponentNotify("启动")

	for {
		select {
		case <-r.exitContext.Done():
			gaia.InfoF("Received exit signal,RunJob shutting down")
			r.Stop()
			return
		case <-time.After(5 * time.Second):
			scanStart := time.Now()
			r.counters.updateScanCnt.Add(1)
			if err := r.updateJobs(); err != nil {
				r.counters.updateScanErr.Add(1)
				recordUpdateScan(context.Background(), scanStart, true)
				r.fireAlarm("JobsErr", err.Error())
				r.instanceLogger.ErrorF("Update jobs error: %s", err.Error())
			} else {
				recordUpdateScan(context.Background(), scanStart, false)
			}
		}
	}
}

func (r *RunJob) Stop() {
	if !r.schedulerRunning.Swap(false) {
		return
	}

	// 1) 先暂停 cron 调度器（其返回的 ctx 在所有 in-flight job 结束时关闭）。
	cronCtx := r.cronScheduler.Stop()

	// 2) 等待 cron 内已触发但还在排队/执行的任务收尾；带超时兜底。
	waitTimeout := r.stopWaitTimeout
	if waitTimeout <= 0 {
		waitTimeout = DefaultStopWaitTimeout
	}
	select {
	case <-cronCtx.Done():
	case <-time.After(waitTimeout):
		r.instanceLogger.WarnF("等待 cron 内任务退出超时(>%v)，继续关停", waitTimeout)
	}

	// 3) 等待我们自己起的心跳/任务 goroutine 退出。
	waitDone := make(chan struct{})
	go func() {
		r.runningWg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(waitTimeout):
		r.instanceLogger.WarnF("等待 RunJob 内 goroutine 退出超时(>%v)，继续关停", waitTimeout)
	}

	// 4) 仅释放属于本实例的租约（不要碰别的副本！）。
	if !r.haDisabled {
		if err := r.releaseAllMyLeases(context.Background()); err != nil {
			r.fireAlarm("JobsErr", "ReleaseLeaseError:"+err.Error())
			r.instanceLogger.ErrorF("释放本实例租约失败: %s", err.Error())
		}
	}

	if r.observeUnregister != nil {
		r.observeUnregister()
		r.observeUnregister = nil
	}
	if r.alarmThrottle != nil {
		r.alarmThrottle.Flush()
	}

	r.sendComponentNotify("停止")
}

// releaseAllMyLeases 关停时把本实例所有未释放的租约置回待运行。
func (r *RunJob) releaseAllMyLeases(ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	now := time.Now()
	tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Where("lease_owner = ?", GetInstanceId()).
		Updates(map[string]interface{}{
			"run_status":      RunStatusWait,
			"lease_owner":     "",
			"lease_expire_at": nil,
			"update_time":     now,
		})
	return tx.Error
}

// WithoutHA 关闭 HA 抢锁逻辑，退回旧单副本模式。
// 默认开启 HA，仅当你确定全集群只跑 1 个 jobs 实例时才需要调用。
func (r *RunJob) WithoutHA() *RunJob {
	r.haDisabled = true
	return r
}

// WithStopWaitTimeout 自定义 Stop() 等待 in-flight 任务退出的最大时长。
func (r *RunJob) WithStopWaitTimeout(d time.Duration) *RunJob {
	r.stopWaitTimeout = d
	return r
}

// Resume 重新启动 cron 调度器（对应 Stop 的逆操作）。
// 适用于 admin 接口中停止后重新恢复调度。
func (r *RunJob) Resume() {
	if r.schedulerRunning.Swap(true) {
		return
	}
	r.cronScheduler.Start()
	r.sendComponentNotify("恢复")
}

// WithHook 注册一个仅作用于本 RunJob 实例的 Hook。
func (r *RunJob) WithHook(h JobHook) *RunJob {
	r.Hook = h
	return r
}

// WithAlarmThrottle 启用告警去重/限流；window<=0 时使用默认 5min。
func (r *RunJob) WithAlarmThrottle(window time.Duration) *RunJob {
	r.alarmThrottle = NewJobAlarmThrottle(window)
	return r
}

// fireAlarm 统一告警入口；启用 throttle 时去重，否则直接发送。
func (r *RunJob) fireAlarm(title, content string) {
	if r.alarmThrottle != nil {
		r.counters.alarmFired.Add(1)
		r.alarmThrottle.Send(title, content)
		return
	}
	r.counters.alarmFired.Add(1)
	GetJobsMetrics().AlarmFired.Add(context.Background(), 1)
	_ = gaia.SendSystemAlarm(title, content)
}

// observeGauges 是 OTel observable gauge 异步采样回调。
func (r *RunJob) observeGauges(observer otelmetric.Observer) {
	defer func() { recover() }()

	m := GetJobsMetrics()
	r.jobMapLock.RLock()
	service := int64(len(r.currentCronServiceJobMap))
	hook := int64(len(r.currentCronHookJobMap))
	r.jobMapLock.RUnlock()

	observer.ObserveInt64(m.LoadedServiceJobs, service)
	observer.ObserveInt64(m.LoadedHookJobs, hook)
}

// SnapshotMetrics 返回当前 RunJob 的指标快照。
func (r *RunJob) SnapshotMetrics() JobsMetricsSnapshot {
	r.jobMapLock.RLock()
	service := len(r.currentCronServiceJobMap)
	hook := len(r.currentCronHookJobMap)
	r.jobMapLock.RUnlock()

	return JobsMetricsSnapshot{
		LoadedServiceJobs: service,
		LoadedHookJobs:    hook,
		ExecTotal:         r.counters.exec.Load(),
		SuccessTotal:      r.counters.success.Load(),
		FailedTotal:       r.counters.failed.Load(),
		PanicTotal:        r.counters.panicCnt.Load(),
		TimeoutTotal:      r.counters.timeoutCnt.Load(),
		SkipTotal:         r.counters.skipCnt.Load(),
		UpdateScanTotal:   r.counters.updateScanCnt.Load(),
		UpdateScanError:   r.counters.updateScanErr.Load(),
		DBErrorTotal:      r.counters.dbError.Load(),
		AlarmFired:        r.counters.alarmFired.Load(),
		AlarmSuppressed:   r.counters.alarmSuppressed.Load(),
		LastSnapshotMs:    time.Now().UnixMilli(),
	}
}

// sendComponentNotify 发送组件生命周期通知
func (r *RunJob) sendComponentNotify(action string) {
	defer gaia.CatchPanic()
	snap := r.SnapshotMetrics()
	if err := gaia.SendLifecycleNotify(gaia.LifecycleNotifyInfo{
		Kind:      "component",
		Name:      "Jobs",
		Component: "Jobs",
		Action:    action,
		Status:    lifecycleStatus(action),
		Schema:    r.dbSchema,
		Instance:  GetInstanceId(),
		Fields: map[string]any{
			"ha_disabled":   r.haDisabled,
			"exec_total":    snap.ExecTotal,
			"success_total": snap.SuccessTotal,
			"failed_total":  snap.FailedTotal,
			"panic_total":   snap.PanicTotal,
			"timeout_total": snap.TimeoutTotal,
			"skip_total":    snap.SkipTotal,
		},
	}); err != nil {
		gaia.WarnF("[Jobs] 发送%s通知失败: %s", action, err.Error())
	}
}

func lifecycleStatus(action string) string {
	switch action {
	case "启动", "恢复":
		return "running"
	case "停止":
		return "stopped"
	default:
		return action
	}
}

// IsHealthy 简单的健康判断：调度器运行中 + 最近 5 分钟更新扫描成功率 > 50%。
// 用于对接 /healthz 探针。
func (r *RunJob) IsHealthy() bool {
	total := r.counters.updateScanCnt.Load()
	errs := r.counters.updateScanErr.Load()
	if total <= 0 {
		return true // 还没开始扫描，先视为健康
	}
	return (total-errs)*100/total >= 50
}
