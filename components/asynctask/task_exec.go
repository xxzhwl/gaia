// Package asynctask 注释
// @author wanlizhan
// @created 2024/5/9
package asynctask

import (
	"context"
	"slices"
	"time"

	"github.com/xxzhwl/gaia"
)

// TaskExecModel 任务执行记录
type TaskExecModel struct {
	Id              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TaskId          int64     `gorm:"column:task_id;not null;default:0"`
	TaskStatus      string    `gorm:"column:task_status;size:32;not null;default:Wait"`
	LastResult      string    `gorm:"column:last_result;type:longtext"`
	LastErrMsg      string    `gorm:"column:last_err_msg;size:512;not null;default:''"`
	LastRunTime     time.Time `gorm:"column:last_run_time;index:idx_asynctask_exec_row_last_run_time"`
	LastRunEndTime  time.Time `gorm:"column:last_run_end_time"`
	LastRunDuration int64     `gorm:"column:last_run_duration;not null;default:0"`
	LogId           string    `gorm:"column:log_id;size:128;not null;default:''"`
	TaskName        string    `gorm:"column:task_name;->;-:migration"`
}

func (m *TaskExecModel) TableName() string {
	return "asynctask_exec_row"
}

// InsertTaskExecRow 插入任务执行记录
func InsertTaskExecRow(model TaskExecModel, ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}

	tx := db.GetGormDb().WithContext(ctx).Create(&model)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}

// TaskExecStats 任务执行统计（基于最近 5 分钟 exec_row）
type TaskExecStats struct {
	TotalTasks  int   `json:"total_tasks"`   // 总任务数
	SuccessNum  int   `json:"success_num"`   // 成功任务数
	FailedNum   int   `json:"failed_num"`    // 失败任务数
	SuccessRate int   `json:"success_rate"`  // 成功率（百分比，0-100）
	AvgExecTime int64 `json:"avg_exec_time"` // 平均执行时间（毫秒）
	P50ExecTime int64 `json:"p50_exec_time"` // P50 执行时间（毫秒）
	P95ExecTime int64 `json:"p95_exec_time"` // P95 执行时间（毫秒）
	P99ExecTime int64 `json:"p99_exec_time"` // P99 执行时间（毫秒）
	MaxExecTime int64 `json:"max_exec_time"` // 最大执行时间（毫秒）
	AvgWaitTime int64 `json:"-"`             // Deprecated: 等待时间不再计算
	P99WaitTime int64 `json:"-"`             // Deprecated: 等待时间不再计算
}

// GetTaskExecStats 获取最近的任务执行统计（最近5分钟）
func GetTaskExecStats(theme string, ctx context.Context) (*TaskExecStats, error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, err
	}

	// 查询最近5分钟的执行记录
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)

	var records []TaskExecModel
	tx := db.GetGormDb().WithContext(ctx).
		Table("asynctask_exec_row as exec_rows").
		Select("exec_rows.*").
		Joins("join asynctasks on asynctasks.id = exec_rows.task_id").
		Where("asynctasks.system_name = ?", theme).
		Where("exec_rows.last_run_time > ?", fiveMinutesAgo).
		Where("exec_rows.task_status IN ?", []string{TaskStatusSuccess.String(), TaskStatusFailed.String()}).
		Order("exec_rows.last_run_time DESC").
		Limit(1000).
		Find(&records)

	if tx.Error != nil {
		return nil, tx.Error
	}

	if len(records) == 0 {
		return &TaskExecStats{}, nil
	}

	stats := &TaskExecStats{
		TotalTasks: len(records),
	}

	var totalExecTime int64
	execTimes := make([]int64, 0, len(records))

	for _, r := range records {
		execTime := r.LastRunDuration
		totalExecTime += execTime
		execTimes = append(execTimes, execTime)

		switch r.TaskStatus {
		case TaskStatusSuccess.String():
			stats.SuccessNum++
		case TaskStatusFailed.String():
			stats.FailedNum++
		}
	}

	stats.AvgExecTime = totalExecTime / int64(len(records))
	stats.P50ExecTime = calculatePercentile(execTimes, 0.50)
	stats.P95ExecTime = calculatePercentile(execTimes, 0.95)
	stats.P99ExecTime = calculatePercentile(execTimes, 0.99)
	stats.MaxExecTime = calculatePercentile(execTimes, 1.0)
	if stats.TotalTasks > 0 {
		stats.SuccessRate = stats.SuccessNum * 100 / stats.TotalTasks
	}

	return stats, nil
}

// calculatePercentile 通用分位数（0~1.0），values 内部会被排序后修改。
func calculatePercentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}

	slices.Sort(values)
	index := int(float64(len(values)) * p)
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

// calculateP99 兼容旧 API。
//
// Deprecated: 请使用 calculatePercentile。
func calculateP99(values []int64) int64 {
	return calculatePercentile(values, 0.99)
}

// FailureReasonStat 失败原因聚合。
type FailureReasonStat struct {
	Reason     string `json:"reason"`      // 截断后的错误前缀
	Count      int64  `json:"count"`       // 出现次数
	LatestTime string `json:"latest_time"` // 最近一次发生时间
}

// GetFailureReasonTopK 返回最近 since 时间内失败原因 Top K（按错误前缀聚合）。
// 用于运维快速定位"最常见的失败原因"。
func GetFailureReasonTopK(theme string, since time.Duration, topK int, ctx context.Context) ([]FailureReasonStat, error) {
	if since <= 0 {
		since = time.Hour
	}
	if topK <= 0 {
		topK = 10
	}

	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-since)
	type row struct {
		Reason     string    `gorm:"column:reason"`
		Count      int64     `gorm:"column:cnt"`
		LatestTime time.Time `gorm:"column:latest"`
	}
	rows := make([]row, 0)
	// 取错误信息前 80 字符聚合，避免不同 trace_id 导致永远不重复
	tx := db.GetGormDb().WithContext(ctx).
		Table("asynctask_exec_row as exec_rows").
		Select("LEFT(exec_rows.last_err_msg, 80) AS reason, COUNT(*) AS cnt, MAX(exec_rows.last_run_end_time) AS latest").
		Joins("join asynctasks on asynctasks.id = exec_rows.task_id").
		Where("asynctasks.system_name = ?", theme).
		Where("exec_rows.task_status = ?", TaskStatusFailed.String()).
		Where("exec_rows.last_run_time > ?", cutoff).
		Where("exec_rows.last_err_msg <> ''").
		Group("reason").
		Order("cnt DESC").
		Limit(topK).
		Find(&rows)
	if tx.Error != nil {
		return nil, tx.Error
	}

	result := make([]FailureReasonStat, 0, len(rows))
	for _, r := range rows {
		result = append(result, FailureReasonStat{
			Reason:     r.Reason,
			Count:      r.Count,
			LatestTime: r.LatestTime.Format(time.RFC3339),
		})
	}
	return result, nil
}

// SlowTaskStat 慢任务统计。
type SlowTaskStat struct {
	TaskId     int64  `json:"task_id"`
	TaskName   string `json:"task_name"`
	Duration   int64  `json:"duration_ms"`
	Status     string `json:"status"`
	LastRunEnd string `json:"last_run_end"`
}

// GetSlowTaskTopK 返回最近 since 时间内最慢的 K 个任务。
func GetSlowTaskTopK(theme string, since time.Duration, topK int, ctx context.Context) ([]SlowTaskStat, error) {
	if since <= 0 {
		since = time.Hour
	}
	if topK <= 0 {
		topK = 10
	}

	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-since)
	type row struct {
		TaskId     int64     `gorm:"column:task_id"`
		TaskName   string    `gorm:"column:task_name"`
		Duration   int64     `gorm:"column:last_run_duration"`
		Status     string    `gorm:"column:task_status"`
		LastRunEnd time.Time `gorm:"column:last_run_end_time"`
	}
	rows := make([]row, 0)
	tx := db.GetGormDb().WithContext(ctx).
		Table("asynctask_exec_row as exec_rows").
		Select("exec_rows.task_id, asynctasks.task_name, exec_rows.last_run_duration, exec_rows.task_status, exec_rows.last_run_end_time").
		Joins("join asynctasks on asynctasks.id = exec_rows.task_id").
		Where("asynctasks.system_name = ?", theme).
		Where("exec_rows.last_run_time > ?", cutoff).
		Order("exec_rows.last_run_duration DESC").
		Limit(topK).
		Find(&rows)
	if tx.Error != nil {
		return nil, tx.Error
	}

	result := make([]SlowTaskStat, 0, len(rows))
	for _, r := range rows {
		result = append(result, SlowTaskStat{
			TaskId:     r.TaskId,
			TaskName:   r.TaskName,
			Duration:   r.Duration,
			Status:     r.Status,
			LastRunEnd: r.LastRunEnd.Format(time.RFC3339),
		})
	}
	return result, nil
}
