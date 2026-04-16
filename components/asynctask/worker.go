// Package asynctask 注释
// @author wanlizhan
// @created 2024/4/29
package asynctask

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia"
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
	if s.IsStopping() {
		return
	}
	for count := 0; count < int(i); count++ {
		if atomic.LoadInt32(&s.statusInfo.AllWorkers)+1 > MaxWorkerNum {
			return
		}
		if s.IsStopping() {
			return
		}
		go func() {
			defer gaia.CatchPanic()
			defer gaia.RemoveContextTrace()
			defer func() {
				s.lastRemoveWorkerRw.Lock()
				delete(s.workerLastRunTime, gaia.GetGoRoutineId())
				s.lastRemoveWorkerRw.Unlock()
			}()
			gaia.BuildContextTrace()
			s.work()
		}()
	}
}

func (s *Scheduler) work() {
	s.Logger.Info("开始扩容")
	atomic.AddInt32(&s.statusInfo.AllWorkers, 1)
	defer func() {
		atomic.AddInt32(&s.statusInfo.AllWorkers, -1)
	}()

	for {
		select {
		case <-s.exitContext.Done():
			gaia.InfoF("Received exit signal,AsyncTask work shutting down")
			return
		case <-s.stopContext.Done():
			s.Logger.Info("Worker received stop signal, shutting down")
			return
		case taskId := <-s.taskIdChan:
			if s.IsStopping() {
				s.resetTaskToWait(taskId)
				continue
			}
			atomic.AddInt64(&s.statusInfo.PullTasks, 1)
			s.exec(taskId, context.Background())
		case <-time.After(time.Second * 5):
			if s.IsStopping() {
				return
			}
			if s.workerNeedExit() {
				s.Logger.InfoF("%s开始缩容", gaia.GetGoRoutineId())
				s.lastRemoveWorkerRw.Lock()
				delete(s.workerLastRunTime, gaia.GetGoRoutineId())
				s.lastRemoveWorkerRw.Unlock()
				return
			}
		}
	}
}

func (s *Scheduler) resetTaskToWait(taskId int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		s.Logger.ErrorF("Failed to connect database for resetting task %d: %s", taskId, err.Error())
		return
	}

	now := time.Now()
	err = db.GetGormDb().WithContext(ctx).Table(taskTable).
		Where("id = ? AND task_status = ?", taskId, TaskStatusRunning.String()).
		Updates(map[string]any{
			"task_status": TaskStatusWait.String(),
			"update_time": now,
		}).Error

	if err != nil {
		s.Logger.ErrorF("Failed to reset task %d status: %s", taskId, err.Error())
		return
	}

	s.deleteInQueue(taskId)
	s.Logger.InfoF("Task %d reset to Wait status during shutdown", taskId)
}

func (s *Scheduler) workerNeedExit() bool {
	allWorkers := atomic.LoadInt32(&s.statusInfo.AllWorkers)
	runningWorkers := atomic.LoadInt32(&s.statusInfo.RunningWorkers)

	if allWorkers <= 10 {
		return false
	}

	idleWorkers := allWorkers - runningWorkers
	if int32(len(s.taskIdChan)) < idleWorkers {
		s.lastRemoveWorkerRw.RLock()
		v, ok := s.workerLastRunTime[gaia.GetGoRoutineId()]
		s.lastRemoveWorkerRw.RUnlock()
		if ok {
			if !v.Add(time.Second * 10).After(time.Now()) {
				return true
			}
		}
	}
	return false
}

func (s *Scheduler) exec(taskId int64, ctx context.Context) {
	defer s.deleteInQueue(taskId)

	s.runningWorkers.Add(1)
	defer s.runningWorkers.Done()

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

	if s.IsStopping() {
		s.releaseTaskToWait(taskId)
		return
	}

	runCtx, span3 := s.tracer.Start(start, fmt.Sprintf("正式执行任务Id:%d", taskId))
	s.Logger.InfoF("%s开始执行task:%d", gaia.GetGoRoutineId(), taskId)
	s.lastRemoveWorkerRw.Lock()
	s.workerLastRunTime[gaia.GetGoRoutineId()] = time.Now()
	s.lastRemoveWorkerRw.Unlock()
	atomic.AddInt32(&s.statusInfo.RunningWorkers, 1)
	defer atomic.AddInt32(&s.statusInfo.RunningWorkers, -1)
	executor := NewExecutor(taskId, s.Theme).WithPreHandler(s.PreHandler).WithPostHandler(s.PostHandler).WithCtx(runCtx)
	res := executor.Run()
	span3.End()
	if res {
		atomic.AddInt64(&s.statusInfo.ExecSuccess, 1)
	} else {
		atomic.AddInt64(&s.statusInfo.ExecFails, 1)
	}
	atomic.AddInt64(&s.statusInfo.ExecTasks, 1)
}

func (s *Scheduler) releaseTaskToWait(taskId int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		s.Logger.ErrorF("Failed to connect database for releasing task %d: %s", taskId, err.Error())
		return
	}

	now := time.Now()
	err = db.GetGormDb().WithContext(ctx).Table(taskTable).
		Where("id = ? AND task_status = ?", taskId, TaskStatusRunning.String()).
		Updates(map[string]any{
			"task_status": TaskStatusWait.String(),
			"update_time": now,
		}).Error

	if err != nil {
		s.Logger.ErrorF("Failed to release task %d: %s", taskId, err.Error())
		return
	}

	s.Logger.InfoF("Task %d released to Wait status during shutdown", taskId)
}
