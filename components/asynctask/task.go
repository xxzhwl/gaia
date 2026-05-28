// Package asynctask 注释
// @author wanlizhan
// @created 2024/4/29
package asynctask

import (
	"context"
	"database/sql"
	"time"

	"github.com/xxzhwl/gaia"
)

var taskTable = "asynctasks"

// TaskModel 任务模型
type TaskModel struct {
	Id int64 `gorm:"column:id;primaryKey;autoIncrement"`
	TaskBaseInfo
	SystemName      string       `gorm:"column:system_name;size:32;not null;default:'';index:idx_asynctasks_system_name"`
	TaskStatus      string       `gorm:"column:task_status;size:32;not null;default:Wait;index:idx_asynctasks_task_status"`
	LastRunTime     sql.NullTime `gorm:"column:last_run_time"`
	LastRunEndTime  sql.NullTime `gorm:"column:last_run_end_time"`
	LastRunDuration int64        `gorm:"column:last_run_duration;not null;default:0"`
	CreateAt        time.Time    `gorm:"column:create_time;index:idx_asynctasks_create_time"`
	UpdateAt        time.Time    `gorm:"column:update_time;autoUpdateTime"`
	RetryTime       int          `gorm:"column:retry_time;not null;default:0"`
	LastResult      string       `gorm:"column:last_result;type:longtext"`
	LastErrMsg      string       `gorm:"column:last_err_msg;size:512;not null;default:''"`
	LogId           string       `gorm:"column:log_id;size:64"`
}

func (TaskModel) TableName() string { return taskTable }

// TaskBaseInfo 任务基本信息
type TaskBaseInfo struct {
	ServiceName  string `gorm:"column:service_name;size:32;not null;default:'';index:idx_asynctasks_service_name"`
	MethodName   string `gorm:"column:method_name;size:64;not null;default:'';index:idx_asynctasks_method_name"`
	TaskName     string `gorm:"column:task_name;size:128;not null;default:'';index:idx_asynctasks_task_name"`
	Arg          string `gorm:"column:arg;type:longtext"`
	MaxRetryTime int    `gorm:"column:max_retry_time;not null;default:0"`
	Priority     int32  `gorm:"column:priority;not null;default:0;index:idx_asynctasks_priority"`
	TenantId     string `gorm:"column:tenant_id;size:64;not null;default:'';index:idx_asynctasks_tenant_id"`
}

// AddTask 新增一个任务
func AddTask(task TaskBaseInfo, systemName string, ctx context.Context) (model TaskModel, err error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return TaskModel{}, err
	}

	model = TaskModel{
		TaskBaseInfo: task,
		SystemName:   systemName,
		TaskStatus:   TaskStatusWait.String(),
		CreateAt:     time.Now(),
		UpdateAt:     time.Now(),
	}

	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Create(&model)
	if tx.Error != nil {
		return TaskModel{}, tx.Error
	}

	// 推 AsyncTaskLog（独立 ES index：async_task_log，phase=enqueue）
	emitEnqueueLog(model)

	return model, nil
}

func findNeedRunTaskIds(limit int, systemName string, ctx context.Context) (taskIds []int64, needContinue bool, err error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, false, err
	}

	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Select("id").
		Where("task_status in ?", []string{TaskStatusWait.String(), TaskStatusRetry.String()}).
		Where("system_name = ?", systemName).
		Limit(limit).Order("id asc").
		Find(&taskIds)
	if tx.Error != nil {
		return nil, false, tx.Error
	}
	if len(taskIds) == limit {
		return taskIds, true, nil
	}
	return
}

func tryLockTask(taskId int64, ctx context.Context) (flag bool, err error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return false, err
	}

	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Where(map[string]any{"id": taskId,
		"task_status": []string{TaskStatusWait.String(), TaskStatusRetry.String()}}).
		UpdateColumn("task_status", TaskStatusRunning.String())
	if tx.Error != nil {
		return false, tx.Error
	}
	if tx.RowsAffected == 0 {
		return false, nil
	}
	return true, nil
}

func updateTaskSuccess(taskId int64, res string, startTime time.Time, ctx context.Context) error {
	endTime := time.Now()
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}
	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Where(map[string]any{"id": taskId}).
		Updates(map[string]any{"task_status": TaskStatusSuccess.String(),
			"last_result": res, "last_run_time": startTime, "last_run_end_time": endTime,
			"last_run_duration": endTime.Sub(startTime).Milliseconds(), "log_id": gaia.GetContextTrace().Id})
	if tx.Error != nil {
		return tx.Error
	}
	return InsertTaskExecRow(TaskExecModel{
		TaskId:          taskId,
		TaskStatus:      TaskStatusSuccess.String(),
		LastResult:      res,
		LastRunTime:     startTime,
		LastRunEndTime:  endTime,
		LastRunDuration: endTime.Sub(startTime).Milliseconds(),
		LogId:           gaia.GetContextTrace().Id,
	}, ctx)
}

func updateTaskFailed(taskId int64, res string, startTime time.Time, ctx context.Context) error {
	endTime := time.Now()
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}
	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Where(map[string]any{"id": taskId}).
		Updates(map[string]any{"task_status": TaskStatusFailed.String(), "last_err_msg": res,
			"last_run_time": startTime, "last_run_end_time": endTime, "log_id": gaia.GetContextTrace().Id,
			"last_run_duration": endTime.Sub(startTime).Milliseconds()})
	if tx.Error != nil {
		return tx.Error
	}
	return InsertTaskExecRow(TaskExecModel{
		TaskId:          taskId,
		TaskStatus:      TaskStatusFailed.String(),
		LastErrMsg:      res,
		LastRunTime:     startTime,
		LastRunEndTime:  endTime,
		LastRunDuration: endTime.Sub(startTime).Milliseconds(),
		LogId:           gaia.GetContextTrace().Id,
	}, ctx)
}

func updateTaskWait(taskId int64, res string, startTime time.Time, ctx context.Context) error {
	endTime := time.Now()
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}
	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Where(map[string]any{"id": taskId}).
		Updates(map[string]any{"task_status": TaskStatusWait.String(), "last_err_msg": res,
			"last_run_time": startTime, "last_run_end_time": endTime, "log_id": gaia.GetContextTrace().Id,
			"last_run_duration": endTime.Sub(startTime).Milliseconds()})
	if tx.Error != nil {
		return tx.Error
	}
	return InsertTaskExecRow(TaskExecModel{
		TaskId:          taskId,
		TaskStatus:      TaskStatusFailed.String(),
		LastErrMsg:      res,
		LastRunTime:     startTime,
		LastRunEndTime:  endTime,
		LastRunDuration: endTime.Sub(startTime).Milliseconds(),
		LogId:           gaia.GetContextTrace().Id,
	}, ctx)
}

func updateTaskRetry(taskInfo TaskModel, res string, startTime time.Time, ctx context.Context) error {
	endTime := time.Now()
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}
	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Where(map[string]any{"id": taskInfo.Id}).
		Updates(map[string]any{"task_status": TaskStatusRetry.String(), "last_result": res,
			"retry_time": taskInfo.RetryTime + 1, "log_id": gaia.GetContextTrace().Id,
			"last_run_time": startTime, "last_run_end_time": endTime,
			"last_run_duration": endTime.Sub(startTime).Milliseconds()})
	if tx.Error != nil {
		return tx.Error
	}
	return InsertTaskExecRow(TaskExecModel{
		TaskId:          taskInfo.Id,
		TaskStatus:      TaskStatusRetry.String(),
		LastResult:      res,
		LastRunTime:     startTime,
		LastRunEndTime:  endTime,
		LastRunDuration: endTime.Sub(startTime).Milliseconds(),
		LogId:           gaia.GetContextTrace().Id,
	}, ctx)
}

func getTaskById(taskId int64, ctx context.Context) (model TaskModel, err error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return TaskModel{}, err
	}
	tx := db.GetGormDb().WithContext(ctx).Table(taskTable).Find(&model, "id=?", taskId)
	if tx.Error != nil {
		return TaskModel{}, tx.Error
	}
	return model, nil
}
