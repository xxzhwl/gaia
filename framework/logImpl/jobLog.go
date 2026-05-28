// Package logImpl 注释
// @author wanlizhan
// @created 2026/6/1
//
// JobLog 模型：定时任务日志（gaia/components/cron jobs）。
// 与 AsyncTaskLog 的核心差异：Job 由"时间触发"，关心的是
//  1. 是否按计划触发了（漏跑）
//  2. 单次执行是否在合理窗口内完成（超时）
//  3. 上一次还没跑完时是否被跳过（skip 防重）
//
// 因此字段里突出 cron_spec / skipped / next_fire_time。
package logImpl

// JobLogModel 完整的定时任务日志 ES 文档。
type JobLogModel struct {
	LogModel
	JobLogBaseModel
}

// JobLogBaseModel 定时任务专属字段。
//
// Phase 取值：
//   - "fire"     调度器到点触发
//   - "start"    任务函数实际开始执行
//   - "success"  正常结束
//   - "fail"     panic / 返回 error
//   - "skipped"  上一次未结束被跳过（防重叠运行）
//   - "miss"     watchdog 检测出来的"该跑没跑"（漏跑）
type JobLogBaseModel struct {
	JobName        string  `json:"job_name"`
	CronSpec       string  `json:"cron_spec,omitempty"`      // "0 */5 * * * *" / "@every 30s"
	Phase          string  `json:"phase"`                    // 见 doc 注释
	NextFireTime   string  `json:"next_fire_time,omitempty"` // 下次预定触发的可读时间
	StartTime      string  `json:"start_time,omitempty"`
	EndTime        string  `json:"end_time,omitempty"`
	Duration       float64 `json:"duration,omitempty"` // 毫秒
	StartTimeStamp int64   `json:"start_time_stamp,omitempty"`
	EndTimeStamp   int64   `json:"end_time_stamp,omitempty"`
	Err            string  `json:"err,omitempty"`
}
