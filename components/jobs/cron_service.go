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
			gaia.SendSystemAlarm("AddCronServiceJob", err.Error())
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
	service, err := GetCronService(job.ServiceName)
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

	type result struct {
		res       any
		err       error
		isPanic   bool
		panicMsg  string
		startTime time.Time
	}
	resultChan := make(chan result, 1)

	go func() {
		startTime := time.Now()
		defer func() {
			if rr := recover(); rr != nil {
				panicErr := gaia.PanicLog(rr)
				r.cronJobLogger.ErrorF("任务%d-%s执行panic: %s", job.Id, job.JobName, panicErr)
				gaia.SendSystemAlarm(fmt.Sprintf("JobPanic[%s]", job.JobName),
					fmt.Sprintf("任务ID: %d, 任务名称: %s, Panic信息: %s", job.Id, job.JobName, panicErr))
				resultChan <- result{
					res:       nil,
					err:       errors.New("任务执行 panic: " + panicErr),
					isPanic:   true,
					panicMsg:  panicErr,
					startTime: startTime,
				}
			}
		}()

		if v, err := r.jobIsRunning(job.job); err != nil {
			r.cronJobLogger.ErrorF("获取任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			resultChan <- result{nil, err, false, "", startTime}
			return
		} else if v {
			r.cronJobLogger.InfoF("任务%d-%s正在运行中,跳过此次运行", job.Id, job.JobName)
			resultChan <- result{nil, errors.New("任务正在运行中"), false, "", startTime}
			return
		}
		if err := r.updateJobToRunning(job.job); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			resultChan <- result{nil, err, false, "", startTime}
			return
		}

		res, doErr := gaia.CallMethodWithArgs(service, job.ServiceMethod, job.Args)
		resultChan <- result{res, doErr, false, "", startTime}
	}()

	select {
	case <-ctx.Done():
		r.cronJobLogger.ErrorF("任务%d-%s执行超时(>%v)", job.Id, job.JobName, timeout)
		timeoutErr := fmt.Errorf("任务执行超时(>%v)", timeout)
		gaia.SendSystemAlarm(fmt.Sprintf("JobTimeout[%s]", job.JobName),
			fmt.Sprintf("任务ID: %d, 任务名称: %s, 超时时间: %v", job.Id, job.JobName, timeout))
		if err := r.updateJobToWaitWithRes(job.job, timeoutErr); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
		}
		return
	case result := <-resultChan:
		if result.err != nil {
			errStr := result.err.Error()
			if errStr != "任务正在运行中" {
				if result.isPanic {
					if err := r.updateJobToWaitWithResAndPanic(job.job, result.panicMsg); err != nil {
						r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
					}
				} else {
					if err := r.updateJobToWaitWithRes(job.job, result.err); err != nil {
						r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
					}
				}
			}
			return
		}
		if err := r.updateJobToWaitWithRes(job.job, result.res); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			return
		}
	}
}
