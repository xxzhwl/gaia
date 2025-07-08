// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"fmt"
	"github.com/robfig/cron/v3"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"sync"
	"time"
)

type RunJob struct {
	instanceLogger *logImpl.DefaultLogger
	cronJobLogger  *logImpl.DefaultLogger
	cronHookLogger *logImpl.DefaultLogger

	currentCronServiceJobMap map[int64]cronJob
	currentCronHookJobMap    map[int64]cronJob
	currentServiceJobIds     []int64
	currentHookJobIds        []int64
	cronScheduler            *cron.Cron

	dbSchema string

	once sync.Once
}

func NewRunJob() *RunJob {
	return &RunJob{
		dbSchema:       "Framework.Mysql",
		instanceLogger: logImpl.NewDefaultLogger().SetTitle("Jobs"),
		cronJobLogger:  logImpl.NewDefaultLogger().SetTitle("CronJob"),
		cronHookLogger: logImpl.NewDefaultLogger().SetTitle("CronHook"),
		cronScheduler: cron.New(cron.WithChain(
			cron.DelayIfStillRunning(CronLogger{logger: logImpl.NewDefaultLogger().SetTitle(
				"DelayIfStillRunning")}))),
		currentCronServiceJobMap: make(map[int64]cronJob),
		currentCronHookJobMap:    make(map[int64]cronJob),
		currentServiceJobIds:     make([]int64, 0),
		currentHookJobIds:        make([]int64, 0),
	}
}

func NewSecondJobs() *RunJob {
	return &RunJob{
		dbSchema:       "Framework.Mysql",
		instanceLogger: logImpl.NewDefaultLogger().SetTitle("Jobs"),
		cronJobLogger:  logImpl.NewDefaultLogger().SetTitle("CronJob"),
		cronHookLogger: logImpl.NewDefaultLogger().SetTitle("CronHook"),
		cronScheduler: cron.New(cron.WithSeconds(),
			cron.WithChain(cron.DelayIfStillRunning(cron.DefaultLogger))),
		currentCronServiceJobMap: make(map[int64]cronJob),
		currentCronHookJobMap:    make(map[int64]cronJob),
		currentServiceJobIds:     make([]int64, 0),
		currentHookJobIds:        make([]int64, 0),
	}
}

func (r *RunJob) WithDbSchema(dbSchema string) *RunJob {
	r.dbSchema = dbSchema
	return r
}

// Run Jobs服务启动
// 这是一个for循环常驻服务，要不断执行
func (r *RunJob) Run() {
	r.once.Do(r.run)
}

func (r *RunJob) run() {
	gaia.BuildContextTrace()
	defer gaia.RemoveContextTrace()
	banList := gaia.GetSafeConfStringSliceFromString("Jobs.BanEnvList")
	//禁止启动的环境
	if gaia.InList(gaia.GetEnvFlag(), banList) {
		gaia.Log(gaia.LogWarnLevel, fmt.Sprintf("Jobs is ban in current env[%s]", gaia.GetEnvFlag()))
		return
	}
	r.cronScheduler.Start()
	for {
		if err := r.updateJobs(); err != nil {
			gaia.SendSystemAlarm("JobsErr", err.Error())
			r.instanceLogger.ErrorF("Update jobs error: %s", err.Error())
		}
		time.Sleep(5 * time.Second)
	}
}

func (r *RunJob) Stop() {
	//先暂停cron调度器
	r.cronScheduler.Stop()
	//所有任务状态置为等待中
	if err := r.UpdateAllJobToWait(); err != nil {
		gaia.SendSystemAlarm("JobsErr", "StopJobsError:"+err.Error())
		r.instanceLogger.ErrorF("Stop jobs error: %s", err.Error())
	}
}
