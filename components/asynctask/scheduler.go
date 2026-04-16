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
	TaskIdChanLength  int
	ScanTaskNum       int

	PreHandler   PreHandlerFunc
	PostHandler  PostHandlerFunc
	AlarmHandler AlarmHandlerFunc

	tracer trace.Tracer

	taskIdChan chan int64
	startFlag  int32
	running    int32
	stopping   int32
	statusInfo SchedulerStatusInfo

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
	if s.TaskIdChanLength <= 0 {
		s.TaskIdChanLength = DefaultTaskIdChanLength
	}
	if s.ScanTaskNum <= 0 || s.ScanTaskNum > MaxScanTaskNum {
		s.ScanTaskNum = DefaultScanTaskNum
	}
	s.taskIdChan = make(chan int64, s.TaskIdChanLength)
	s.inQueueTaskIds = make(map[int64]struct{})
	s.workerLastRunTime = make(map[string]time.Time)
	SchedulerMap[s.Theme] = s
	return s
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

	s.stopContext, s.stopCancel = context.WithCancel(context.Background())

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

	s.Logger.Info("Scheduler stopped completely")
}

func (s *Scheduler) drainTaskQueue() {
	s.inQueueTaskIdsRw.Lock()
	defer s.inQueueTaskIdsRw.Unlock()

	taskIds := make([]int64, 0)
	for {
		select {
		case taskId := <-s.taskIdChan:
			taskIds = append(taskIds, taskId)
		default:
			goto done
		}
	}
done:

	if len(taskIds) == 0 {
		s.Logger.Info("Task queue is empty")
		return
	}

	s.Logger.InfoF("Draining %d tasks from queue, resetting to Wait status", len(taskIds))

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

	for _, id := range taskIds {
		delete(s.inQueueTaskIds, id)
	}

	s.Logger.InfoF("Successfully drained %d tasks from queue", len(taskIds))
}

func (s *Scheduler) Resume() {
	if atomic.CompareAndSwapInt32(&s.stopping, 1, 0) {
		atomic.StoreInt32(&s.running, 1)
		s.stopContext, s.stopCancel = context.WithCancel(context.Background())
		s.Logger.Info("Scheduler resumed")

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
			scans := atomic.AddInt64(&s.statusInfo.Scans, 1)
			ids, _, err := findNeedRunTaskIds(s.ScanTaskNum, s.Theme, start)
			if err != nil {
				s.Logger.Error("扫描需要运行的任务Id列表失败:" + err.Error())
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
							newIds = append(newIds, id)
						default:
							s.deleteInQueue(id)
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
	for {
		select {
		case <-s.exitContext.Done():
			gaia.InfoF("Received exit signal,AsyncTask monitor shutting down")
			return
		case <-s.stopContext.Done():
			s.Logger.Info("Monitor received stop signal, shutting down")
			return
		case <-time.After(5 * time.Second):
			if float64(len(s.taskIdChan))/float64(s.TaskIdChanLength) > 0.8 {
				s.Logger.WarnF("队列将满,%d---%d", len(s.taskIdChan), s.TaskIdChanLength)
				gaia.SendSystemAlarm(fmt.Sprintf("TaskMgr:%s队列将满", s.Theme),
					fmt.Sprintf("当前任务队列长度%d-任务队列总长度%d", len(s.taskIdChan), s.TaskIdChanLength))
			}

			status := s.GetStatusInfo()

			s.Logger.InfoF("队列任务数%d,入队任务数:%d，拉取任务数:%d,执行任务数:%d,成功任务数:%d,失败任务数:%d,运行中workers:%d,全部workers:%d",
				len(s.taskIdChan), status.PushTasks, status.PullTasks, status.ExecTasks, status.ExecSuccess, status.ExecFails,
				status.RunningWorkers, status.AllWorkers)

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
			atomic.AddInt64(&s.statusInfo.PushTasks, 1)

		case <-time.After(100 * time.Millisecond):
			s.deleteInQueue(taskId)
			return
		}
	}()

}

func (s *Scheduler) GetStatusInfo() SchedulerStatusInfo {
	return SchedulerStatusInfo{
		Scans:          atomic.LoadInt64(&s.statusInfo.Scans),
		PushTasks:      atomic.LoadInt64(&s.statusInfo.PushTasks),
		PullTasks:      atomic.LoadInt64(&s.statusInfo.PullTasks),
		ExecTasks:      atomic.LoadInt64(&s.statusInfo.ExecTasks),
		ExecSuccess:    atomic.LoadInt64(&s.statusInfo.ExecSuccess),
		ExecFails:      atomic.LoadInt64(&s.statusInfo.ExecFails),
		RunningWorkers: atomic.LoadInt32(&s.statusInfo.RunningWorkers),
		AllWorkers:     atomic.LoadInt32(&s.statusInfo.AllWorkers),
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
