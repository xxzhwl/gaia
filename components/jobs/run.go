// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/gexit"
)

type RunJob struct {
	exitContext context.Context

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

	// 并发安全锁
	jobMapLock sync.RWMutex // 保护 currentCronServiceJobMap 和 currentCronHookJobMap
	jobIdsLock sync.RWMutex // 保护 currentServiceJobIds 和 currentHookJobIds
}

func NewRunJob() *RunJob {
	return &RunJob{
		exitContext:    gexit.GetExitContext(),
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
		exitContext:    gexit.GetExitContext(),
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
		select {
		case <-r.exitContext.Done():
			gaia.InfoF("Received exit signal,RunJob shutting down")
			r.Stop()
			return
		case <-time.After(5 * time.Second):
			if err := r.updateJobs(); err != nil {
				gaia.SendSystemAlarm("JobsErr", err.Error())
				r.instanceLogger.ErrorF("Update jobs error: %s", err.Error())
			}
		}
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
