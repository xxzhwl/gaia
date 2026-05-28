// Package asynctask 注释
// @author wanlizhan
// @created 2025/02/11
package asynctask

import (
	"context"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
)

// ListTasksArgs 任务列表查询参数
type ListTasksArgs struct {
	Page       int      `json:"page" require:"1" range:"1,10000"`
	PageSize   int      `json:"page_size" require:"1" range:"1,100"`
	TaskStatus []string `json:"task_status"` // 任务状态筛选
	TaskName   string   `json:"task_name"`   // 任务名称模糊查询
	TenantId   string   `json:"tenant_id"`   // 租户筛选
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

	query := db.GetGormDb().WithContext(ctx).Table(taskTable)
	if systemName != "" {
		query = query.Where("system_name = ?", systemName)
	}

	// 状态筛选
	if len(args.TaskStatus) > 0 {
		query = query.Where("task_status in ?", args.TaskStatus)
	}

	// 名称模糊查询
	if args.TaskName != "" {
		query = query.Where("task_name LIKE ? ESCAPE '\\\\'", "%"+escapeLikePattern(args.TaskName)+"%")
	}

	if args.TenantId != "" {
		query = query.Where("tenant_id = ?", args.TenantId)
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
	return GetTaskExecRecordsWithStatus(taskId, "", page, pageSize, ctx)
}

// GetTaskExecRecordsWithStatus 获取任务执行记录，并可按状态筛选。
func GetTaskExecRecordsWithStatus(taskId int64, status string, page, pageSize int, ctx context.Context) ([]TaskExecModel, int64, error) {
	db, err := gaia.NewMysqlWithSchema("AsyncTask.Mysql")
	if err != nil {
		return nil, 0, err
	}

	var total int64
	var records []TaskExecModel

	query := db.GetGormDb().WithContext(ctx).
		Table("asynctask_exec_row as exec_rows").
		Select("exec_rows.*, asynctasks.task_name as task_name").
		Joins("left join asynctasks on asynctasks.id = exec_rows.task_id")
	if taskId > 0 {
		query = query.Where("exec_rows.task_id = ?", taskId)
	}
	if status != "" {
		query = query.Where("exec_rows.task_status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("exec_rows.id desc").Offset(offset).Limit(pageSize).Find(&records).Error; err != nil {
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

type SchedulerInfo struct {
	Theme   string              `json:"theme"`
	Running bool                `json:"running"`
	Status  SchedulerStatusInfo `json:"status"`
}

func GetAllSchedulerInfo() []SchedulerInfo {
	locker.RLock()
	defer locker.RUnlock()

	result := make([]SchedulerInfo, 0)
	for theme, scheduler := range SchedulerMap {
		result = append(result, SchedulerInfo{
			Theme:   theme,
			Running: scheduler.IsRunning(),
			Status:  scheduler.GetStatusInfo(),
		})
	}
	return result
}

func StopScheduler(theme string) bool {
	locker.Lock()
	defer locker.Unlock()

	if scheduler, ok := SchedulerMap[theme]; ok {
		scheduler.Stop()
		return true
	}
	return false
}

func ResumeScheduler(theme string) bool {
	locker.Lock()
	defer locker.Unlock()

	if scheduler, ok := SchedulerMap[theme]; ok {
		scheduler.Resume()
		return true
	}
	return false
}

// GetMetricsSnapshot 返回某个 theme 当前的指标快照（进程内累计 + 队列水位 + 状态）。
// 该快照不依赖 OTel exporter，admin 接口可直接序列化返回。
func GetMetricsSnapshot(theme string) (MetricsSnapshot, bool) {
	scheduler := GetScheduler(theme)
	if scheduler == nil {
		return MetricsSnapshot{}, false
	}
	return scheduler.SnapshotMetrics(), true
}

// GetAllMetricsSnapshot 返回所有 scheduler 的指标快照。
func GetAllMetricsSnapshot() []MetricsSnapshot {
	locker.RLock()
	defer locker.RUnlock()

	result := make([]MetricsSnapshot, 0, len(SchedulerMap))
	for _, scheduler := range SchedulerMap {
		result = append(result, scheduler.SnapshotMetrics())
	}
	return result
}

func escapeLikePattern(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
