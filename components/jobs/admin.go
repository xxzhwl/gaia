// Package jobs 包注释
// @author wanlizhan
// @created 2025/02/11
package jobs

import (
	"context"
	"net/http"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
)

// ListJobsArgs 任务列表查询参数
type ListJobsArgs struct {
	Page     int    `json:"page" require:"1" range:"1,10000"`
	PageSize int    `json:"page_size" require:"1" range:"1,100"`
	JobType  string `json:"job_type"` // 任务类型筛选：cron_job/cron_hook
	JobName  string `json:"job_name"` // 任务名称模糊查询
	Enabled  *bool  `json:"enabled"`  // 启用状态筛选
}

// ListJobsResult 任务列表查询结果
type ListJobsResult struct {
	List  []job `json:"list"`
	Total int64 `json:"total"`
}

// ListJobs 获取任务列表（供管理后台使用）
func (r *RunJob) ListJobs(args ListJobsArgs, ctx context.Context) (ListJobsResult, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return ListJobsResult{}, err
	}

	result := ListJobsResult{
		List: make([]job, 0),
	}

	query := db.GetGormDb().WithContext(ctx).Table(JobCenterTable)

	// 类型筛选
	if args.JobType != "" {
		query = query.Where("job_type = ?", args.JobType)
	}

	// 名称模糊查询
	if args.JobName != "" {
		query = query.Where("job_name like ?", "%"+args.JobName+"%")
	}

	// 启用状态筛选
	if args.Enabled != nil {
		query = query.Where("enabled = ?", *args.Enabled)
	}

	// 查询总数
	if err := query.Count(&result.Total).Error; err != nil {
		return ListJobsResult{}, err
	}

	// 分页查询
	offset := (args.Page - 1) * args.PageSize
	if err := query.Order("id desc").Offset(offset).Limit(args.PageSize).Find(&result.List).Error; err != nil {
		return ListJobsResult{}, err
	}

	return result, nil
}

// GetJobDetail 获取任务详情（供管理后台使用）
func (r *RunJob) GetJobDetail(jobId int64, ctx context.Context) (job, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return job{}, err
	}

	var j job
	tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).Where("id = ?", jobId).First(&j)
	if tx.Error != nil {
		return job{}, tx.Error
	}
	return j, nil
}

// GetJobRecords 获取任务执行记录（供管理后台使用）
func (r *RunJob) GetJobRecords(jobId int64, page, pageSize int, ctx context.Context) ([]jobRecord, int64, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return nil, 0, err
	}

	// 先获取任务信息以确定 job_name
	jobDetail, err := r.GetJobDetail(jobId, ctx)
	if err != nil {
		return nil, 0, err
	}

	var total int64
	var records []jobRecord

	query := db.GetGormDb().WithContext(ctx).Table(JobRecordTable).Where("job_name = ?", jobDetail.JobName)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Order("id desc").Offset(offset).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, 0, err
	}

	return records, total, nil
}

// ToggleJob 启用/禁用任务（供管理后台使用）
func (r *RunJob) ToggleJob(jobId int64, enabled bool, ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}

	return db.GetGormDb().WithContext(ctx).Table(JobCenterTable).Where("id = ?", jobId).
		UpdateColumn("enabled", enabled).Error
}

// ExecuteJobImmediately 立即执行任务（供管理后台使用）
func (r *RunJob) ExecuteJobImmediately(jobId int64, ctx context.Context) error {
	jobDetail, err := r.GetJobDetail(jobId, ctx)
	if err != nil {
		return err
	}

	// 检查任务是否已在运行
	if running, err := r.jobIsRunning(jobDetail); err != nil {
		return err
	} else if running {
		return nil
	}

	// 更新状态为运行中
	if err := r.updateJobToRunning(jobDetail); err != nil {
		return err
	}

	// 根据任务类型执行
	switch jobDetail.JobType {
	case CronJob:
		service, err := GetCronService(jobDetail.ServiceName)
		if err != nil {
			r.updateJobToWaitWithRes(jobDetail, err)
			return err
		}
		res, doErr := gaia.CallMethodWithArgs(service, jobDetail.ServiceMethod, jobDetail.Args)
		if doErr != nil {
			r.updateJobToWaitWithRes(jobDetail, doErr)
			return doErr
		}
		return r.updateJobToWaitWithRes(jobDetail, res)
	case CronHook:
		// Hook 类型任务通过 HTTP 调用
		resInfo, _, err := httpclient.NewHttpRequest(jobDetail.HookUrl).
			WithMethod(http.MethodPost).
			WithBody(jobDetail.Args).
			Do()
		if err != nil {
			r.updateJobToWaitWithRes(jobDetail, err)
			return err
		}
		return r.updateJobToWaitWithRes(jobDetail, string(resInfo))
	}

	return nil
}

// GetRunningJobs 获取当前正在运行的任务（供管理后台使用）
func (r *RunJob) GetRunningJobs(ctx context.Context) ([]job, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return nil, err
	}

	var jobs []job
	tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).Where("run_status = ?", RunStatusRunning).Find(&jobs)
	if tx.Error != nil {
		return nil, tx.Error
	}
	return jobs, nil
}
