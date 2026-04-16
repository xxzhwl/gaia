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
	Id              int64
	TaskId          int64
	TaskStatus      string
	LastResult      string
	LastErrMsg      string
	LastRunTime     time.Time
	LastRunEndTime  time.Time
	LastRunDuration int64
	LogId           string
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

// TaskExecStats 任务执行统计
type TaskExecStats struct {
	TotalTasks  int   // 总任务数
	AvgExecTime int64 // 平均执行时间（毫秒）
	P99ExecTime int64 // P99 执行时间（毫秒）
	AvgWaitTime int64 // Deprecated: 等待时间不再计算，保留字段仅兼容旧调用方
	P99WaitTime int64 // Deprecated: 等待时间不再计算，保留字段仅兼容旧调用方
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

	// 计算统计值
	stats := &TaskExecStats{
		TotalTasks: len(records),
	}

	var totalExecTime int64
	execTimes := make([]int64, 0, len(records))

	for _, r := range records {
		execTime := r.LastRunDuration

		totalExecTime += execTime
		execTimes = append(execTimes, execTime)
	}

	stats.AvgExecTime = totalExecTime / int64(len(records))
	stats.P99ExecTime = calculateP99(execTimes)

	return stats, nil
}

// calculateP99 计算 P99 值
func calculateP99(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}

	slices.Sort(values)

	index := int(float64(len(values)) * 0.99)
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
