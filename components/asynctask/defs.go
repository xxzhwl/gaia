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
	Scans     int64 //任务扫描次数
	PushTasks int64 //从DB中获取到可能可以执行的任务ID，并Push到队列中的任务数
	PullTasks int64 //worker从channel队列中获取的任务数

	ExecTasks   int64 //实际执行的任务数
	ExecSuccess int64 //执行成功数
	ExecFails   int64 //执行失败数 ExecSuccess + ExecFails = ExecTasks

	RunningWorkers int32 //当前运行中的worker
	AllWorkers     int32

	// 以下字段为 v2 新增的运行时计数（进程内累计），便于 admin 接口直接读取，不依赖 OTel exporter

	RetryCount      int64 // 任务进入重试状态的累计次数
	PanicCount      int64 // 任务执行 panic 的累计次数
	QueueDropCount  int64 // 因内存队列满而丢弃的累计次数
	HeartbeatDead   int64 // 心跳失活回收的累计任务数
	DBErrorCount    int64 // DB 操作错误累计次数
	WorkerScaleUp   int64 // worker 扩容累计次数
	WorkerScaleDown int64 // worker 缩容累计次数
	AlarmFired      int64 // 实际发送告警累计次数
	AlarmSuppressed int64 // 因去重抑制的告警累计次数
}
