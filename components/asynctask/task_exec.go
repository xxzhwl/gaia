// Package asynctask 注释
// @author wanlizhan
// @created 2024/5/9
package asynctask

import (
	"context"
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
