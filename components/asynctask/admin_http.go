// Package asynctask HTTP 管理 API
// @author wanlizhan
// @created 2025/6/28
package asynctask

import (
	"fmt"
	"strconv"

	"github.com/xxzhwl/gaia/framework/server"
)

// RegisterAsyncTaskAdminRoutes 注册 AsyncTask 管理 HTTP 路由。
func RegisterAsyncTaskAdminRoutes(g *server.Server, authMid server.Plugin) {
	v1 := g.Group("/api/v1")

	v1.POST("/tasks", server.MakePlugin(authMid), server.MakeHandler(submitTask()))
	v1.GET("/tasks/:id", server.MakePlugin(authMid), server.MakeHandler(getTask()))
	v1.GET("/tasks", server.MakePlugin(authMid), server.MakeHandler(listTasks()))
	v1.POST("/tasks/:id/retry", server.MakePlugin(authMid), server.MakeHandler(retryTask()))
	v1.POST("/tasks/:id/cancel", server.MakePlugin(authMid), server.MakeHandler(cancelTask()))

	v1.GET("/records", server.MakePlugin(authMid), server.MakeHandler(listRecords()))

	v1.GET("/schedulers", server.MakePlugin(authMid), server.MakeHandler(listSchedulers()))
	v1.GET("/schedulers/:theme", server.MakePlugin(authMid), server.MakeHandler(getScheduler()))
	v1.POST("/schedulers/:theme/start", server.MakePlugin(authMid), server.MakeHandler(startScheduler()))
	v1.POST("/schedulers/:theme/stop", server.MakePlugin(authMid), server.MakeHandler(stopScheduler()))

	v1.GET("/stats", server.MakePlugin(authMid), server.MakeHandler(getStats()))
}

func submitTask() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		var body struct {
			Theme       string `json:"theme"`
			TaskName    string `json:"task_name"`
			ServiceName string `json:"service_name"`
			MethodName  string `json:"method_name"`
			Arg         string `json:"arg"`
			Priority    int32  `json:"priority"`
			TenantId    string `json:"tenant_id"`
			MaxRetry    int    `json:"max_retry"`
		}
		if err := req.BindJsonWithChecker(&body); err != nil {
			return nil, err
		}
		task, err := SubmitTask(body.Theme, TaskBaseInfo{
			TaskName:     body.TaskName,
			ServiceName:  body.ServiceName,
			MethodName:   body.MethodName,
			Arg:          body.Arg,
			Priority:     body.Priority,
			TenantId:     body.TenantId,
			MaxRetryTime: body.MaxRetry,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": task.Id}, nil
	}
}

func getTask() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]invalid task id")
		}
		return GetTaskDetail(id, req.TraceContext)
	}
}

func listTasks() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		page, _ := strconv.ParseInt(req.GetUrlQuery("page"), 10, 64)
		pageSize, _ := strconv.ParseInt(req.GetUrlQuery("page_size"), 10, 64)
		args := ListTasksArgs{
			Page:       int(page),
			PageSize:   int(pageSize),
			TaskName:   req.GetUrlQuery("task_name"),
			TaskStatus: parseStatusFilter(req.GetUrlQuery("status")),
			TenantId:   req.GetUrlQuery("tenant_id"),
		}
		if args.Page <= 0 {
			args.Page = 1
		}
		if args.PageSize <= 0 {
			args.PageSize = 20
		}
		result, err := ListTasks(args, req.GetUrlQuery("theme"), req.TraceContext)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tasks": result.List, "total": result.Total}, nil
	}
}

func retryTask() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]invalid task id")
		}
		if err := RetryTask(id, req.TraceContext); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func cancelTask() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]invalid task id")
		}
		if err := CancelTask(id, req.TraceContext); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func listRecords() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		taskID, _ := strconv.ParseInt(req.GetUrlQuery("task_id"), 10, 64)
		page, _ := strconv.ParseInt(req.GetUrlQuery("page"), 10, 64)
		pageSize, _ := strconv.ParseInt(req.GetUrlQuery("page_size"), 10, 64)
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 20
		}
		records, total, err := GetTaskExecRecordsWithStatus(
			taskID,
			req.GetUrlQuery("status"),
			int(page),
			int(pageSize),
			req.TraceContext,
		)
		if err != nil {
			return nil, err
		}
		return map[string]any{"records": records, "total": total}, nil
	}
}

func listSchedulers() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		return GetAllSchedulerInfo(), nil
	}
}

func getScheduler() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		theme := req.GetUrlParam("theme")
		sch := GetScheduler(theme)
		if sch == nil {
			return nil, nil
		}
		status := sch.GetStatusInfo()
		return map[string]any{
			"theme":        sch.Theme,
			"is_running":   sch.IsRunning(),
			"worker_count": status.AllWorkers,
			"queue_depth":  status.PushTasks - status.PullTasks,
			"success":      status.ExecSuccess,
			"failed":       status.ExecFails,
		}, nil
	}
}

func startScheduler() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		theme := req.GetUrlParam("theme")
		var body struct {
			WorkerCount int32 `json:"worker_count"`
		}
		if err := req.BindJsonWithChecker(&body); err != nil {
			return nil, err
		}
		wc := int(body.WorkerCount)
		if wc <= 0 {
			wc = 5
		}
		sch := GetScheduler(theme)
		if sch != nil {
			sch.Resume()
			return nil, nil
		}
		StartScheduler(req.TraceContext, theme, WithWorkerNum(wc))
		return nil, nil
	}
}

func stopScheduler() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		theme := req.GetUrlParam("theme")
		StopScheduler(theme)
		return nil, nil
	}
}

func getStats() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		var pending, running, success, failed int64
		for _, sch := range GetAllSchedulerInfo() {
			pending += sch.Status.PushTasks - sch.Status.PullTasks
			running += int64(sch.Status.RunningWorkers)
			success += sch.Status.ExecSuccess
			failed += sch.Status.ExecFails
		}
		return map[string]any{
			"pending": pending,
			"running": running,
			"success": success,
			"failed":  failed,
		}, nil
	}
}
