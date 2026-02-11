// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/17
package jobs

import (
	"fmt"
	"net/http"

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
			//如果发生变化要考虑几种情况：1.被关闭了 2.核心参数被修改了
			if newJob, ok := jobsTemp[updateJobsId]; ok {
				if !newJob.Enabled {
					//被关闭，需要移除
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
			gaia.SendSystemAlarm("AddCronServiceJob", err.Error())
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
	if v, err := r.jobIsRunning(job.job); err != nil {
		r.cronHookLogger.ErrorF("获取任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
		return
	} else if v {
		r.cronHookLogger.InfoF("任务%d-%s正在运行中,跳过此次运行", job.Id, job.JobName)
		return
	}

	if err := r.updateJobToRunning(job.job); err != nil {
		r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
		return
	}

	resInfo, _, errTemp := httpclient.NewHttpRequest(job.HookUrl).WithMethod(http.MethodPost).WithBody(job.Args).Do()
	if errTemp != nil {
		if err := r.updateJobToWaitWithRes(job.job, errTemp); err != nil {
			r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
			return
		}
	}
	if err := r.updateJobToWaitWithRes(job.job, string(resInfo)); err != nil {
		r.cronHookLogger.ErrorF("更新任务%d-%s状态失败:%s", job.Id, job.JobName, err.Error())
		return
	}
	return
}
