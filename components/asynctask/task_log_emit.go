// Package asynctask 注释
// @author wanlizhan: 2026/6/1
//
// 结构化异步任务日志接入（写入独立 ES index: async_task_log）。
//
// 之所以单独成文件：
//  1. 不污染 executor.go / task.go 主流程；
//  2. ES mapping 调整、字段裁剪都集中到这里；
//  3. emit 失败永远不向上抛错——日志属于"可观测增强"，不能因为日志失败影响任务调度。
//
// Phase 取值（与 logImpl.AsyncTaskLogBaseModel 注释保持一致）：
//   - "enqueue" 投递入队（AddTask 末尾）
//   - "run"     worker 拉到任务，开始执行（fireStartHook）
//   - "success" 成功结束
//   - "fail"    重试上限后仍失败 / 业务直接置 Failed
//   - "retry"   失败但仍有重试机会
//   - "drop"    超过最大重试被丢弃 / 进入死信（当前框架与 fail 合并，留接口）
package asynctask

import (
	"strconv"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

// mapAsyncTaskStatusToPhase 把 executor.recordPostExec 的 status 映射成 ES 侧 Phase。
//
//	内部 status         → 外部 phase
//	"Success"           → "success"
//	"Retry"             → "retry"
//	"Failed" 且没重试   → "drop"   （retry==maxRetry，没有再被拉起的机会）
//	"Failed" 还能重试   → "fail"   （理论上当前 executor 不走这里，留兼容）
//
// 之所以区分 fail / drop：监控告警侧关心"是否还会自愈"——能 retry 的允许短暂抖动，
// 但 drop 表示已经永久失败需要人工介入，告警阈值不一样。
func mapAsyncTaskStatusToPhase(status string, info TaskModel) string {
	switch status {
	case TaskStatusSuccess.String():
		return "success"
	case TaskStatusRetry.String():
		return "retry"
	case TaskStatusFailed.String():
		// 是否已经"用光重试"——用光则归为 drop。
		if info.MaxRetryTime > 0 && info.RetryTime+1 >= info.MaxRetryTime {
			return "drop"
		}
		return "fail"
	default:
		return status
	}
}

// emitEnqueueLog 投递入队事件。
//
// 注意：这里我们不知道 max_retry 是否合理（用户传什么写什么），不做任何校验。
// payload 写到 body 里方便溯源；过大的 payload 应在调用方裁剪。
func emitEnqueueLog(model TaskModel) {
	defer func() { _ = recover() }()

	logger := getEnqueueLogger(model.SystemName)
	if logger == nil {
		return
	}

	now := time.Now()
	body := logImpl.AsyncTaskLogBaseModel{
		TaskName:       displayTaskName(model),
		TaskId:         strconv.FormatInt(model.Id, 10),
		Phase:          "enqueue",
		MaxRetry:       model.MaxRetryTime,
		Queue:          model.SystemName,
		Payload:        model.Arg,
		StartTime:      now.Format(gaia.DateTimeMillsFormat),
		StartTimeStamp: now.UnixMilli(),
	}
	logger.AsyncTaskLogBody(gaia.LogInfoLevel, "enqueue "+body.TaskName, body)
}

// emitTaskLog 在 fireStartHook / recordPostExec 中调用，输出 run/success/fail/retry/drop。
func (e *Executor) emitTaskLog(phase string, start, end time.Time, errMsg string, isPanic bool) {
	defer func() { _ = recover() }()

	if e.Logger == nil {
		return
	}

	body := logImpl.AsyncTaskLogBaseModel{
		TaskName:       displayTaskName(e.TaskInfo),
		TaskId:         strconv.FormatInt(e.TaskInfo.Id, 10),
		Phase:          phase,
		RetryCount:     e.TaskInfo.RetryTime,
		MaxRetry:       e.TaskInfo.MaxRetryTime,
		Queue:          e.TaskInfo.SystemName,
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		StartTimeStamp: start.UnixMilli(),
		Err:            errMsg,
	}
	if !end.IsZero() {
		body.EndTime = end.Format(gaia.DateTimeMillsFormat)
		body.EndTimeStamp = end.UnixMilli()
		body.Duration = float64(end.Sub(start).Milliseconds())
	}
	// success 阶段保留结果（按需裁剪以免 ES 文档膨胀）；其他阶段不带。
	if phase == "success" {
		body.Result = e.TaskInfo.LastResult
	}

	level := gaia.LogInfoLevel
	switch phase {
	case "fail", "drop":
		level = gaia.LogErrorLevel
	case "retry":
		level = gaia.LogWarnLevel
	}
	if isPanic {
		level = gaia.LogErrorLevel
	}

	content := body.TaskName + " " + phase
	if errMsg != "" {
		content = content + " err=" + errMsg
	}
	e.Logger.AsyncTaskLogBody(level, content, body)
}

// displayTaskName 用 TaskName 优先，回退到 service.method，便于 ES 聚合时的稳定 key。
func displayTaskName(m TaskModel) string {
	if m.TaskName != "" {
		return m.TaskName
	}
	if m.ServiceName != "" || m.MethodName != "" {
		return m.ServiceName + "." + m.MethodName
	}
	return "unknown"
}

// getEnqueueLogger 给入队事件挑一个合适的 logger。
//   - 若该 theme 的 scheduler 已存在，复用其 Logger（title 一致）；
//   - 否则新建一个 title 为 "<theme>_AsyncTaskEnqueue" 的 logger。
//
// 这样 enqueue 和后续 run/success 的本地日志在同一个文件里，
// 排查时不用切多个文件。
func getEnqueueLogger(systemName string) *logImpl.DefaultLogger {
	if sch := GetScheduler(systemName); sch != nil && sch.Logger != nil {
		return sch.Logger
	}
	return logImpl.NewDefaultLogger().SetTitle(systemName + "_AsyncTaskEnqueue")
}
