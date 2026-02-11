// Package asynctask 包注释
// @author wanlizhan
// @created 2024/5/6
package asynctask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"time"
)

// Executor 执行器
type Executor struct {
	TaskId       int64
	TaskInfo     TaskModel
	PreHandler   PreHandlerFunc
	PostHandler  PostHandlerFunc
	AlarmHandler AlarmHandlerFunc

	Ctx    context.Context
	Logger *logImpl.DefaultLogger
}

// NewExecutor 获取一个执行器
func NewExecutor(taskId int64, theme string) *Executor {
	return &Executor{TaskId: taskId, Logger: logImpl.NewDefaultLogger().SetTitle(theme + "_AsyncTaskExecutor")}
}

func (e *Executor) WithCtx(ctx context.Context) *Executor {
	e.Ctx = ctx
	return e
}

// WithPreHandler 执行器新增前置处理器
func (e *Executor) WithPreHandler(handler PreHandlerFunc) *Executor {
	e.PreHandler = handler
	return e
}

// WithPostHandler 执行器新增后置处理器
func (e *Executor) WithPostHandler(handler PostHandlerFunc) *Executor {
	e.PostHandler = handler
	return e
}

// WithAlarmHandler 执行器新增告警处理器
func (e *Executor) WithAlarmHandler(handler AlarmHandlerFunc) *Executor {
	e.AlarmHandler = handler
	return e
}

// Run 执行器执行任务
func (e *Executor) Run() bool {
	gaia.BuildContextTrace()
	now := time.Now()
	//业务执行之前的失败，是可以重试的
	model, err := getTaskById(e.TaskId, e.Ctx)
	if err != nil {
		msg := fmt.Sprintf("获取任务信息%d失败:%s", e.TaskId, err.Error())
		e.Logger.Error(msg)
		updateTaskWait(e.TaskId, msg, now, e.Ctx)
		return false
	}
	if model.Id == 0 {
		msg := fmt.Sprintf("获取任务%d信息失败", e.TaskId)
		e.Logger.Error(msg)
		updateTaskWait(e.TaskId, msg, now, e.Ctx)
		return false
	}

	e.TaskInfo = model

	if err := e.preRun(); err != nil {
		msg := fmt.Sprintf("预处理错误:%s", err.Error())
		e.Logger.Error(msg)
		updateTaskFailed(e.TaskInfo.Id, msg, now, e.Ctx)
		return false
	}

	if res, err := e.run(); err != nil {
		msg := fmt.Sprintf("执行错误:%s", err.Error())
		e.Logger.Error(msg)
		updateTaskFailed(e.TaskInfo.Id, msg, now, e.Ctx)
		return false
	} else {
		marshal, err := json.Marshal(res)
		if err != nil {
			msg := fmt.Sprintf("结果序列化错误:%s", err.Error())
			e.Logger.Error(msg)
			updateTaskFailed(e.TaskInfo.Id, msg, now, e.Ctx)
			return false
		}
		if e.TaskInfo.MaxRetryTime >= e.TaskInfo.RetryTime+1 {
			updateTaskRetry(e.TaskInfo, string(marshal), now, e.Ctx)
		} else {
			updateTaskSuccess(e.TaskInfo.Id, string(marshal), now, e.Ctx)
		}
	}

	if err := e.postRun(); err != nil {
		msg := fmt.Sprintf("后处理错误:%s", err.Error())
		e.Logger.Error(msg)
		updateTaskFailed(e.TaskInfo.Id, msg, now, e.Ctx)
		return false
	}
	gaia.RemoveContextTrace()
	return true
}

func (e *Executor) preRun() (err error) {
	if e.PreHandler == nil {
		e.Logger.InfoF("TaskId:%d无前置任务", e.TaskInfo.Id)
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("PreHandlerPanic:" + gaia.PanicLog(r))
		}
	}()

	return e.PreHandler()
}

func (e *Executor) postRun() (err error) {
	if e.PostHandler == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("PostHandlerPanic:" + gaia.PanicLog(r))
		}
	}()

	return e.PostHandler(e.TaskInfo.Id)
}

func (e *Executor) run() (res any, err error) {
	//开始链接写入心跳信息
	if err = InsertOrUpdateHeartBeat(e.TaskInfo.Id); err != nil {
		e.Logger.Error(err.Error())
	}
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("RunPanic:" + gaia.PanicLog(r))
		}
	}()
	//这个任务要开始心跳连接
	go func() {
		defer gaia.CatchPanic()
		e.heartBeat(ctx)
	}()
	proxy := gaia.GetProxy(e.TaskInfo.SystemName, e.TaskInfo.ServiceName)
	if proxy == nil {
		return nil, fmt.Errorf("[%s-%s]获取Proxy为Nil", e.TaskInfo.SystemName, e.TaskInfo.ServiceName)
	}

	return gaia.CallMethodWithArgs(proxy, e.TaskInfo.MethodName, []byte(e.TaskInfo.Arg))
}

func (e *Executor) heartBeat(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second * 5):
			if err := InsertOrUpdateHeartBeat(e.TaskInfo.Id); err != nil {
				e.Logger.Error(err.Error())
			}
		}
	}
}
