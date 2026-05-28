// Package jobs 定时或常驻任务中心
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/xxzhwl/gaia"
)

const (
	CronJob  = "cron_job"
	CronHook = "cron_hook"

	JobCenterTable   = "job_center"
	JobRecordTable   = "job_record"
	RunStatusWait    = "待运行"
	RunStatusRunning = "运行中"
)

type cronJob struct {
	EntryId cron.EntryID
	job
}

// job 任务基础信息
type job struct {
	Id                int64        `gorm:"column:id;primaryKey;autoIncrement"`
	LastRunTime       sql.NullTime `gorm:"column:last_run_time"`
	CreateTime        time.Time    `gorm:"column:create_time"`
	UpdateTime        time.Time    `gorm:"column:update_time"`
	RunStatus         string       `gorm:"column:run_status;size:32;not null;default:待运行"`
	Enabled           bool         `gorm:"column:enabled;not null;default:false"`
	LeaseOwner        string       `gorm:"column:lease_owner;size:96;not null;default:''"`
	LeaseExpireAt     sql.NullTime `gorm:"column:lease_expire_at"`
	LastHeartbeatTime sql.NullTime `gorm:"column:last_heartbeat_time"`
	JobBase
}

func (job) TableName() string { return JobCenterTable }

// jobRecord 任务执行信息
type jobRecord struct {
	Id            int64     `gorm:"column:id;primaryKey;autoIncrement"`
	CreateTime    time.Time `gorm:"column:create_time"`
	UpdateTime    time.Time `gorm:"column:update_time"`
	JobResultFlag string    `gorm:"column:job_result_flag;size:32;not null;default:''"`
	RunResult     string    `gorm:"column:run_result;type:text"`
	RunErr        string    `gorm:"column:run_err;type:text"`
	DurationMs    int64     `gorm:"column:duration_ms;not null;default:0"`          // 执行耗时（毫秒）
	InstanceId    string    `gorm:"column:instance_id;size:96;not null;default:''"` // 执行该次任务的进程实例ID
	JobBase
}

func (jobRecord) TableName() string { return JobRecordTable }

// runStartTimeStore 在调度线程中记录任务执行起始时间，用于 jobRecord 计算 duration。
var (
	runStartTimeStore   = map[int64]time.Time{}
	runStartTimeStoreMu sync.RWMutex
)

// noteJobStart 记录某个 job 本次执行的起始时间。
func noteJobStart(jobId int64, t time.Time) {
	runStartTimeStoreMu.Lock()
	runStartTimeStore[jobId] = t
	runStartTimeStoreMu.Unlock()
}

// popJobStart 取出并清除某个 job 的起始时间。
func popJobStart(jobId int64) (time.Time, bool) {
	runStartTimeStoreMu.Lock()
	defer runStartTimeStoreMu.Unlock()
	t, ok := runStartTimeStore[jobId]
	if ok {
		delete(runStartTimeStore, jobId)
	}
	return t, ok
}

func (r *RunJob) updateJobs() error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	var jobs []job
	systemName := gaia.GetSystemEnName()
	tx := db.GetGormDb().Table(JobCenterTable).
		Where("system_name = ? OR system_name = ''", systemName).
		Find(&jobs)
	if tx.Error != nil {
		return tx.Error
	}
	cronHookJobs, cronServiceJobs := []job{}, []job{}
	for _, j := range jobs {
		if j.JobType == CronJob {
			cronServiceJobs = append(cronServiceJobs, j)
			continue
		}
		if j.JobType == CronHook {
			cronHookJobs = append(cronHookJobs, j)
			continue
		}
	}
	if err := r.updateCronServiceJobs(cronServiceJobs); err != nil {
		return err
	}
	if err := r.updateCronHookJobs(cronHookJobs); err != nil {
		return err
	}
	return nil
}

func (r *RunJob) updateJobToRunning(job job) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	return db.GetGormDb().Table(JobCenterTable).Where("id = ?", job.Id).Updates(map[string]interface{}{
		"run_status":    RunStatusRunning,
		"last_run_time": time.Now(),
	}).Error
}

func (r *RunJob) updateJobToWaitWithRes(job job, res any) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	// HA 开启时，只释放本实例持有的租约；同时清空 owner / lease_expire_at。
	now := time.Now()
	updates := map[string]interface{}{
		"run_status":  RunStatusWait,
		"update_time": gaia.Date(gaia.DateTimeFormat),
	}
	if !r.haDisabled {
		updates["lease_owner"] = ""
		updates["lease_expire_at"] = nil
		tx := db.GetGormDb().Table(JobCenterTable).
			Where("id = ?", job.Id).
			Where("lease_owner = ? OR lease_owner = ''", GetInstanceId()).
			Updates(updates)
		if tx.Error != nil {
			return tx.Error
		}
	} else {
		tx := db.GetGormDb().Table(JobCenterTable).Where("id = ?", job.Id).Updates(updates)
		if tx.Error != nil {
			return tx.Error
		}
	}
	jobErrs, resInfo := "", ""
	flag := "success"
	if v, ok := res.(error); ok {
		jobErrs = v.Error()
		flag = "failed"
	} else {
		resInfo = gaia.MustPrettyString(res)
	}
	durationMs := int64(0)
	if st, ok := popJobStart(job.Id); ok {
		durationMs = time.Since(st).Milliseconds()
	}
	record := jobRecord{
		JobBase: JobBase{
			SystemName:    job.SystemName,
			JobName:       job.JobName,
			JobType:       job.JobType,
			CronExpr:      job.CronExpr,
			HookUrl:       job.HookUrl,
			ServiceName:   job.ServiceName,
			ServiceMethod: job.ServiceMethod,
			Args:          job.Args,
		},
		CreateTime:    now,
		UpdateTime:    now,
		JobResultFlag: flag,
		RunErr:        jobErrs,
		RunResult:     resInfo,
		DurationMs:    durationMs,
		InstanceId:    GetInstanceId(),
	}
	return db.GetGormDb().Table(JobRecordTable).Create(&record).Error
}

// updateJobToWaitWithResAndPanic 更新任务状态并记录panic执行记录
func (r *RunJob) updateJobToWaitWithResAndPanic(job job, panicMsg string) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	now := time.Now()
	updates := map[string]interface{}{
		"run_status":  RunStatusWait,
		"update_time": gaia.Date(gaia.DateTimeFormat),
	}
	if !r.haDisabled {
		updates["lease_owner"] = ""
		updates["lease_expire_at"] = nil
		tx := db.GetGormDb().Table(JobCenterTable).
			Where("id = ?", job.Id).
			Where("lease_owner = ? OR lease_owner = ''", GetInstanceId()).
			Updates(updates)
		if tx.Error != nil {
			return tx.Error
		}
	} else {
		tx := db.GetGormDb().Table(JobCenterTable).Where("id = ?", job.Id).Updates(updates)
		if tx.Error != nil {
			return tx.Error
		}
	}

	durationMs := int64(0)
	if st, ok := popJobStart(job.Id); ok {
		durationMs = time.Since(st).Milliseconds()
	}
	record := jobRecord{
		JobBase: JobBase{
			SystemName:    job.SystemName,
			JobName:       job.JobName,
			JobType:       job.JobType,
			CronExpr:      job.CronExpr,
			HookUrl:       job.HookUrl,
			ServiceName:   job.ServiceName,
			ServiceMethod: job.ServiceMethod,
			Args:          job.Args,
		},
		CreateTime:    now,
		UpdateTime:    now,
		JobResultFlag: "panic",
		RunErr:        "[PANIC] " + panicMsg,
		RunResult:     "",
		DurationMs:    durationMs,
		InstanceId:    GetInstanceId(),
	}
	return db.GetGormDb().Table(JobRecordTable).Create(&record).Error
}

func (r *RunJob) jobIsRunning(curJob job) (bool, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return false, err
	}
	var jobTemp job
	tx := db.GetGormDb().Table(JobCenterTable).Where("id = ?", curJob.Id).First(&jobTemp)
	if tx.Error != nil {
		return false, tx.Error
	}
	if jobTemp.RunStatus == RunStatusRunning {
		return true, nil
	}
	return false, nil
}

func (r *RunJob) jobIsWait(curJob job) (bool, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return false, err
	}
	var jobTemp job
	tx := db.GetGormDb().Table(JobCenterTable).Where("id = ?", curJob.Id).First(&jobTemp)
	if tx.Error != nil {
		return false, tx.Error
	}
	if jobTemp.RunStatus == RunStatusWait {
		return true, nil
	}
	return false, nil
}

type JobBase struct {
	SystemName    string `gorm:"column:system_name;size:64;not null;default:'';index:idx_job_center_system_name"`
	JobName       string `gorm:"column:job_name;size:64;not null;default:''" require:"1"`
	JobType       string `gorm:"column:job_type;size:64;not null;default:''" range:"cron_job,cron_hook,back_job"`
	CronExpr      string `gorm:"column:cron_expr;size:32;not null;default:''" require:"1"`
	HookUrl       string `gorm:"column:hook_url;size:512;not null;default:''"`
	ServiceName   string `gorm:"column:service_name;size:128;not null;default:''"`
	ServiceMethod string `gorm:"column:service_method;size:64;not null;default:''"`
	Args          []byte `gorm:"column:args;type:longtext"`
	Timeout       int    `gorm:"column:timeout"` // 任务执行超时时间（秒），默认 300 秒（5分钟）
}

// GetTimeout 获取任务超时时间，默认 5 分钟
func (j JobBase) GetTimeout() int {
	if j.Timeout <= 0 {
		return 300 // 默认 5 分钟
	}
	return j.Timeout
}

type AddJobArgs struct {
	JobBase
	Enable bool
}

type UpdateJobArgs struct {
	JobId int64
	JobBase
	Enable bool
}

func (r *RunJob) AddJob(item AddJobArgs) (int64, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return -1, err
	}
	item.SystemName = normalizeSystemName(item.SystemName)
	tempJob := job{
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
		Enabled:    item.Enable,
		RunStatus:  RunStatusWait, // 显式赋初值，避免 GORM 零值写入空串导致租约抢占判断异常
		JobBase:    item.JobBase,
	}
	tx := db.GetGormDb().Table(JobCenterTable).Create(&tempJob)
	if tx.Error != nil {
		return -1, tx.Error
	}
	return tempJob.Id, nil
}

func (r *RunJob) RemoveJob(id int64) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	tx := db.GetGormDb().Table(JobCenterTable).Where("id = ?", id).Delete(&job{})
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}

func (r *RunJob) UpdateJob(item UpdateJobArgs) (int64, error) {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return -1, err
	}
	item.SystemName = normalizeSystemName(item.SystemName)
	updates := map[string]any{
		"system_name":    item.SystemName,
		"job_name":       item.JobName,
		"job_type":       item.JobType,
		"cron_expr":      item.CronExpr,
		"hook_url":       item.HookUrl,
		"service_name":   item.ServiceName,
		"service_method": item.ServiceMethod,
		"args":           item.Args,
		"timeout":        item.Timeout,
		"enabled":        item.Enable,
		"update_time":    time.Now(),
	}
	tx := db.GetGormDb().Table(JobCenterTable).Where("id=?", item.JobId).Updates(updates)
	if tx.Error != nil {
		return -1, tx.Error
	}
	return tx.RowsAffected, nil
}

// registerBuiltinJobs scans CronServiceMap and inserts any {system}/{service}/{method}
// combination not already present in job_center as a disabled cron_job.
func (r *RunJob) registerBuiltinJobs(ctx context.Context) error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}

	for _, svc := range ListRegisteredCronServices("") {
		if !svc.Executable {
			continue
		}
		for _, method := range svc.Methods {
			var existing job
			tx := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).
				Where("system_name = ? AND service_name = ? AND service_method = ?",
					svc.SystemName, svc.ServiceName, method).
				First(&existing)
			if tx.Error == nil {
				continue
			}
			if !errors.Is(tx.Error, gorm.ErrRecordNotFound) {
				return tx.Error
			}
			jobName := fmt.Sprintf("%s.%s", svc.ServiceName, method)
			tempJob := job{
				CreateTime: time.Now(),
				UpdateTime: time.Now(),
				Enabled:    false,
				RunStatus:  RunStatusWait,
				JobBase: JobBase{
					SystemName:    svc.SystemName,
					JobName:       jobName,
					JobType:       CronJob,
					CronExpr:      "",
					ServiceName:   svc.ServiceName,
					ServiceMethod: method,
					Timeout:       300,
				},
			}
			if err := db.GetGormDb().WithContext(ctx).Table(JobCenterTable).Create(&tempJob).Error; err != nil {
				r.instanceLogger.WarnF("auto register job %s/%s/%s failed: %s", svc.SystemName, svc.ServiceName, method, err.Error())
			} else {
				r.instanceLogger.InfoF("auto registered job: %s/%s/%s", svc.SystemName, svc.ServiceName, method)
			}
		}
	}
	return nil
}
