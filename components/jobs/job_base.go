// Package jobs 定时或常驻任务中心
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"database/sql"
	"github.com/robfig/cron/v3"
	"github.com/xxzhwl/gaia"
	"time"
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
	Id          int64
	LastRunTime sql.NullTime
	CreateTime  time.Time
	UpdateTime  time.Time
	RunStatus   string
	Enabled     bool
	JobBase
}

// jobRecord 任务执行信息
type jobRecord struct {
	Id            int64
	CreateTime    time.Time
	UpdateTime    time.Time
	JobResultFlag string
	RunResult     string
	RunErr        string
	JobBase
}

func (r *RunJob) updateJobs() error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	var jobs []job
	tx := db.GetGormDb().Table(JobCenterTable).Find(&jobs)
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
	tx := db.GetGormDb().Table(JobCenterTable).Where("id = ?", job.Id).Update("run_status", RunStatusWait).
		UpdateColumn("update_time", gaia.Date(gaia.DateTimeFormat))
	if tx.Error != nil {
		return tx.Error
	}
	jobErrs, resInfo := "", ""
	flag := "success"
	if v, ok := res.(error); ok {
		jobErrs = v.Error()
		flag = "failed"
	} else {
		resInfo = gaia.MustPrettyString(res)
	}
	record := jobRecord{
		JobBase: JobBase{
			JobName:       job.JobName,
			JobType:       job.JobType,
			CronExpr:      job.CronExpr,
			HookUrl:       job.HookUrl,
			ServiceName:   job.ServiceName,
			ServiceMethod: job.ServiceMethod,
			Args:          job.Args,
		},
		CreateTime:    time.Now(),
		UpdateTime:    time.Now(),
		JobResultFlag: flag,
		RunErr:        jobErrs,
		RunResult:     resInfo,
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
	JobName       string `require:"1"`
	JobType       string `range:"cron_job,cron_hook,back_job"`
	CronExpr      string `require:"1"`
	HookUrl       string
	ServiceName   string
	ServiceMethod string
	Args          []byte
	Timeout       int // 任务执行超时时间（秒），默认 300 秒（5分钟）
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
	tempJob := job{
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
		Enabled:    item.Enable,
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
	tempJob := job{
		Id:      item.JobId,
		Enabled: item.Enable,
		JobBase: item.JobBase,
	}
	tx := db.GetGormDb().Table(JobCenterTable).Where("id=?", item.JobId).Updates(&tempJob)
	if tx.Error != nil {
		return -1, tx.Error
	}
	return tx.RowsAffected, nil
}

func (r *RunJob) UpdateAllJobToWait() error {
	db, err := gaia.NewMysqlWithSchema(r.dbSchema)
	if err != nil {
		return err
	}
	tx := db.GetGormDb().Table(JobCenterTable).Where("id > 0").UpdateColumn("run_status", RunStatusWait)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}
