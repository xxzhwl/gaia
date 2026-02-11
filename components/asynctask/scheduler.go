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

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/tracer"
	"github.com/xxzhwl/gaia/gexit"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
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

// PreHandlerFunc 前置处理器
type PreHandlerFunc func() error

// PostHandlerFunc 后置处理器
type PostHandlerFunc func(taskId int64) error

// AlarmHandlerFunc 告警处理器
type AlarmHandlerFunc func(taskId int64, message string) error

// SchedulerMap 调度中心
var SchedulerMap = make(map[string]*Scheduler)

var locker sync.RWMutex

// Scheduler 调度器
type Scheduler struct {
	Theme string

	WorkerNum         int           //工作协程总个数
	ScanTaskInterval  time.Duration //扫描DB任务间隔
	HeartBeatInterval time.Duration //心跳间隔
	TaskIdChanLength  int           //任务队列长度
	ScanTaskNum       int           //每次扫描DB任务数

	PreHandler   PreHandlerFunc   //前置动作
	PostHandler  PostHandlerFunc  //后置动作
	AlarmHandler AlarmHandlerFunc //告警动作

	tracer trace.Tracer

	taskIdChan chan int64
	startFlag  int32
	statusInfo SchedulerStatusInfo
	//workers    map[string]*Worker //工作协程

	inQueueTaskIds   map[int64]struct{} //队列中的任务ID
	inQueueTaskIdsRw sync.RWMutex       //用于操作 _taskIdsInChan 的读写锁

	//lastRemoveWorkerTime time.Time //最后一次移除worker的时间
	workerLastRunTime  map[string]time.Time //最后一次移除worker的时间
	lastRemoveWorkerRw sync.RWMutex

	l sync.RWMutex

	Logger *logImpl.DefaultLogger

	exitContext context.Context
}

// NewScheduler 获取一个调度器
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

// GetScheduler 根据theme获取调度器
func GetScheduler(theme string) *Scheduler {
	locker.RLock()
	defer locker.RUnlock()
	if v, ok := SchedulerMap[theme]; ok {
		return v
	}
	return nil
}

// Start 启动扫描任务
func (s *Scheduler) Start(ctx context.Context) {
	tracer.SetupTracer(ctx, "asyncTask_"+s.Theme)
	s.tracer = otel.Tracer("asyncTask_" + s.Theme)

	if atomic.SwapInt32(&s.startFlag, 1) != 0 {
		return
	}

	wg := sync.WaitGroup{}

	//1.开始扫描任务入队
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.scanTasks()
	}()

	//2.启动工作协程
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.wakeUpWorker(int32(10))
	}()

	//3.监控队列执行情况
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.monit()
	}()

	//4.心跳检测
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.heartBeat()
	}()
	wg.Wait()
}

// ReceiveTask 接受任务
func (s *Scheduler) ReceiveTask(task TaskBaseInfo) (model TaskModel, err error) {
	return AddTask(task, s.Theme, context.Background())
}

func (s *Scheduler) scanTasks() {
	for {

		select {
		case <-s.exitContext.Done():
			gaia.InfoF("Received exit signal,AsyncTask Scans shutting down")
			return

		case <-time.After(s.ScanTaskInterval):
			start, span := s.tracer.Start(context.Background(), "ScanTasks")
			s.statusInfo.Scans++
			ids, _, err := findNeedRunTaskIds(s.ScanTaskNum, s.Theme, start)
			if err != nil {
				s.Logger.Error("扫描需要运行的任务Id列表失败:" + err.Error())
				continue
			}
			span.End()
			if len(ids) != 0 {
				needWorkers := int32(len(ids)) - (atomic.LoadInt32(&s.statusInfo.AllWorkers) - atomic.LoadInt32(&s.statusInfo.
					RunningWorkers))
				s.wakeUpWorker(needWorkers)

				newIds := make([]int64, 0)
				for _, id := range ids {
					//允许入队才可以
					ok := s.allowInQueue(id)
					if ok {
						//入队抢占成功，入队
						select {
						case s.taskIdChan <- id:
							newIds = append(newIds, id)
						default:
							//队列已满，删除标记
							s.deleteInQueue(id)
						}
					}
					atomic.AddInt32(&s.statusInfo.PushTasks, 1)
				}
				s.Logger.InfoF("扫描次数:%d,扫描到待运行的任务:%v", s.statusInfo.Scans, newIds)
			}
		}

		//start, span := s.tracer.Start(context.Background(), "ScanTasks")
		//s.statusInfo.Scans++
		//ids, needContinue, err := findNeedRunTaskIds(s.ScanTaskNum, s.Theme, start)
		//if err != nil {
		//	s.Logger.Error("扫描需要运行的任务Id列表失败:" + err.Error())
		//	continue
		//}
		//span.End()
		//if len(ids) != 0 {
		//	needWorkers := int32(len(ids)) - (atomic.LoadInt32(&s.statusInfo.AllWorkers) - atomic.LoadInt32(&s.statusInfo.
		//		RunningWorkers))
		//	s.wakeUpWorker(needWorkers)
		//
		//	newIds := make([]int64, 0)
		//	for _, id := range ids {
		//		//允许入队才可以
		//		ok := s.allowInQueue(id)
		//		if ok {
		//			//入队抢占成功，入队
		//			s.taskIdChan <- id
		//			newIds = append(newIds, id)
		//		}
		//		atomic.AddInt32(&s.statusInfo.PushTasks, 1)
		//	}
		//	s.Logger.InfoF("扫描次数:%d,扫描到待运行的任务:%v", s.statusInfo.Scans, newIds)
		//}
		//if !needContinue {
		//	time.Sleep(s.ScanTaskInterval)
		//} else {
		//	time.Sleep(50 * time.Millisecond)
		//}
	}
}

func (s *Scheduler) allowInQueue(taskId int64) bool {
	s.inQueueTaskIdsRw.Lock()
	defer s.inQueueTaskIdsRw.Unlock()
	//在就不进去
	if _, ok := s.inQueueTaskIds[taskId]; ok {
		return false
	}
	//不在就进去
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
		case <-time.After(5 * time.Second):
			if float64(len(s.taskIdChan)/s.TaskIdChanLength) > 0.8 {
				s.Logger.WarnF("队列将满,%d---%d", len(s.taskIdChan), s.TaskIdChanLength)
				gaia.SendSystemAlarm(fmt.Sprintf("TaskMgr:%s队列将满", s.Theme),
					fmt.Sprintf("当前任务队列长度%d-任务队列总长度%d", len(s.taskIdChan), s.TaskIdChanLength))
			}

			pushTasks := atomic.LoadInt32(&s.statusInfo.PushTasks)
			pullTasks := atomic.LoadInt32(&s.statusInfo.PullTasks)
			execTasks := atomic.LoadInt32(&s.statusInfo.ExecTasks)
			successTasks := atomic.LoadInt32(&s.statusInfo.ExecSuccess)
			failTasks := atomic.LoadInt32(&s.statusInfo.ExecFails)
			runningWorkers := atomic.LoadInt32(&s.statusInfo.RunningWorkers)
			allWorkers := atomic.LoadInt32(&s.statusInfo.AllWorkers)

			s.Logger.InfoF("队列任务数%d,入队任务数:%d，拉取任务数:%d,执行任务数:%d,成功任务数:%d,失败任务数:%d,运行中workers:%d,全部workers:%d",
				len(s.taskIdChan), pushTasks, pullTasks, execTasks, successTasks, failTasks, runningWorkers, allWorkers)

			// 输出性能指标
			s.logPerformanceMetrics()
		}
	}
}

// logPerformanceMetrics 输出性能指标
func (s *Scheduler) logPerformanceMetrics() {
	// 获取最近执行的任务统计
	ctx := gaia.NewContextTrace().GetParentCtx()
	stats, err := GetTaskExecStats(s.Theme, ctx)
	if err != nil {
		s.Logger.ErrorF("获取任务执行统计失败:%s", err.Error())
		return
	}

	if stats.TotalTasks > 0 {
		s.Logger.InfoF("最近任务统计-总数:%d,平均执行时间:%dms,平均等待时间:%dms,P99执行时间:%dms,P99等待时间:%dms",
			stats.TotalTasks,
			stats.AvgExecTime,
			stats.AvgWaitTime,
			stats.P99ExecTime,
			stats.P99WaitTime,
		)
	}
}

// TaskQuickQueue 任务快速的直接进入处理队列，无缝处理
// 适用任务接收完成后，立即处理的场景
func (s *Scheduler) TaskQuickQueue(taskId int64) {
	//以异步的方式尝试快速入队，即使入队失败，也不影响后续的处理，通过常规扫描的方式只是会慢一点
	//通过提高异步队列的缓冲长度(ChSize)可以提供快速入队的成功率，默认设置为 2000
	go func() {
		if !s.allowInQueue(taskId) {
			return
		}
		select {
		case s.taskIdChan <- taskId:
			atomic.AddInt32(&s.statusInfo.PushTasks, 1)

		case <-time.After(100 * time.Millisecond):
			//最多等待100ms
			s.deleteInQueue(taskId)
			return
		}
	}()

}

// GetStatusInfo 获取某个scheduler的状态
func (s *Scheduler) GetStatusInfo() SchedulerStatusInfo {
	return s.statusInfo
}

// GetStatusInfo 根据Theme获取状态
func GetStatusInfo(theme string) SchedulerStatusInfo {
	if v, ok := SchedulerMap[theme]; ok {
		return v.statusInfo
	}
	return SchedulerStatusInfo{}
}
