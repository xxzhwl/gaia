// Package asynctask 注释
// @author wanlizhan
// @created 2025/02/11
package asynctask

import (
	"context"
	"time"

	"github.com/xxzhwl/gaia"
)

// ListTasksArgs 任务列表查询参数
type ListTasksArgs struct {
	Page       int      `json:"page" require:"1" range:"1,10000"`
	PageSize   int      `json:"page_size" require:"1" range:"1,100"`
	TaskStatus []string `json:"task_status"` // 任务状态筛选
	TaskName   string   `json:"task_name"`   // 任务名称模糊查询
}

// ListTasksResult 任务列表查询结果
type ListTasksResult struct {
	List  []TaskModel `json:"list"`
	Total int64       `json:"total"`
}

// ListTasks 获取任务列表（供管理后台使用）
func ListTasks(args ListTasksArgs, systemName string, ctx context.Context) (ListTasksResult, error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return ListTasksResult{}, err
	}

	result := ListTasksResult{
		List: make([]TaskModel, 0),
	}

	query := db.GetGormDb().WithContext(ctx).Table(taskTable).Where("system_name = ?", systemName)

	// 状态筛选
	if len(args.TaskStatus) > 0 {
		query = query.Where("task_status in ?", args.TaskStatus)
	}

	// 名称模糊查询
	if args.TaskName != "" {
		query = query.Where("task_name like ?", "%"+args.TaskName+"%")
	}

	// 查询总数
	if err := query.Count(&result.Total).Error; err != nil {
		return ListTasksResult{}, err
	}

	// 分页查询
	offset := (args.Page - 1) * args.PageSize
	if err := query.Order("id desc").Offset(offset).Limit(args.PageSize).Find(&result.List).Error; err != nil {
		return ListTasksResult{}, err
	}

	return result, nil
}

// GetTaskDetail 获取任务详情（供管理后台使用）
func GetTaskDetail(taskId int64, ctx context.Context) (TaskModel, error) {
	return getTaskById(taskId, ctx)
}

// GetTaskExecRecords 获取任务执行记录（供管理后台使用）
func GetTaskExecRecords(taskId int64, page, pageSize int, ctx context.Context) ([]TaskExecModel, int64, error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, 0, err
	}

	var total int64
	var records []TaskExecModel

	query := db.GetGormDb().WithContext(ctx).Table("asynctask_exec_row").Where("task_id = ?", taskId)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("id desc").Offset(offset).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// RetryTask 手动重试任务（供管理后台使用）
func RetryTask(taskId int64, ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}

	return db.GetGormDb().WithContext(ctx).Table(taskTable).Where("id = ?", taskId).
		Updates(map[string]any{
			"task_status": TaskStatusWait.String(),
			"retry_time":  0,
			"update_time": time.Now(),
		}).Error
}

// CancelTask 取消任务（供管理后台使用）
func CancelTask(taskId int64, ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return err
	}

	return db.GetGormDb().WithContext(ctx).Table(taskTable).Where("id = ?", taskId).
		UpdateColumn("task_status", TaskStatusFailed.String()).Error
}

// GetAllSchedulerStatus 获取所有调度器状态（供管理后台使用）
func GetAllSchedulerStatus() map[string]SchedulerStatusInfo {
	locker.RLock()
	defer locker.RUnlock()

	result := make(map[string]SchedulerStatusInfo)
	for theme, scheduler := range SchedulerMap {
		result[theme] = scheduler.GetStatusInfo()
	}
	return result
}

// StopScheduler 停止指定调度器（供管理后台使用）
func StopScheduler(theme string) bool {
	locker.Lock()
	defer locker.Unlock()

	if scheduler, ok := SchedulerMap[theme]; ok {
		// 设置退出标志，实际退出由各个 goroutine 检测 exitContext 完成
		delete(SchedulerMap, theme)
		_ = scheduler
		return true
	}
	return false
}
