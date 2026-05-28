// Package jobs 注释
// @author wanlizhan
// @created 2025/02/11
package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
)

// ListJobsArgs 任务列表查询参数
type ListJobsArgs struct {
	Page       int    `json:"page" require:"1" range:"1,10000"`
	PageSize   int    `json:"page_size" require:"1" range:"1,100"`
	SystemName string `json:"system_name"` // 系统名称筛选
	JobType    string `json:"job_type"`    // 任务类型筛选：cron_job/cron_hook
	JobName    string `json:"job_name"`    // 任务名称模糊查询
	Enabled    *bool  `json:"enabled"`     // 启用状态筛选
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

	if args.JobType != "" {
		query = query.Where("job_type = ?", args.JobType)
	}

	if args.SystemName != "" {
		query = query.Where("system_name = ?", args.SystemName)
	}

	if args.JobName != "" {
		query = query.Where("job_name like ?", "%"+args.JobName+"%")
	}

	if args.Enabled != nil {
		query = query.Where("enabled = ?", *args.Enabled)
	}

	if err := query.Count(&result.Total).Error; err != nil {
		return ListJobsResult{}, err
	}

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
	return r.GetJobRecordsWithStatus(jobId, "", page, pageSize, ctx)
}

// GetJobRecordsWithStatus 获取任务执行记录，并可按执行结果筛选。
func (r *RunJob) GetJobRecordsWithStatus(jobId int64, status string, page, pageSize int, ctx context.Context) ([]jobRecord, int64, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return nil, 0, err
	}

	jobDetail, err := r.GetJobDetail(jobId, ctx)
	if err != nil {
		return nil, 0, err
	}

	var total int64
	var records []jobRecord

	query := db.GetGormDb().WithContext(ctx).Table(JobRecordTable).
		Where("system_name = ? AND job_name = ?", jobDetail.SystemName, jobDetail.JobName)
	if status != "" {
		query = query.Where("job_result_flag = ?", status)
	}

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

	// HA 开启时走原子抢锁，避免与其他副本冲突。
	if !r.haDisabled {
		leaseDur := computeLeaseDuration(jobDetail.JobBase)
		ok, err := r.tryAcquireJobLease(ctx, jobId, leaseDur)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("任务正在运行中或被其他实例抢占")
		}
		stopHb := r.startLeaseHeartbeat(context.Background(), jobId, leaseDur)
		defer stopHb()
	} else {
		if running, err := r.jobIsRunning(jobDetail); err != nil {
			return err
		} else if running {
			return errors.New("任务正在运行中")
		}

		if err := r.updateJobToRunning(jobDetail); err != nil {
			return err
		}
	}

	switch jobDetail.JobType {
	case CronJob:
		service, err := GetCronServiceForSystem(jobDetail.SystemName, jobDetail.ServiceName)
		if err != nil {
			r.updateJobToWaitWithRes(jobDetail, err)
			return err
		}

		var res any
		var doErr error
		var panicMsg string
		var isPanic bool

		func() {
			defer func() {
				if rr := recover(); rr != nil {
					panicMsg = gaia.PanicLog(rr)
					isPanic = true
					r.cronJobLogger.ErrorF("任务%d-%s执行panic: %s", jobDetail.Id, jobDetail.JobName, panicMsg)
					r.fireAlarm(fmt.Sprintf("JobPanic[%s]", jobDetail.JobName),
						fmt.Sprintf("任务ID: %d, 任务名称: %s, Panic信息: %s", jobDetail.Id, jobDetail.JobName, panicMsg))
				}
			}()
			res, doErr = gaia.CallMethodWithArgs(service, jobDetail.ServiceMethod, jobDetail.Args)
		}()

		if isPanic {
			r.updateJobToWaitWithResAndPanic(jobDetail, panicMsg)
			return errors.New("任务执行 panic: " + panicMsg)
		}
		if doErr != nil {
			r.updateJobToWaitWithRes(jobDetail, doErr)
			return doErr
		}
		return r.updateJobToWaitWithRes(jobDetail, res)

	case CronHook:
		var resInfo string
		var doErr error
		var panicMsg string
		var isPanic bool

		func() {
			defer func() {
				if rr := recover(); rr != nil {
					panicMsg = gaia.PanicLog(rr)
					isPanic = true
					r.cronHookLogger.ErrorF("任务%d-%s执行panic: %s", jobDetail.Id, jobDetail.JobName, panicMsg)
					r.fireAlarm(fmt.Sprintf("JobHookPanic[%s]", jobDetail.JobName),
						fmt.Sprintf("任务ID: %d, 任务名称: %s, Panic信息: %s", jobDetail.Id, jobDetail.JobName, panicMsg))
				}
			}()
			var rawRes []byte
			rawRes, _, doErr = httpclient.NewHttpRequest(jobDetail.HookUrl).WithMethod(http.MethodPost).
				WithBody(jobDetail.Args).
				Do()
			resInfo = string(rawRes)
		}()

		if isPanic {
			r.updateJobToWaitWithResAndPanic(jobDetail, panicMsg)
			return errors.New("任务执行 panic: " + panicMsg)
		}
		if doErr != nil {
			r.updateJobToWaitWithRes(jobDetail, doErr)
			return doErr
		}
		return r.updateJobToWaitWithRes(jobDetail, resInfo)
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

// JobsHealthInfo 环境健康数据，用于托管面版 / 外部探针。
type JobsHealthInfo struct {
	Healthy        bool                `json:"healthy"`
	InstanceId     string              `json:"instance_id"` // 本进程实例ID，供多副本排查
	DBSchema       string              `json:"db_schema"`
	HADisabled     bool                `json:"ha_disabled"`
	MetricsSnap    JobsMetricsSnapshot `json:"metrics"`
	RunningJobNum  int                 `json:"running_job_num"`
	WaitJobNum     int                 `json:"wait_job_num"`
	DisabledJobNum int                 `json:"disabled_job_num"`
	MyOwnedJobNum  int                 `json:"my_owned_job_num"` // 当前本实例持有租约的任务数
}

// GetHealthInfo 返回当前 RunJob 的健康 / 运行状态 / 指标快照。
func (r *RunJob) GetHealthInfo(ctx context.Context) (JobsHealthInfo, error) {
	info := JobsHealthInfo{
		Healthy:     r.IsHealthy(),
		InstanceId:  GetInstanceId(),
		DBSchema:    r.dbSchema,
		HADisabled:  r.haDisabled,
		MetricsSnap: r.SnapshotMetrics(),
	}

	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return info, err
	}
	type countRow struct {
		Status  string
		Enabled bool
		Count   int64
	}
	rows := make([]countRow, 0)
	err = db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Select("run_status as status, enabled, COUNT(*) as count").
		Group("run_status, enabled").Scan(&rows).Error
	if err != nil {
		return info, err
	}
	for _, row := range rows {
		if !row.Enabled {
			info.DisabledJobNum += int(row.Count)
			continue
		}
		switch row.Status {
		case RunStatusRunning:
			info.RunningJobNum += int(row.Count)
		case RunStatusWait:
			info.WaitJobNum += int(row.Count)
		}
	}

	// 本实例持有租约数
	if !r.haDisabled {
		var mine int64
		if err := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
			Where("lease_owner = ?", GetInstanceId()).
			Count(&mine).Error; err == nil {
			info.MyOwnedJobNum = int(mine)
		}
	}
	return info, nil
}

// JobFailureReasonStat 失败原因统计。
type JobFailureReasonStat struct {
	Reason     string `json:"reason"`
	Count      int64  `json:"count"`
	LatestTime string `json:"latest_time"`
}

// GetJobFailureReasonTopK 返回最近 since 时间内任务失败原因 Top K。
// since<=0 时默认 1 小时；topK<=0 时默认 10。
func (r *RunJob) GetJobFailureReasonTopK(since time.Duration, topK int, ctx context.Context) ([]JobFailureReasonStat, error) {
	if since <= 0 {
		since = time.Hour
	}
	if topK <= 0 {
		topK = 10
	}
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
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
	tx := db.GetGormDb().WithContext(ctx).Table(JobRecordTable).
		Select("LEFT(run_err, 80) AS reason, COUNT(*) AS cnt, MAX(create_time) AS latest").
		Where("create_time > ?", cutoff).
		Where("job_result_flag IN ?", []string{"failed", "panic"}).
		Where("run_err <> ''").
		Group("reason").
		Order("cnt DESC").
		Limit(topK).
		Find(&rows)
	if tx.Error != nil {
		return nil, tx.Error
	}
	result := make([]JobFailureReasonStat, 0, len(rows))
	for _, r2 := range rows {
		result = append(result, JobFailureReasonStat{
			Reason:     r2.Reason,
			Count:      r2.Count,
			LatestTime: r2.LatestTime.Format(time.RFC3339),
		})
	}
	return result, nil
}

// JobSlowStat 慢任务统计。
type JobSlowStat struct {
	JobName    string `json:"job_name"`
	JobType    string `json:"job_type"`
	Duration   int64  `json:"duration_ms"`
	Status     string `json:"status"`
	CreateTime string `json:"create_time"`
}

// GetSlowJobsTopK 返回最近 since 时间内耗时最高的 K 个任务。
func (r *RunJob) GetSlowJobsTopK(since time.Duration, topK int, ctx context.Context) ([]JobSlowStat, error) {
	if since <= 0 {
		since = time.Hour
	}
	if topK <= 0 {
		topK = 10
	}
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-since)
	type row struct {
		JobName    string    `gorm:"column:job_name"`
		JobType    string    `gorm:"column:job_type"`
		Duration   int64     `gorm:"column:duration_ms"`
		Status     string    `gorm:"column:job_result_flag"`
		CreateTime time.Time `gorm:"column:create_time"`
	}
	rows := make([]row, 0)
	tx := db.GetGormDb().WithContext(ctx).Table(JobRecordTable).
		Select("job_name, job_type, duration_ms, job_result_flag, create_time").
		Where("create_time > ?", cutoff).
		Order("duration_ms DESC").
		Limit(topK).
		Find(&rows)
	if tx.Error != nil {
		return nil, tx.Error
	}
	result := make([]JobSlowStat, 0, len(rows))
	for _, r2 := range rows {
		result = append(result, JobSlowStat{
			JobName:    r2.JobName,
			JobType:    r2.JobType,
			Duration:   r2.Duration,
			Status:     r2.Status,
			CreateTime: r2.CreateTime.Format(time.RFC3339),
		})
	}
	return result, nil
}

// JobsExecStats 任务执行统计（默认最近 5 分钟）。
type JobsExecStats struct {
	TotalExecutions int   `json:"total_executions"`
	SuccessNum      int   `json:"success_num"`
	FailedNum       int   `json:"failed_num"`
	PanicNum        int   `json:"panic_num"`
	SuccessRate     int   `json:"success_rate"`
	AvgDuration     int64 `json:"avg_duration_ms"`
	P50Duration     int64 `json:"p50_duration_ms"`
	P95Duration     int64 `json:"p95_duration_ms"`
	P99Duration     int64 `json:"p99_duration_ms"`
	MaxDuration     int64 `json:"max_duration_ms"`
}

// JobStats 任务总数统计（单次 DB 聚合查询）。
type JobStats struct {
	Total    int64            `json:"total"`
	Enabled  int64            `json:"enabled"`
	Disabled int64            `json:"disabled"`
	Running  int64            `json:"running"`
	ByType   map[string]int64 `json:"by_type"` // job_type → count
}

// GetJobStats 一次性返回任务总数/启用数/运行数/按类型分布。
// 只做 2 次轻量 GROUP BY 查询，避免多次 List 的 N+1 问题。
func (r *RunJob) GetJobStats(ctx context.Context) (*JobStats, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return nil, err
	}

	stats := &JobStats{ByType: map[string]int64{}}

	// 查询 1: 按 run_status + enabled 分组（一次得到 total/enabled/disabled/running）
	type countRow struct {
		Status  string
		Enabled bool
		Count   int64
	}
	rows := make([]countRow, 0)
	err = db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Select("run_status as status, enabled, COUNT(*) as count").
		Group("run_status, enabled").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		stats.Total += row.Count
		if row.Enabled {
			stats.Enabled += row.Count
		} else {
			stats.Disabled += row.Count
		}
		if row.Status == RunStatusRunning {
			stats.Running += row.Count
		}
	}

	// 查询 2: 按 job_type 分组
	type typeRow struct {
		JobType string
		Count   int64
	}
	typeRows := make([]typeRow, 0)
	err = db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
		Select("job_type, COUNT(*) as count").
		Group("job_type").Scan(&typeRows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range typeRows {
		stats.ByType[row.JobType] = row.Count
	}

	return stats, nil
}

// GetJobsExecStats 返回最近 since 时间内的任务执行统计。默认 since=5min。
func (r *RunJob) GetJobsExecStats(since time.Duration, ctx context.Context) (*JobsExecStats, error) {
	if since <= 0 {
		since = 5 * time.Minute
	}
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return nil, err
	}
	type row struct {
		Flag     string `gorm:"column:job_result_flag"`
		Duration int64  `gorm:"column:duration_ms"`
	}
	rows := make([]row, 0)
	cutoff := time.Now().Add(-since)
	tx := db.GetGormDb().WithContext(ctx).Table(JobRecordTable).
		Select("job_result_flag, duration_ms").
		Where("create_time > ?", cutoff).
		Limit(5000).
		Find(&rows)
	if tx.Error != nil {
		return nil, tx.Error
	}
	stats := &JobsExecStats{}
	if len(rows) == 0 {
		return stats, nil
	}
	durations := make([]int64, 0, len(rows))
	var total int64
	for _, r2 := range rows {
		switch r2.Flag {
		case "success":
			stats.SuccessNum++
		case "failed":
			stats.FailedNum++
		case "panic":
			stats.PanicNum++
			stats.FailedNum++
		}
		durations = append(durations, r2.Duration)
		total += r2.Duration
	}
	stats.TotalExecutions = len(rows)
	if stats.TotalExecutions > 0 {
		stats.AvgDuration = total / int64(stats.TotalExecutions)
		stats.SuccessRate = stats.SuccessNum * 100 / stats.TotalExecutions
	}
	stats.P50Duration = jobsCalcPercentile(durations, 0.50)
	stats.P95Duration = jobsCalcPercentile(durations, 0.95)
	stats.P99Duration = jobsCalcPercentile(durations, 0.99)
	stats.MaxDuration = jobsCalcPercentile(durations, 1.0)
	return stats, nil
}

// jobsCalcPercentile 计算分位数。
func jobsCalcPercentile(values []int64, p float64) int64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	// 插入排序即可，数据量 < 5000
	sorted := make([]int64, n)
	copy(sorted, values)
	for i := 1; i < n; i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	idx := int(float64(n) * p)
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}
