// Package asynctask 注释
// @author wanlizhan
// @created 2024/4/29
package asynctask

import (
	"context"
	"fmt"
	"github.com/xxzhwl/gaia"
	"sync/atomic"
	"time"
)

type Worker struct {
	Id              string
	CurrentTaskId   int64
	Status          string
	ExecTaskNums    int64
	LastTaskEndTime time.Time

	sleepChan chan bool
}

func (s *Scheduler) wakeUpWorker(i int32) {
	for count := 0; count < int(i); count++ {
		if atomic.LoadInt32(&s.statusInfo.AllWorkers)+1 > MaxWorkerNum {
			return
		}
		go func() {
			defer gaia.CatchPanic()
			defer gaia.RemoveContextTrace()
			gaia.BuildContextTrace()
			s.work()
		}()
	}
}

func (s *Scheduler) work() {
	s.Logger.Info("开始扩容")
	atomic.AddInt32(&s.statusInfo.AllWorkers, 1)
	start, span := s.tracer.Start(context.Background(), "AsyncTaskWorker"+gaia.GetGoRoutineId())
	for {
		select {
		case taskId := <-s.taskIdChan:
			atomic.AddInt32(&s.statusInfo.PullTasks, 1)
			s.exec(taskId, start)
		case <-time.After(time.Second * 5):
			//五秒检查一次是否需要缩容
			if s.workerNeedExit() {
				s.Logger.InfoF("%s开始缩容", gaia.GetGoRoutineId())
				delete(s.workerLastRunTime, gaia.GetGoRoutineId())
				atomic.AddInt32(&s.statusInfo.AllWorkers, -1)
				span.End()
				return
			}
		}
	}
}

func (s *Scheduler) workerNeedExit() bool {
	//小于10个不缩容
	if atomic.LoadInt32(&s.statusInfo.AllWorkers) <= 10 {
		return false
	}
	//如果当前任务队列任务数/当前空闲中的worker<1且上次运行时间超过10秒，就可以缩容了
	s.lastRemoveWorkerRw.Lock()
	defer s.lastRemoveWorkerRw.Unlock()
	if int32(len(s.taskIdChan))/
		(atomic.LoadInt32(&s.statusInfo.AllWorkers)-atomic.LoadInt32(&s.statusInfo.RunningWorkers)) < 1 {
		if v, ok := s.workerLastRunTime[gaia.GetGoRoutineId()]; ok {
			if !v.Add(time.Second * 10).After(time.Now()) {
				return true
			}
		}
	}
	return false
}

func (s *Scheduler) exec(taskId int64, ctx context.Context) {
	defer s.deleteInQueue(taskId)
	start, span := s.tracer.Start(ctx, fmt.Sprintf("执行任务Id:%d", taskId))
	defer span.End()

	lockTaskCtx, span2 := s.tracer.Start(start, fmt.Sprintf("抢占任务Id:%d", taskId))
	flag, err := tryLockTask(taskId, lockTaskCtx)
	span2.End()
	if err != nil {
		s.Logger.ErrorF("尝试锁定当前任务%d失败:%s", taskId, err.Error())
		return
	}
	if !flag {
		return
	}
	//开始执行
	runCtx, span3 := s.tracer.Start(start, fmt.Sprintf("正式执行任务Id:%d", taskId))
	s.Logger.InfoF("%s开始执行task:%d", gaia.GetGoRoutineId(), taskId)
	s.lastRemoveWorkerRw.Lock()
	s.workerLastRunTime[gaia.GetGoRoutineId()] = time.Now()
	s.lastRemoveWorkerRw.Unlock()
	atomic.AddInt32(&s.statusInfo.RunningWorkers, 1)
	executor := NewExecutor(taskId, s.Theme).WithPreHandler(s.PreHandler).WithPostHandler(s.PostHandler).WithCtx(runCtx)
	res := executor.Run()
	span3.End()
	if res {
		atomic.AddInt32(&s.statusInfo.ExecSuccess, 1)
	} else {
		atomic.AddInt32(&s.statusInfo.ExecFails, 1)
	}
	atomic.AddInt32(&s.statusInfo.ExecTasks, 1)
	atomic.AddInt32(&s.statusInfo.RunningWorkers, -1)
}
