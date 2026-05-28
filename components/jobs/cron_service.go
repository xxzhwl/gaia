// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
)

func (r *RunJob) updateCronServiceJobs(jobs []job) error {
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
	currentIds := make([]int64, len(r.currentServiceJobIds))
	copy(currentIds, r.currentServiceJobIds)
	r.jobIdsLock.RUnlock()

	newJobsId := make([]int64, 0)
	needAddJobsId, needDeleteJobsId := gaia.DifferenceList(jobsId, currentIds)

	interSectJobsId := gaia.IntersectList(jobsId, currentIds)

	needAddJobs, needDeleteJobs := make(map[int64]cronJob), make(map[int64]cronJob)
	for _, addJobId := range needAddJobsId {
		if v, ok := jobsTemp[addJobId]; ok {
			if v.Enabled {
				r.cronJobLogger.InfoF("新增任务%d", v.job.Id)
				needAddJobs[addJobId] = v
			}
		}
	}

	r.jobMapLock.RLock()
	for _, deleteJobId := range needDeleteJobsId {
		if v, ok := r.currentCronServiceJobMap[deleteJobId]; ok {
			r.cronJobLogger.InfoF("任务%d因被删除而移除", v.job.Id)
			needDeleteJobs[deleteJobId] = v
		}
	}

	for _, updateJobsId := range interSectJobsId {
		if v, ok := r.currentCronServiceJobMap[updateJobsId]; ok {
			if newJob, ok := jobsTemp[updateJobsId]; ok {
				if !newJob.Enabled {
					r.cronJobLogger.InfoF("任务%d因被关闭而移除", v.job.Id)
					needDeleteJobs[updateJobsId] = v
					continue
				}
				if newJob.CronExpr != v.CronExpr || newJob.ServiceMethod != v.ServiceMethod || string(newJob.Args) != string(v.Args) {
					needDeleteJobs[updateJobsId] = v
					needAddJobs[updateJobsId] = newJob
					r.cronJobLogger.InfoF("任务%d发生变更\n%+v\n->\n%+v", v.job.Id, v, newJob)
				}
			}
		}
	}
	r.jobMapLock.RUnlock()

	for _, job := range needDeleteJobs {
		r.deleteCronServiceJob(job)
	}

	for _, job := range needAddJobs {
		if err := r.addCronServiceJob(job); err != nil {
			r.cronJobLogger.ErrorF("添加任务失败:%s", err.Error())
			r.fireAlarm("AddCronServiceJob", err.Error())
			continue
		}
	}

	r.jobMapLock.RLock()
	for i := range r.currentCronServiceJobMap {
		newJobsId = append(newJobsId, i)
	}
	r.jobMapLock.RUnlock()

	r.jobIdsLock.Lock()
	r.currentServiceJobIds = newJobsId
	r.jobIdsLock.Unlock()
	return nil
}

func (r *RunJob) deleteCronServiceJob(job cronJob) {
	r.cronScheduler.Remove(job.EntryId)
	r.jobMapLock.Lock()
	delete(r.currentCronServiceJobMap, job.Id)
	r.jobMapLock.Unlock()
}

func (r *RunJob) addCronServiceJob(job cronJob) error {
	service, err := GetCronServiceForSystem(job.SystemName, job.ServiceName)
	if err != nil {
		return err
	}
	entryId, err := r.cronScheduler.AddFunc(job.CronExpr, func() {
		r.DoCronServiceJob(service, job)
	})
	if err != nil {
		return fmt.Errorf("添加任务失败%s", err.Error())
	}
	r.jobMapLock.Lock()
	r.currentCronServiceJobMap[job.Id] = cronJob{
		EntryId: entryId,
		job:     job.job,
	}
	r.jobMapLock.Unlock()
	return nil
}

func (r *RunJob) DoCronServiceJob(service any, job cronJob) {
	gaia.BuildContextTrace()
	defer gaia.RemoveContextTrace()

	timeout := time.Duration(job.job.GetTimeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// HA 抢锁：多副本下仅 RowsAffected==1 的实例能进入后续逻辑。
	// 关闭 HA 时退化为旧 SELECT+UPDATE 逻辑。
	if !r.haDisabled {
		leaseDur := computeLeaseDuration(job.JobBase)
		acquireCtx, acquireCancel := context.WithTimeout(context.Background(), 5*time.Second)
		ok, err := r.tryAcquireJobLease(acquireCtx, job.Id, leaseDur)
		acquireCancel()
		if err != nil {
			r.cronJobLogger.ErrorF("抢占任务%d-%s租约失败:%s", job.Id, job.JobName, err.Error())
			r.counters.dbError.Add(1)
			recordJobDBError(context.Background(), "acquire_lease")
			return
		}
		recordLeaseAcquire(context.Background(), job.JobBase, ok)
		if !ok {
			// 被其他副本抢先 / 任务还在运行中：静默跳过，记 skip 指标。
			r.cronJobLogger.InfoF("任务%d-%s未抢到租约（可能被其他副本执行中），跳过本轮", job.Id, job.JobName)
			r.fireJobEnd(job, time.Now(), time.Now(), "skip", "lease acquired by other instance", false, false)
			return
		}
		// 抱锁成功起心跳续租。
		stopHb := r.startLeaseHeartbeat(context.Background(), job.Id, leaseDur)
		defer stopHb()
	}

	startTime := time.Now()
	r.fireJobStart(job, startTime)

	type result struct {
		res      any
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
				r.cronJobLogger.ErrorF("任务%d-%s执行panic: %s", job.Id, job.JobName, panicErr)
				r.fireAlarm(fmt.Sprintf("JobPanic[%s]", job.JobName),
					fmt.Sprintf("任务ID: %d, 任务名称: %s, Panic信息: %s", job.Id, job.JobName, panicErr))
				resultChan <- result{
					res:      nil,
					err:      errors.New("任务执行 panic: " + panicErr),
					isPanic:  true,
					panicMsg: panicErr,
				}
			}
		}()

		// 关闭 HA 时还使用旧逻辑。
		if r.haDisabled {
			if v, err := r.jobIsRunning(job.job); err != nil {
				r.cronJobLogger.ErrorF("获取任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				r.counters.dbError.Add(1)
				recordJobDBError(context.Background(), "is_running")
				resultChan <- result{nil, err, false, ""}
				return
			} else if v {
				r.cronJobLogger.InfoF("任务%d-%s正在运行中,跳过此次运行", job.Id, job.JobName)
				resultChan <- result{nil, errors.New("任务正在运行中"), false, ""}
				return
			}
			if err := r.updateJobToRunning(job.job); err != nil {
				r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				r.counters.dbError.Add(1)
				recordJobDBError(context.Background(), "to_running")
				resultChan <- result{nil, err, false, ""}
				return
			}
		}

		res, doErr := gaia.CallMethodWithArgs(service, job.ServiceMethod, job.Args)
		resultChan <- result{res, doErr, false, ""}
	}()

	select {
	case <-ctx.Done():
		r.cronJobLogger.ErrorF("任务%d-%s执行超时(>%v)", job.Id, job.JobName, timeout)
		timeoutErr := fmt.Errorf("任务执行超时(>%v)", timeout)
		r.fireAlarm(fmt.Sprintf("JobTimeout[%s]", job.JobName),
			fmt.Sprintf("任务ID: %d, 任务名称: %s, 超时时间: %v", job.Id, job.JobName, timeout))
		if err := r.updateJobToWaitWithRes(job.job, timeoutErr); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
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
					r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				}
				r.fireJobEnd(job, startTime, endTime, "panic", result.panicMsg, true, false)
			} else {
				if err := r.updateJobToWaitWithRes(job.job, result.err); err != nil {
					r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				}
				r.fireJobEnd(job, startTime, endTime, "failed", errStr, false, false)
			}
			return
		}
		if err := r.updateJobToWaitWithRes(job.job, result.res); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			r.fireJobEnd(job, startTime, endTime, "failed", err.Error(), false, false)
			return
		}
		r.fireJobEnd(job, startTime, endTime, "success", "", false, false)
	}
}

// fireJobStart 记录一次 job 开始事件，并触发 OnJobStart Hook。
func (r *RunJob) fireJobStart(job cronJob, start time.Time) {
	noteJobStart(job.Id, start)
	ev := JobEvent{
		JobId:         job.Id,
		JobName:       job.JobName,
		JobType:       job.JobType,
		ServiceName:   job.ServiceName,
		ServiceMethod: job.ServiceMethod,
		HookUrl:       job.HookUrl,
		CronExpr:      job.CronExpr,
		Status:        "running",
		StartTime:     start,
		InstanceId:    GetInstanceId(),
	}
	fireJobHook(context.Background(), "start", ev, r.Hook)

	// 推 JobLog（结构化定时任务日志，独立 ES index：job_log）
	r.emitJobLog(job, "start", start, time.Time{}, "", false)
}

// fireJobEnd 记录一次 job 结束事件：计量 + 直方图 + Hook。
func (r *RunJob) fireJobEnd(job cronJob, start, end time.Time, status, errMsg string, isPanic, isTimeout bool) {
	durationMs := end.Sub(start).Milliseconds()

	r.counters.exec.Add(1)
	switch status {
	case "success":
		r.counters.success.Add(1)
	case "failed":
		r.counters.failed.Add(1)
	case "panic":
		r.counters.panicCnt.Add(1)
		r.counters.failed.Add(1)
	case "timeout":
		r.counters.timeoutCnt.Add(1)
		r.counters.failed.Add(1)
	case "skip":
		r.counters.skipCnt.Add(1)
	}

	ctx := context.Background()
	recordJobExec(ctx, job.JobBase, status, durationMs)
	if isPanic {
		recordJobPanic(ctx, job.JobBase)
	}
	if isTimeout {
		recordJobTimeout(ctx, job.JobBase)
	}
	if status == "skip" {
		recordJobSkip(ctx, job.JobBase)
	}

	ev := JobEvent{
		JobId:         job.Id,
		JobName:       job.JobName,
		JobType:       job.JobType,
		ServiceName:   job.ServiceName,
		ServiceMethod: job.ServiceMethod,
		HookUrl:       job.HookUrl,
		CronExpr:      job.CronExpr,
		Status:        status,
		StartTime:     start,
		EndTime:       end,
		Duration:      durationMs,
		ErrMsg:        errMsg,
		IsPanic:       isPanic,
		IsTimeout:     isTimeout,
		InstanceId:    GetInstanceId(),
	}

	if isPanic {
		fireJobHook(ctx, "panic", ev, r.Hook)
	}
	if isTimeout {
		fireJobHook(ctx, "timeout", ev, r.Hook)
	}
	fireJobHook(ctx, "finish", ev, r.Hook)

	// 推 JobLog（结构化定时任务日志，独立 ES index：job_log）
	r.emitJobLog(job, mapJobStatusToPhase(status), start, end, errMsg, isPanic || isTimeout)
}
