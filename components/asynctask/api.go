// Package asynctask 包注释
// @author wanlizhan
// @created 2026/4/16
package asynctask

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia"
)

// StartScheduler 一键创建并启动调度器。
func StartScheduler(ctx context.Context, theme string, options ...SchedulerOption) *Scheduler {
	scheduler := NewScheduler(theme, options...)
	if scheduler.IsRunning() {
		return scheduler
	}
	if atomic.LoadInt32(&scheduler.startFlag) != 0 {
		scheduler.Resume()
		return scheduler
	}
	go func() {
		defer gaia.CatchPanic()
		scheduler.Start(ctx)
	}()
	return scheduler
}

// SubmitTask 快捷提交任务并尝试快速入队。
func SubmitTask(theme string, task TaskBaseInfo) (TaskModel, error) {
	scheduler := GetScheduler(theme)
	if scheduler == nil {
		return TaskModel{}, fmt.Errorf("scheduler %s not found", theme)
	}

	model, err := AddTask(task, theme, context.Background())
	if err != nil {
		return TaskModel{}, err
	}

	scheduler.TaskQuickQueue(model.Id)
	return model, nil
}

// SubmitTaskWithRetry 快捷提交带重试配置的任务。
func SubmitTaskWithRetry(theme, serviceName, methodName, taskName, arg string, maxRetry int) (TaskModel, error) {
	return SubmitTask(theme, TaskBaseInfo{
		ServiceName:  serviceName,
		MethodName:   methodName,
		TaskName:     taskName,
		Arg:          arg,
		MaxRetryTime: maxRetry,
	})
}

// GetTaskStatus 查询任务当前状态。
func GetTaskStatus(taskId int64) (string, error) {
	task, err := getTaskById(taskId, context.Background())
	if err != nil {
		return "", err
	}
	if task.Id == 0 {
		return "", fmt.Errorf("task %d not found", taskId)
	}
	return task.TaskStatus, nil
}

// IsTaskDone 判断任务是否已完成。
func IsTaskDone(taskId int64) (bool, error) {
	status, err := GetTaskStatus(taskId)
	if err != nil {
		return false, err
	}
	return status == TaskStatusSuccess.String() || status == TaskStatusFailed.String(), nil
}

// IsTaskSuccess 判断任务是否执行成功。
func IsTaskSuccess(taskId int64) (bool, error) {
	status, err := GetTaskStatus(taskId)
	if err != nil {
		return false, err
	}
	return status == TaskStatusSuccess.String(), nil
}

// WaitTaskDone 阻塞等待任务完成或超时。
func WaitTaskDone(taskId int64, timeout time.Duration) (TaskModel, error) {
	if timeout <= 0 {
		return TaskModel{}, fmt.Errorf("timeout must be greater than 0")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		task, err := getTaskById(taskId, ctx)
		if err != nil {
			return TaskModel{}, err
		}
		if task.Id == 0 {
			return TaskModel{}, fmt.Errorf("task %d not found", taskId)
		}
		if task.TaskStatus == TaskStatusSuccess.String() || task.TaskStatus == TaskStatusFailed.String() {
			return task, nil
		}

		select {
		case <-ctx.Done():
			return TaskModel{}, fmt.Errorf("wait task %d done timeout after %s", taskId, timeout)
		case <-ticker.C:
		}
	}
}

// TaskCountByStatus 按状态统计任务数量。
type TaskCountByStatus struct {
	Wait    int64 `json:"wait"`
	Running int64 `json:"running"`
	Success int64 `json:"success"`
	Failed  int64 `json:"failed"`
	Retry   int64 `json:"retry"`
	Total   int64 `json:"total"`
}

type taskStatusCountRow struct {
	TaskStatus string `gorm:"column:task_status"`
	Total      int64  `gorm:"column:total"`
}

// GetTaskCountByStatus 统计某个 theme 下各状态任务数量。
func GetTaskCountByStatus(theme string) (TaskCountByStatus, error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return TaskCountByStatus{}, err
	}

	rows := make([]taskStatusCountRow, 0)
	if err := db.GetGormDb().WithContext(context.Background()).Table(taskTable).
		Select("task_status, count(*) as total").
		Where("system_name = ?", theme).
		Group("task_status").
		Find(&rows).Error; err != nil {
		return TaskCountByStatus{}, err
	}

	result := TaskCountByStatus{}
	for _, row := range rows {
		switch row.TaskStatus {
		case TaskStatusWait.String():
			result.Wait = row.Total
		case TaskStatusRunning.String():
			result.Running = row.Total
		case TaskStatusSuccess.String():
			result.Success = row.Total
		case TaskStatusFailed.String():
			result.Failed = row.Total
		case TaskStatusRetry.String():
			result.Retry = row.Total
		}
		result.Total += row.Total
	}

	return result, nil
}

// CleanFinishedTasks 清理指定时间之前的已完成任务及关联记录。
func CleanFinishedTasks(theme string, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, fmt.Errorf("olderThan must be greater than 0")
	}

	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cutoff := time.Now().Add(-olderThan)
	taskIds := make([]int64, 0)
	if err := db.GetGormDb().WithContext(ctx).Table(taskTable).
		Where("system_name = ?", theme).
		Where("task_status IN ?", []string{TaskStatusSuccess.String(), TaskStatusFailed.String()}).
		Where("update_time < ?", cutoff).
		Pluck("id", &taskIds).Error; err != nil {
		return 0, err
	}

	if len(taskIds) == 0 {
		return 0, nil
	}

	tx := db.GetGormDb().WithContext(ctx).Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}

	if err := tx.Table("asynctask_exec_row").Where("task_id IN ?", taskIds).Delete(&TaskExecModel{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}
	if err := tx.Table(heartBeatTable).Where("task_id IN ?", taskIds).Delete(&HeartBeatModel{}).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	deleteResult := tx.Table(taskTable).Where("id IN ?", taskIds).Delete(&TaskModel{})
	if deleteResult.Error != nil {
		tx.Rollback()
		return 0, deleteResult.Error
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}

	return deleteResult.RowsAffected, nil
}
