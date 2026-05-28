// Package jobs 注释
// @author wanlizhan
// @created 2026/6/1
//
// 结构化定时任务日志接入（写入独立 ES index: job_log）。
//
// 之所以单独成文件：
//  1. 不污染 cron_service.go / cron_hook.go 的核心调度逻辑；
//  2. 后续 ES mapping / 字段裁剪都集中在一处；
//  3. emitJobLog 失败永远不向上抛错——日志属于"可观测增强"，不能因为日志失败影响任务调度。
package jobs

import (
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

// mapJobStatusToPhase 把内部 status 字符串映射到 JobLogBaseModel.Phase 取值。
//   - 内部 status："success" / "failed" / "panic" / "timeout" / "skip"
//   - 远程 phase ："success" / "fail"   / "fail"  / "fail"    / "skipped"
//
// 之所以把 panic / timeout 都归到 fail：
//
//	ES 侧关心的是"运行结果"维度，is_panic / is_timeout 这种"失败原因"
//	建议通过 err 字段或独立的 dim/tag 区分，而不是再开一个 phase 值。
func mapJobStatusToPhase(status string) string {
	switch status {
	case "success":
		return "success"
	case "skip":
		return "skipped"
	case "failed", "panic", "timeout":
		return "fail"
	default:
		return status
	}
}

// emitJobLog 把一次 job 事件推到结构化日志。
//   - phase = "start"：start 已知，end/duration 留空
//   - phase = "success/fail/skipped"：start/end/duration 完整
//
// 日志级别策略：
//   - 成功类（start/success/skipped）→ Info
//   - 失败类（fail）→ Error
//
// logger 选择：cron_job 类型走 cronJobLogger，cron_hook 类型走 cronHookLogger。
// 这样本地日志文件里 title 也能区分，方便 grep。
func (r *RunJob) emitJobLog(j cronJob, phase string, start, end time.Time, errMsg string, isFailureLike bool) {
	defer func() { _ = recover() }()

	logger := r.cronJobLogger
	if j.JobType == CronHook {
		logger = r.cronHookLogger
	}
	if logger == nil {
		return
	}

	body := logImpl.JobLogBaseModel{
		JobName:        j.JobName,
		CronSpec:       j.CronExpr,
		Phase:          phase,
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		StartTimeStamp: start.UnixMilli(),
		Err:            errMsg,
	}
	if !end.IsZero() {
		body.EndTime = end.Format(gaia.DateTimeMillsFormat)
		body.EndTimeStamp = end.UnixMilli()
		body.Duration = float64(end.Sub(start).Milliseconds())
	}

	level := gaia.LogInfoLevel
	if isFailureLike || phase == "fail" {
		level = gaia.LogErrorLevel
	}

	content := j.JobName + " " + phase
	if errMsg != "" {
		content = content + " err=" + errMsg
	}
	logger.JobLogBody(level, content, body)
}
