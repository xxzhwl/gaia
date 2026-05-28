// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
)

func (r *RunJob) updateCronHookJobs(jobs []job) error {
	jobsId := make([]int64, 0)
	jobsTemp := make(map[int64]cronJob)
	for _, j := range jobs {
		jobsId = append(jobsId, j.Id)
		jobsTemp[j.Id] = cronJob{
			EntryId: 0,
			job:     j,
		}
	}

	r.jobIdsLock.RLock()
	currentIds := make([]int64, len(r.currentHookJobIds))
	copy(currentIds, r.currentHookJobIds)
	r.jobIdsLock.RUnlock()

	newJobsId := make([]int64, 0)
	needAddJobsId, needDeleteJobsId := gaia.DifferenceList(jobsId, currentIds)
	interSectJobsId := gaia.IntersectList(jobsId, currentIds)

	needAddJobs, needDeleteJobs := make(map[int64]cronJob), make(map[int64]cronJob)
	for _, addJobId := range needAddJobsId {
		if v, ok := jobsTemp[addJobId]; ok {
			if v.Enabled {
				r.cronHookLogger.InfoF("新增任务%d", v.job.Id)
				needAddJobs[addJobId] = v
			}
		}
	}

	r.jobMapLock.RLock()
	for _, deleteJobId := range needDeleteJobsId {
		if v, ok := r.currentCronHookJobMap[deleteJobId]; ok {
			r.cronHookLogger.InfoF("任务%d因被删除而移除", v.job.Id)
			needDeleteJobs[deleteJobId] = v
		}
	}

	for _, updateJobsId := range interSectJobsId {
		if v, ok := r.currentCronHookJobMap[updateJobsId]; ok {
			if newJob, ok := jobsTemp[updateJobsId]; ok {
				if !newJob.Enabled {
					r.cronHookLogger.InfoF("任务%d因被关闭而移除", v.job.Id)
					needDeleteJobs[updateJobsId] = v
					continue
				}
				if newJob.CronExpr != v.CronExpr || newJob.ServiceMethod != v.ServiceMethod || string(newJob.Args) != string(v.Args) {
					needDeleteJobs[updateJobsId] = v
					needAddJobs[updateJobsId] = newJob
					r.cronHookLogger.InfoF("任务%d发生变更\n%+v\n->\n%+v", v.job.Id, v, newJob)
				}
			}
		}
	}
	r.jobMapLock.RUnlock()

	for _, job := range needDeleteJobs {
		r.deleteCronHookJob(job)
	}

	for _, job := range needAddJobs {
		if err := r.addCronHookJob(job); err != nil {
			r.cronHookLogger.Error(err.Error())
			r.fireAlarm("AddCronHookJob", err.Error())
			continue
		}
	}

	r.jobMapLock.RLock()
	for i := range r.currentCronHookJobMap {
		newJobsId = append(newJobsId, i)
	}
	r.jobMapLock.RUnlock()

	r.jobIdsLock.Lock()
	r.currentHookJobIds = newJobsId
	r.jobIdsLock.Unlock()
	return nil
}

func (r *RunJob) deleteCronHookJob(job cronJob) {
	r.cronScheduler.Remove(job.EntryId)
	r.jobMapLock.Lock()
	delete(r.currentCronHookJobMap, job.Id)
	r.jobMapLock.Unlock()
}

func (r *RunJob) addCronHookJob(job cronJob) error {
	entryId, err := r.cronScheduler.AddFunc(job.CronExpr, func() {
		r.DoCronHookJob(job)
	})
	if err != nil {
		return fmt.Errorf("添加任务失败%s", err.Error())
	}
	r.jobMapLock.Lock()
	r.currentCronHookJobMap[job.Id] = cronJob{
		EntryId: entryId,
		job:     job.job,
	}
	r.jobMapLock.Unlock()
	return nil
}

func (r *RunJob) DoCronHookJob(job cronJob) {
	gaia.BuildContextTrace()
	defer gaia.RemoveContextTrace()

	timeout := time.Duration(job.job.GetTimeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// HA 抢占：多副本下仅抢到租约的副本才实际发 webhook，避免下游被重复调用。
	if !r.haDisabled {
		leaseDur := computeLeaseDuration(job.JobBase)
		acquireCtx, acquireCancel := context.WithTimeout(context.Background(), 5*time.Second)
		ok, err := r.tryAcquireJobLease(acquireCtx, job.Id, leaseDur)
		acquireCancel()
		if err != nil {
			r.cronHookLogger.ErrorF("抢占任务%d-%s租约失败:%s", job.Id, job.JobName, err.Error())
			r.counters.dbError.Add(1)
			recordJobDBError(context.Background(), "acquire_lease")
			return
		}
		recordLeaseAcquire(context.Background(), job.JobBase, ok)
		if !ok {
			r.cronHookLogger.InfoF("任务%d-%s未抢到租约（可能被其他副本执行中），跳过本轮", job.Id, job.JobName)
			r.fireJobEnd(job, time.Now(), time.Now(), "skip", "lease acquired by other instance", false, false)
			return
		}
		stopHb := r.startLeaseHeartbeat(context.Background(), job.Id, leaseDur)
		defer stopHb()
	}

	startTime := time.Now()
	r.fireJobStart(job, startTime)

	type result struct {
		resInfo  string
		err      error
		isPanic  bool
		panicMsg string
	}
	resultChan := make(chan result, 1)

	r.runningWg.Add(1)
	go func() {
		defer r.runningWg.Done()
		defer func() {
			if rr := recover(); rr != nil {
				panicErr := gaia.PanicLog(rr)
				r.cronHookLogger.ErrorF("任务%d-%s执行panic: %s", job.Id, job.JobName, panicErr)
				r.fireAlarm(fmt.Sprintf("JobHookPanic[%s]", job.JobName),
					fmt.Sprintf("任务ID: %d, 任务名称: %s, Panic信息: %s", job.Id, job.JobName, panicErr))
				resultChan <- result{"", fmt.Errorf("任务执行 panic: %s", panicErr), true, panicErr}
			}
		}()

		if r.haDisabled {
			if v, err := r.jobIsRunning(job.job); err != nil {
				r.cronHookLogger.ErrorF("获取任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				r.counters.dbError.Add(1)
				recordJobDBError(context.Background(), "is_running")
				resultChan <- result{"", err, false, ""}
				return
			} else if v {
				r.cronHookLogger.InfoF("任务%d-%s正在运行中,跳过此次运行", job.Id, job.JobName)
				resultChan <- result{"", fmt.Errorf("任务正在运行中"), false, ""}
				return
			}

			if err := r.updateJobToRunning(job.job); err != nil {
				r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				r.counters.dbError.Add(1)
				recordJobDBError(context.Background(), "to_running")
				resultChan <- result{"", err, false, ""}
				return
			}
		}

		resInfo, _, errTemp := httpclient.NewHttpRequest(job.HookUrl).
			WithContext(ctx).
			WithMethod(http.MethodPost).
			WithBody(job.Args).Do()
		resultChan <- result{string(resInfo), errTemp, false, ""}
	}()

	select {
	case <-ctx.Done():
		r.cronHookLogger.ErrorF("任务%d-%s执行超时(>%v)", job.Id, job.JobName, timeout)
		timeoutErr := fmt.Errorf("任务执行超时(>%v)", timeout)
		r.fireAlarm(fmt.Sprintf("JobHookTimeout[%s]", job.JobName),
			fmt.Sprintf("任务ID: %d, 任务名称: %s, 超时时间: %v", job.Id, job.JobName, timeout))
		if err := r.updateJobToWaitWithRes(job.job, timeoutErr); err != nil {
			r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
		}
		r.fireJobEnd(job, startTime, time.Now(), "timeout", timeoutErr.Error(), false, true)
		return
	case result := <-resultChan:
		endTime := time.Now()
		if result.err != nil {
			errStr := result.err.Error()
			if errStr == "任务正在运行中" {
				r.fireJobEnd(job, startTime, endTime, "skip", errStr, false, false)
				return
			}
			if result.isPanic {
				if err := r.updateJobToWaitWithResAndPanic(job.job, result.panicMsg); err != nil {
					r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				}
				r.fireJobEnd(job, startTime, endTime, "panic", result.panicMsg, true, false)
			} else {
				if err := r.updateJobToWaitWithRes(job.job, result.err); err != nil {
					r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				}
				r.fireJobEnd(job, startTime, endTime, "failed", errStr, false, false)
			}
			return
		}
		if err := r.updateJobToWaitWithRes(job.job, result.resInfo); err != nil {
			r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			r.fireJobEnd(job, startTime, endTime, "failed", err.Error(), false, false)
			return
		}
		r.fireJobEnd(job, startTime, endTime, "success", "", false, false)
	}
}
