// Package logImpl 注释
// @author wanlizhan
// @created 2026/6/1
//
// AsyncTaskLog 模型：异步任务日志（gaia/components/asynctask）。
// 与 JobLog 的区别：异步任务由"事件触发"（业务投递），而 Job 由"时间触发"。
// 拆开两个 LogType 让告警规则可以针对消费失败率（async）和漏跑/超时（job）分别定。
package logImpl

// AsyncTaskLogModel 完整的异步任务日志 ES 文档。
type AsyncTaskLogModel struct {
	LogModel
	AsyncTaskLogBaseModel
}

// AsyncTaskLogBaseModel 异步任务专属字段。
//
// Phase 取值：
//   - "enqueue"  投递入队
//   - "run"      worker 拉到任务开始执行
//   - "success"  成功结束
//   - "fail"     执行失败（已计入 RetryCount）
//   - "retry"    准备重试
//   - "drop"     超过最大重试次数被丢弃 / 进入死信
//
// Payload 仅在 enqueue / fail 时建议带；高频 run/success 不建议写入以免 ES 文档膨胀。
type AsyncTaskLogBaseModel struct {
	TaskName       string  `json:"task_name"`             // 任务名（路由 key）
	TaskId         string  `json:"task_id"`               // 单次任务实例 id（重试共享同一个）
	Phase          string  `json:"phase"`                 // 见 doc 注释
	RetryCount     int     `json:"retry_count,omitempty"` // 当前是第几次重试（首次为 0）
	MaxRetry       int     `json:"max_retry,omitempty"`   // 配置的最大重试次数
	Queue          string  `json:"queue,omitempty"`       // 队列名（多队列场景下区分用）
	Payload        string  `json:"payload,omitempty"`     // JSON 化后的入参
	Result         string  `json:"result,omitempty"`      // JSON 化后的结果（success 时可选记录）
	StartTime      string  `json:"start_time,omitempty"`
	EndTime        string  `json:"end_time,omitempty"`
	Duration       float64 `json:"duration,omitempty"` // 毫秒；enqueue/retry 阶段可为 0
	StartTimeStamp int64   `json:"start_time_stamp,omitempty"`
	EndTimeStamp   int64   `json:"end_time_stamp,omitempty"`
	Err            string  `json:"err,omitempty"`
}
