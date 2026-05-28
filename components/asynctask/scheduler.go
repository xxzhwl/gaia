// Package asynctask 注释
// @author wanlizhan
// @created 2024/4/29
package asynctask

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/tracer"
	"github.com/xxzhwl/gaia/gexit"
)

const (
	DefaultWorkerNum = 30
	MaxWorkerNum     = 500

	DefaultScanTaskInterval  = time.Second * 5
	DefaultHeartBeatInterval = time.Second * 5
	DefaultMonitInterval     = time.Second * 5
	DefaultTaskIdChanLength  = 100
	DefaultScanTaskNum       = 50
	MaxScanTaskNum           = 500
)

type PreHandlerFunc func() error

type PostHandlerFunc func(taskId int64) error

type AlarmHandlerFunc func(taskId int64, message string) error

var SchedulerMap = make(map[string]*Scheduler)

var locker sync.RWMutex

type Scheduler struct {
	Theme string

	WorkerNum         int
	ScanTaskInterval  time.Duration
	HeartBeatInterval time.Duration
	MonitInterval     time.Duration
	TaskIdChanLength  int
	ScanTaskNum       int

	PreHandler   PreHandlerFunc
	PostHandler  PostHandlerFunc
	AlarmHandler AlarmHandlerFunc

	// Hook 仅作用于本 scheduler 的任务事件钩子（与全局 RegisterTaskHook 共存）。
	Hook TaskHook

	tracer trace.Tracer

	taskIdChan chan int64
	startFlag  int32
	running    int32
	stopping   int32
	statusInfo SchedulerStatusInfo

	// 进程内独立累计 counter（不依赖 OTel exporter）
	counters localCounters

	// 任务入队内存队列的时间戳，用于计算队列等待耗时
	enqueueAt   map[int64]time.Time
	enqueueAtMu sync.Mutex

	// 告警去重器；nil 表示直接发送，不做聚合
	alarmThrottle *AlarmThrottle

	// observable gauge 注销函数
	observeUnregister func()

	inQueueTaskIds   map[int64]struct{}
	inQueueTaskIdsRw sync.RWMutex

	workerLastRunTime  map[string]time.Time
	lastRemoveWorkerRw sync.RWMutex

	runningWorkers sync.WaitGroup

	l sync.RWMutex

	Logger *logImpl.DefaultLogger

	exitContext context.Context
	stopCancel  context.CancelFunc
	stopContext context.Context
}

func NewScheduler(theme string, options ...SchedulerOption) *Scheduler {
	locker.Lock()
	defer locker.Unlock()

	if len(theme) == 0 {
		theme = "asynctask"
	}
	if v, ok := SchedulerMap[theme]; ok {
		return v
	}

	s := &Scheduler{exitContext: gexit.GetExitContext()}
	s.stopContext, s.stopCancel = context.WithCancel(context.Background())
	s.Apply(options...)
	s.Theme = theme

	if s.Logger == nil {
		s.Logger = logImpl.NewDefaultLogger().SetTitle(s.Theme)
	}
	if s.WorkerNum <= 0 || s.WorkerNum >= MaxWorkerNum {
		s.WorkerNum = DefaultWorkerNum
	}
	if s.ScanTaskInterval <= 1*time.Second {
		s.ScanTaskInterval = DefaultScanTaskInterval
	}
	if s.HeartBeatInterval <= 1*time.Second {
		s.HeartBeatInterval = DefaultHeartBeatInterval
	}
	if s.MonitInterval <= 0 {
		s.MonitInterval = DefaultMonitInterval
	}
	if s.TaskIdChanLength <= 0 {
		s.TaskIdChanLength = DefaultTaskIdChanLength
	}
	if s.ScanTaskNum <= 0 || s.ScanTaskNum > MaxScanTaskNum {
		s.ScanTaskNum = DefaultScanTaskNum
	}
	s.taskIdChan = make(chan int64, s.TaskIdChanLength)
	s.inQueueTaskIds = make(map[int64]struct{})
	s.workerLastRunTime = make(map[string]time.Time)
	s.enqueueAt = make(map[int64]time.Time)
	SchedulerMap[s.Theme] = s
	return s
}

// Bootstrap 运行 asynctask 相关表的自动迁移。
func (s *Scheduler) Bootstrap(ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return fmt.Errorf("asynctask bootstrap db: %w", err)
	}
	if err := db.GetGormDb().WithContext(ctx).AutoMigrate(
		&TaskModel{},
		&HeartBeatModel{},
		&TaskExecModel{},
	); err != nil {
		return fmt.Errorf("asynctask migrate tables: %w", err)
	}
	return nil
}

func GetScheduler(theme string) *Scheduler {
	locker.RLock()
	defer locker.RUnlock()
	if v, ok := SchedulerMap[theme]; ok {
		return v
	}
	return nil
}

func (s *Scheduler) Start(ctx context.Context) {
	tracer.SetupTracer(ctx, "asyncTask_"+s.Theme)
	s.tracer = otel.Tracer("asyncTask_" + s.Theme)

	gaia.InfoF("%s调度器启动中", s.Theme)
	if atomic.SwapInt32(&s.startFlag, 1) != 0 {
		return
	}

	atomic.StoreInt32(&s.running, 1)
	atomic.StoreInt32(&s.stopping, 0)

	s.sendComponentNotify("启动")

	s.stopContext, s.stopCancel = context.WithCancel(context.Background())

	// 注册 OTel observable gauge 异步采样回调，scheduler 停止时会被注销
	s.observeUnregister = GetMetrics().registerObserveProvider(s.observeGauges)

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.scanTasks()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.wakeUpWorker(s.initialWorkerCount())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.monit()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.heartBeat()
	}()
	gaia.InfoF("%s调度器完成中", s.Theme)
	wg.Wait()
}

func (s *Scheduler) Stop() {
	if !atomic.CompareAndSwapInt32(&s.running, 1, 0) {
		return
	}

	if !atomic.CompareAndSwapInt32(&s.stopping, 0, 1) {
		return
	}

	s.Logger.Info("Scheduler stopping...")

	s.stopCancel()

	s.drainTaskQueue()

	s.Logger.Info("Waiting for running workers to finish...")
	s.runningWorkers.Wait()

	if s.observeUnregister != nil {
		s.observeUnregister()
		s.observeUnregister = nil
	}
	if s.alarmThrottle != nil {
		s.alarmThrottle.Flush()
	}

	s.sendComponentNotify("停止")

	s.Logger.Info("Scheduler stopped completely")
}

func (s *Scheduler) drainTaskQueue() {
	// 1) 仅持锁期间快速排空 channel + 收集 inQueue ids（非阻塞操作）
	s.inQueueTaskIdsRw.Lock()
	taskIds := make([]int64, 0)
Drain:
	for {
		select {
		case taskId := <-s.taskIdChan:
			taskIds = append(taskIds, taskId)
		default:
			// channel 已空，跳出 for（带标签 break，避免误用 goto）
			break Drain
		}
	}
	// 同步删除 inQueueTaskIds 标记
	for _, id := range taskIds {
		delete(s.inQueueTaskIds, id)
	}
	s.inQueueTaskIdsRw.Unlock()

	if len(taskIds) == 0 {
		s.Logger.Info("Task queue is empty")
		return
	}

	s.Logger.InfoF("Draining %d tasks from queue, resetting to Wait status", len(taskIds))

	// 2) DB 操作放在锁外执行（带 30s 超时兜底）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		s.Logger.ErrorF("Failed to connect database for draining tasks: %s", err.Error())
		return
	}

	now := time.Now()
	err = db.GetGormDb().WithContext(ctx).Table(taskTable).
		Where("id IN ? AND task_status = ?", taskIds, TaskStatusRunning.String()).
		Updates(map[string]any{
			"task_status": TaskStatusWait.String(),
			"update_time": now,
		}).Error

	if err != nil {
		s.Logger.ErrorF("Failed to reset task status: %s", err.Error())
		return
	}

	s.Logger.InfoF("Successfully drained %d tasks from queue", len(taskIds))
}

func (s *Scheduler) Resume() {
	if atomic.CompareAndSwapInt32(&s.stopping, 1, 0) {
		atomic.StoreInt32(&s.running, 1)
		s.stopContext, s.stopCancel = context.WithCancel(context.Background())
		s.Logger.Info("Scheduler resumed")

		// 重新注册 observable gauge 异步采样回调（Stop 时已注销）
		if s.observeUnregister == nil {
			s.observeUnregister = GetMetrics().registerObserveProvider(s.observeGauges)
		}

		go func() {
			defer gaia.CatchPanic()
			s.wakeUpWorker(s.initialWorkerCount())
		}()
		go func() {
			defer gaia.CatchPanic()
			s.scanTasks()
		}()
		go func() {
			defer gaia.CatchPanic()
			s.monit()
		}()
		go func() {
			defer gaia.CatchPanic()
			s.heartBeat()
		}()
		s.sendComponentNotify("恢复")
	}
}

func (s *Scheduler) IsRunning() bool {
	return atomic.LoadInt32(&s.running) == 1
}

func (s *Scheduler) IsStopping() bool {
	return atomic.LoadInt32(&s.stopping) == 1
}

func (s *Scheduler) ReceiveTask(task TaskBaseInfo) (model TaskModel, err error) {
	return AddTask(task, s.Theme, context.Background())
}

func (s *Scheduler) scanTasks() {
	gaia.BuildContextTrace()
	for {
		select {
		case <-s.exitContext.Done():
			gaia.InfoF("Received exit signal,AsyncTask Scans shutting down")
			return
		case <-s.stopContext.Done():
			s.Logger.Info("ScanTasks received stop signal, shutting down")
			return
		case <-time.After(s.ScanTaskInterval):
			if s.IsStopping() {
				return
			}

			start, span := s.tracer.Start(context.Background(), "ScanTasks")
			scanStart := time.Now()
			scans := atomic.AddInt64(&s.statusInfo.Scans, 1)
			ids, _, err := findNeedRunTaskIds(s.ScanTaskNum, s.Theme, start)
			recordScan(start, s.Theme, scanStart, err != nil)
			if err != nil {
				s.Logger.Error("扫描需要运行的任务Id列表失败:" + err.Error())
				s.counters.dbError.Add(1)
				recordDBError(start, s.Theme, "scan")
				span.End()
				continue
			}
			span.End()
			if len(ids) != 0 {
				needWorkers := int32(len(ids)) - (atomic.LoadInt32(&s.statusInfo.AllWorkers) - atomic.LoadInt32(&s.statusInfo.
					RunningWorkers))
				s.wakeUpWorker(needWorkers)

				newIds := make([]int64, 0)
				for _, id := range ids {
					ok := s.allowInQueue(id)
					if ok {
						select {
						case s.taskIdChan <- id:
							s.markEnqueued(id)
							newIds = append(newIds, id)
						default:
							s.deleteInQueue(id)
							s.counters.queueDrop.Add(1)
							recordQueueDrop(start, s.Theme)
						}
					}
					atomic.AddInt64(&s.statusInfo.PushTasks, 1)
				}
				s.Logger.InfoF("扫描次数:%d,扫描到待运行的任务:%v", scans, newIds)
			}
		}
	}
}

func (s *Scheduler) allowInQueue(taskId int64) bool {
	s.inQueueTaskIdsRw.Lock()
	defer s.inQueueTaskIdsRw.Unlock()
	if _, ok := s.inQueueTaskIds[taskId]; ok {
		return false
	}
	s.inQueueTaskIds[taskId] = struct{}{}
	return true
}

func (s *Scheduler) deleteInQueue(taskId int64) {
	s.inQueueTaskIdsRw.Lock()
	defer s.inQueueTaskIdsRw.Unlock()
	delete(s.inQueueTaskIds, taskId)
}

func (s *Scheduler) monit() {
	gaia.BuildContextTrace()
	for {
		select {
		case <-s.exitContext.Done():
			gaia.InfoF("Received exit signal,AsyncTask monitor shutting down")
			return
		case <-s.stopContext.Done():
			s.Logger.Info("Monitor received stop signal, shutting down")
			return
		case <-time.After(s.MonitInterval):
			if float64(len(s.taskIdChan))/float64(s.TaskIdChanLength) > 0.8 {
				s.Logger.WarnF("队列将满,%d---%d", len(s.taskIdChan), s.TaskIdChanLength)
				s.fireAlarm(fmt.Sprintf("TaskMgr:%s队列将满", s.Theme),
					fmt.Sprintf("当前任务队列长度%d-任务队列总长度%d", len(s.taskIdChan), s.TaskIdChanLength))
			}

			status := s.GetStatusInfo()

			s.Logger.DebugF("队列任务数%d,入队任务数:%d，拉取任务数:%d,执行任务数:%d,成功任务数:%d,失败任务数:%d,重试:%d,Panic:%d,运行中workers:%d,全部workers:%d,丢弃:%d,失活:%d",
				len(s.taskIdChan), status.PushTasks, status.PullTasks, status.ExecTasks, status.ExecSuccess, status.ExecFails,
				status.RetryCount, status.PanicCount,
				status.RunningWorkers, status.AllWorkers,
				status.QueueDropCount, status.HeartbeatDead)

			s.logPerformanceMetrics()
		}
	}
}

func (s *Scheduler) logPerformanceMetrics() {
	ctx := gaia.NewContextTrace().GetParentCtx()
	stats, err := GetTaskExecStats(s.Theme, ctx)
	if err != nil {
		s.Logger.ErrorF("获取任务执行统计失败:%s", err.Error())
		return
	}

	if stats.TotalTasks > 0 {
		s.Logger.InfoF("最近任务统计-总数:%d,平均执行时间:%dms,P99执行时间:%dms",
			stats.TotalTasks,
			stats.AvgExecTime,
			stats.P99ExecTime,
		)
	}
}

func (s *Scheduler) TaskQuickQueue(taskId int64) {
	go func() {
		if !s.allowInQueue(taskId) {
			return
		}
		select {
		case s.taskIdChan <- taskId:
			s.markEnqueued(taskId)
			atomic.AddInt64(&s.statusInfo.PushTasks, 1)

		case <-time.After(100 * time.Millisecond):
			s.deleteInQueue(taskId)
			s.counters.queueDrop.Add(1)
			recordQueueDrop(context.Background(), s.Theme)
			return
		}
	}()

}

func (s *Scheduler) GetStatusInfo() SchedulerStatusInfo {
	return SchedulerStatusInfo{
		Scans:           atomic.LoadInt64(&s.statusInfo.Scans),
		PushTasks:       atomic.LoadInt64(&s.statusInfo.PushTasks),
		PullTasks:       atomic.LoadInt64(&s.statusInfo.PullTasks),
		ExecTasks:       atomic.LoadInt64(&s.statusInfo.ExecTasks),
		ExecSuccess:     atomic.LoadInt64(&s.statusInfo.ExecSuccess),
		ExecFails:       atomic.LoadInt64(&s.statusInfo.ExecFails),
		RunningWorkers:  atomic.LoadInt32(&s.statusInfo.RunningWorkers),
		AllWorkers:      atomic.LoadInt32(&s.statusInfo.AllWorkers),
		RetryCount:      s.counters.retry.Load(),
		PanicCount:      s.counters.panic.Load(),
		QueueDropCount:  s.counters.queueDrop.Load(),
		HeartbeatDead:   s.counters.heartbeatDead.Load(),
		DBErrorCount:    s.counters.dbError.Load(),
		WorkerScaleUp:   s.counters.workerUp.Load(),
		WorkerScaleDown: s.counters.workerDown.Load(),
		AlarmFired:      s.counters.alarmFired.Load(),
		AlarmSuppressed: s.counters.alarmSuppress.Load(),
	}
}

func GetStatusInfo(theme string) SchedulerStatusInfo {
	if v := GetScheduler(theme); v != nil {
		return v.GetStatusInfo()
	}
	return SchedulerStatusInfo{}
}

func (s *Scheduler) initialWorkerCount() int32 {
	return int32(min(10, s.WorkerNum))
}

// SnapshotMetrics 返回本调度器当前的指标快照。
// 包含队列水位、worker 数、状态计数等，便于 admin 接口直接读取。
func (s *Scheduler) SnapshotMetrics() MetricsSnapshot {
	status := s.GetStatusInfo()
	depth := len(s.taskIdChan)
	cap := s.TaskIdChanLength
	usage := "0%"
	if cap > 0 {
		usage = fmt.Sprintf("%d%%", depth*100/cap)
	}
	return MetricsSnapshot{
		Theme:            s.Theme,
		QueueDepth:       depth,
		QueueCapacity:    cap,
		QueueUsage:       usage,
		Status:           status,
		RetryCount:       status.RetryCount,
		PanicCount:       status.PanicCount,
		QueueDropCount:   status.QueueDropCount,
		HeartbeatDead:    status.HeartbeatDead,
		DBErrorCount:     status.DBErrorCount,
		WorkerScaleUp:    status.WorkerScaleUp,
		WorkerScaleDown:  status.WorkerScaleDown,
		AlarmFiredCount:  status.AlarmFired,
		AlarmSuppressed:  status.AlarmSuppressed,
		LastSnapshotTime: time.Now().UnixMilli(),
	}
}

// markEnqueued 记录任务被推入内存队列的时间戳，用于后续计算 queue 等待耗时。
func (s *Scheduler) markEnqueued(taskId int64) {
	s.enqueueAtMu.Lock()
	s.enqueueAt[taskId] = time.Now()
	s.enqueueAtMu.Unlock()
}

// popEnqueueAt 取出并删除任务的入队时间戳。
func (s *Scheduler) popEnqueueAt(taskId int64) (time.Time, bool) {
	s.enqueueAtMu.Lock()
	defer s.enqueueAtMu.Unlock()
	t, ok := s.enqueueAt[taskId]
	if ok {
		delete(s.enqueueAt, taskId)
	}
	return t, ok
}

// fireAlarm 统一告警入口；启用 throttle 时会去重，否则直接发送。
func (s *Scheduler) fireAlarm(title, content string) {
	if s.alarmThrottle != nil {
		// AlarmThrottle 内部已自行判断是否真实发送，并在内部维护 fired/suppressed 全局指标。
		// 这里再额外做本地 counter 记录，方便 admin 接口直接读取。
		before := GetMetrics().AlarmFired
		_ = before
		s.alarmThrottle.Send(title, content)
		// 简化：每次调用都视为一次"投递尝试"，由 throttle 内部决定 fired/suppressed
		// 这里通过比较前后无法精确归因到本 scheduler，因此采取简化策略：
		// throttle.Send 已增加全局 metric；本地 counter 在 send 真实发送时由 SendSystemAlarm 直接调用计数。
		s.counters.alarmFired.Add(1) // 视为一次告警事件（包含被抑制的）
		return
	}
	s.counters.alarmFired.Add(1)
	GetMetrics().AlarmFired.Add(context.Background(), 1)
	_ = gaia.SendSystemAlarm(title, content)
}

// observeGauges 是 OTel observable gauge 异步采样回调，注册到全局 metric callback。
func (s *Scheduler) observeGauges(observer otelmetric.Observer) {
	defer func() { recover() }()

	if !s.IsRunning() {
		return
	}

	m := GetMetrics()
	attrs := otelmetric.WithAttributes(MetricLabel.Theme.String(s.Theme))

	observer.ObserveInt64(m.QueueDepth, int64(len(s.taskIdChan)), attrs)
	observer.ObserveInt64(m.QueueCapacity, int64(s.TaskIdChanLength), attrs)
	observer.ObserveInt64(m.WorkerTotal, int64(atomic.LoadInt32(&s.statusInfo.AllWorkers)), attrs)
	observer.ObserveInt64(m.WorkerRunning, int64(atomic.LoadInt32(&s.statusInfo.RunningWorkers)), attrs)
}

// sendComponentNotify 发送调度器生命周期通知
func (s *Scheduler) sendComponentNotify(action string) {
	defer gaia.CatchPanic()
	snap := s.SnapshotMetrics()
	status := s.GetStatusInfo()
	if err := gaia.SendLifecycleNotify(gaia.LifecycleNotifyInfo{
		Kind:      "component",
		Name:      "AsyncTask",
		Component: "AsyncTask",
		Action:    action,
		Status:    lifecycleStatus(action),
		Fields: map[string]any{
			"theme":          s.Theme,
			"running":        s.IsRunning(),
			"worker_total":   status.AllWorkers,
			"worker_running": status.RunningWorkers,
			"queue_depth":    snap.QueueDepth,
			"success_total":  status.ExecSuccess,
			"failed_total":   status.ExecFails,
			"retry_total":    snap.RetryCount,
			"panic_total":    snap.PanicCount,
		},
	}); err != nil {
		s.Logger.WarnF("发送%s通知失败: %s", action, err.Error())
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
