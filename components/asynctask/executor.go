// Package asynctask 包注释
// @author wanlizhan
// @created 2024/5/6
package asynctask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

const (
	ExecutorTaskIdCtxKey      = "ExecutorTaskIdCtxKey"
	ExecutorTaskStatusCtxKey  = "ExecutorTaskStatusCtxKey"
	ExecutorTaskSuccessCtxKey = "ExecutorTaskSuccessCtxKey"
	ExecutorTaskErrMsgCtxKey  = "ExecutorTaskErrMsgCtxKey"
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
	defer gaia.RemoveContextTrace()
	tc := gaia.NewContextTrace()
	if err := tc.SetKvData(ExecutorTaskIdCtxKey, e.TaskId); err != nil {
		e.Logger.Error(err.Error())
	}

	now := time.Now()
	ok := false
	finalStatus := TaskStatusFailed.String()
	finalErrMsg := ""
	//业务执行之前的失败，是可以重试的
	model, err := getTaskById(e.TaskId, e.Ctx)
	if err != nil {
		msg := fmt.Sprintf("[%s-%s-%s]获取任务信息%d失败:%s", e.TaskInfo.SystemName, e.TaskInfo.ServiceName,
			e.TaskInfo.MethodName, e.TaskId,
			err.Error())
		e.Logger.Error(msg)
		e.recordDBErr("get_task")
		updateTaskWait(e.TaskId, msg, now, e.Ctx)
		return false
	}
	if model.Id == 0 {
		msg := fmt.Sprintf("[%s-%s-%s]获取任务%d信息失败", e.TaskInfo.SystemName, e.TaskInfo.ServiceName,
			e.TaskInfo.MethodName, e.TaskId)
		e.Logger.Error(msg)
		updateTaskWait(e.TaskId, msg, now, e.Ctx)
		return false
	}

	e.TaskInfo = model
	// 触发 OnTaskStart Hook
	e.fireStartHook(now)

	if err := e.preRun(); err != nil {
		msg := fmt.Sprintf("[%s-%s-%s]预处理错误:%s", e.TaskInfo.SystemName, e.TaskInfo.ServiceName,
			e.TaskInfo.MethodName, err.Error())
		e.Logger.Error(msg)
		updateTaskFailed(e.TaskInfo.Id, msg, now, e.Ctx)
		e.recordPostExec(now, TaskStatusFailed.String(), msg, false)
		return false
	}

	if res, err := e.run(); err != nil {
		msg := fmt.Sprintf("[%s-%s-%s]执行错误:%s", e.TaskInfo.SystemName, e.TaskInfo.ServiceName,
			e.TaskInfo.MethodName, err.Error())
		e.Logger.Error(msg)
		finalErrMsg = msg
		isPanic := strings.HasPrefix(err.Error(), "RunPanic:")
		if isPanic {
			recordPanic(e.Ctx, e.theme(), "run")
		}
		// 失败时若仍有重试次数，则置为 Retry，等待下一轮调度；否则置为 Failed
		if e.TaskInfo.MaxRetryTime >= e.TaskInfo.RetryTime+1 {
			finalStatus = TaskStatusRetry.String()
			updateTaskRetry(e.TaskInfo, msg, now, e.Ctx)
			recordRetry(e.Ctx, e.theme())
			e.recordPostExec(now, TaskStatusRetry.String(), msg, isPanic)
		} else {
			finalStatus = TaskStatusFailed.String()
			updateTaskFailed(e.TaskInfo.Id, msg, now, e.Ctx)
			e.recordPostExec(now, TaskStatusFailed.String(), msg, isPanic)
		}
	} else {
		marshal, err := json.Marshal(res)
		if err != nil {
			msg := fmt.Sprintf("[%s-%s-%s]结果序列化错误:%s", e.TaskInfo.SystemName, e.TaskInfo.ServiceName,
				e.TaskInfo.MethodName, err.Error())
			e.Logger.Error(msg)
			finalErrMsg = msg
			finalStatus = TaskStatusFailed.String()
			updateTaskFailed(e.TaskInfo.Id, msg, now, e.Ctx)
			e.recordPostExec(now, TaskStatusFailed.String(), msg, false)
		} else {
			// 业务成功：直接置为 Success（不再因 MaxRetryTime > 0 而被错误地置为 Retry）
			ok = true
			finalStatus = TaskStatusSuccess.String()
			updateTaskSuccess(e.TaskInfo.Id, string(marshal), now, e.Ctx)
			e.recordPostExec(now, TaskStatusSuccess.String(), "", false)
		}
	}

	if err := tc.SetKvData(ExecutorTaskStatusCtxKey, finalStatus); err != nil {
		e.Logger.Error(err.Error())
	}
	if err := tc.SetKvData(ExecutorTaskSuccessCtxKey, ok); err != nil {
		e.Logger.Error(err.Error())
	}
	if finalErrMsg != "" {
		if err := tc.SetKvData(ExecutorTaskErrMsgCtxKey, finalErrMsg); err != nil {
			e.Logger.Error(err.Error())
		}
	}
	if err := e.postRun(); err != nil {
		msg := fmt.Sprintf("[%s-%s-%s]后处理错误:%s", e.TaskInfo.SystemName, e.TaskInfo.ServiceName,
			e.TaskInfo.MethodName, err.Error())
		e.Logger.Error(msg)
	}
	e.Logger.InfoF("[%s-%s-%s]执行完成", e.TaskInfo.SystemName, e.TaskInfo.ServiceName, e.TaskInfo.MethodName)

	return ok
}

// theme 返回当前任务所属的 scheduler theme（asynctask 中等同 SystemName）。
func (e *Executor) theme() string {
	return e.TaskInfo.SystemName
}

func (e *Executor) recordDBErr(op string) {
	recordDBError(e.Ctx, e.theme(), op)
	if sch := GetScheduler(e.theme()); sch != nil {
		sch.counters.dbError.Add(1)
	}
}

// fireStartHook 在执行业务函数前触发 OnTaskStart 钩子，并记录 wait 时延。
func (e *Executor) fireStartHook(start time.Time) {
	waitMs := int64(0)
	if !e.TaskInfo.CreateAt.IsZero() && e.TaskInfo.RetryTime == 0 {
		waitMs = start.Sub(e.TaskInfo.CreateAt).Milliseconds()
		if waitMs < 0 {
			waitMs = 0
		}
		recordWaitDuration(e.Ctx, e.theme(), waitMs)
	}

	ev := TaskEvent{
		Theme:       e.theme(),
		TaskId:      e.TaskInfo.Id,
		TaskName:    e.TaskInfo.TaskName,
		ServiceName: e.TaskInfo.ServiceName,
		MethodName:  e.TaskInfo.MethodName,
		Status:      TaskStatusRunning.String(),
		StartTime:   start,
		WaitMillis:  waitMs,
		RetryTime:   e.TaskInfo.RetryTime,
	}

	var schedulerHook TaskHook
	if sch := GetScheduler(e.theme()); sch != nil {
		schedulerHook = sch.Hook
	}
	fireHook(e.Ctx, "start", ev, schedulerHook)

	// 推 AsyncTaskLog（独立 ES index：async_task_log，phase=run）
	e.emitTaskLog("run", start, time.Time{}, "", false)
}

// recordPostExec 记录耗时直方图、状态计数，并触发 OnTaskFinish/OnTaskPanic 钩子。
func (e *Executor) recordPostExec(start time.Time, status, errMsg string, isPanic bool) {
	end := time.Now()
	durMs := end.Sub(start).Milliseconds()

	recordExecDuration(e.Ctx, e.theme(), status, durMs)

	if isPanic {
		if sch := GetScheduler(e.theme()); sch != nil {
			sch.counters.panic.Add(1)
		}
	}

	ev := TaskEvent{
		Theme:       e.theme(),
		TaskId:      e.TaskInfo.Id,
		TaskName:    e.TaskInfo.TaskName,
		ServiceName: e.TaskInfo.ServiceName,
		MethodName:  e.TaskInfo.MethodName,
		Status:      status,
		StartTime:   start,
		EndTime:     end,
		Duration:    durMs,
		RetryTime:   e.TaskInfo.RetryTime,
		ErrMsg:      errMsg,
		IsPanic:     isPanic,
	}

	var schedulerHook TaskHook
	if sch := GetScheduler(e.theme()); sch != nil {
		schedulerHook = sch.Hook
	}

	if isPanic {
		fireHook(e.Ctx, "panic", ev, schedulerHook)
	}
	fireHook(e.Ctx, "finish", ev, schedulerHook)

	// 推 AsyncTaskLog（独立 ES index：async_task_log）
	e.emitTaskLog(mapAsyncTaskStatusToPhase(status, e.TaskInfo), start, end, errMsg, isPanic)
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
		e.Logger.InfoF("TaskId:%d无后置任务", e.TaskInfo.Id)
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("PostHandlerPanic:" + gaia.PanicLog(r))
		}
	}()

	return e.PostHandler()
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

	return gaia.CallMethodWithJSONArgsContext(e.Ctx, proxy, e.TaskInfo.MethodName, []byte(e.TaskInfo.Arg))
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
