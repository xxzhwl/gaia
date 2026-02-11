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
	Id int64
	TaskBaseInfo
	SystemName      string
	TaskStatus      string
	LastRunTime     sql.NullTime
	LastRunEndTime  sql.NullTime
	LastRunDuration int64
	CreateAt        time.Time `gorm:"column:create_time"`
	UpdateAt        time.Time `gorm:"column:update_time"`
	RetryTime       int
	LastResult      string
	LastErrMsg      string
	LogId           string
}

// TaskBaseInfo 任务基本信息
type TaskBaseInfo struct {
	ServiceName  string
	MethodName   string
	TaskName     string
	Arg          string
	MaxRetryTime int
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
