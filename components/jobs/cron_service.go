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
			//如果发生变化要考虑几种情况：1.被关闭了 2.核心参数被修改了
			if newJob, ok := jobsTemp[updateJobsId]; ok {
				if !newJob.Enabled {
					//被关闭，需要移除
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

	// 创建带超时的 context
	timeout := time.Duration(job.job.GetTimeout()) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 使用 channel 来传递执行结果
	type result struct {
		res any
		err error
	}
	resultChan := make(chan result, 1)

	// 在单独的 goroutine 中执行任务
	go func() {
		defer func() {
			if rr := recover(); rr != nil {
				panicErr := gaia.PanicLog(rr)
				resultChan <- result{nil, errors.New("任务执行 panic: " + panicErr)}
			}
		}()

		if v, err := r.jobIsRunning(job.job); err != nil {
			r.cronJobLogger.ErrorF("获取任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			resultChan <- result{nil, err}
			return
		} else if v {
			r.cronJobLogger.InfoF("任务%d-%s正在运行中,跳过此次运行", job.Id, job.JobName)
			resultChan <- result{nil, errors.New("任务正在运行中")}
			return
		}
		if err := r.updateJobToRunning(job.job); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			resultChan <- result{nil, err}
			return
		}

		res, doErr := gaia.CallMethodWithArgs(service, job.ServiceMethod, job.Args)
		resultChan <- result{res, doErr}
	}()

	// 等待任务完成或超时
	select {
	case <-ctx.Done():
		// 任务超时
		r.cronJobLogger.ErrorF("任务%d-%s执行超时(>%v)", job.Id, job.JobName, timeout)
		if err := r.updateJobToWaitWithRes(job.job, fmt.Errorf("任务执行超时(>%v)", timeout)); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
		}
		return
	case result := <-resultChan:
		if result.err != nil {
			// 任务执行出错（可能是 panic 或运行中）
			if errStr := result.err.Error(); errStr != "任务正在运行中" {
				if err := r.updateJobToWaitWithRes(job.job, result.err); err != nil {
					r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
				}
			}
			return
		}
		// 任务执行成功
		if err := r.updateJobToWaitWithRes(job.job, result.res); err != nil {
			r.cronJobLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			return
		}
	}
}
