// Package asynctask 注释
// @author wanlizhan
// @created 2024/4/29
package asynctask

// TaskStatus 定义任务状态
type TaskStatus uint8

// WorkerStatus 定义工作单元状态
type WorkerStatus uint8

const (
	TaskStatusWait    TaskStatus = iota //等待
	TaskStatusRunning                   //执行
	TaskStatusSuccess                   //执行成功
	TaskStatusFailed                    //执行失败
	TaskStatusRetry                     //需要重试
)
const (
	WorkerStatusSleep WorkerStatus = iota
	WorkerStatusRun
)

// String 输出任务状态
func (s TaskStatus) String() string {
	switch s {
	case TaskStatusWait:
		return "Wait"
	case TaskStatusRunning:
		return "Running"
	case TaskStatusSuccess:
		return "Success"
	case TaskStatusFailed:
		return "Failed"
	case TaskStatusRetry:
		return "Retry"
	default:
		return "Unknown"
	}
}

// String 输出工作状态
func (w WorkerStatus) String() string {
	switch w {
	case WorkerStatusSleep:
		return "Sleep"
	case WorkerStatusRun:
		return "Run"
	default:
		return "Unknown"
	}
}

// SchedulerOption 调度器选项
type SchedulerOption func(scheduler *Scheduler)

// SchedulerStatusInfo 调度器状态信息
type SchedulerStatusInfo struct {
	Scans     int32 //任务扫描次数
	PushTasks int32 //从DB中获取到可能可以执行的任务ID，并Push到队列中的任务数
	PullTasks int32 //worker从channel队列中获取的任务数

	ExecTasks   int32 //实际执行的任务数
	ExecSuccess int32 //执行成功数
	ExecFails   int32 //执行失败数 ExecSuccess + ExecFails = ExecTasks

	RunningWorkers int32 //当前运行中的worker
	AllWorkers     int32
}
