// Package jobs HTTP 管理 API
// @author wanlizhan
// @created 2025/6/28
package jobs

import (
	"fmt"
	"strconv"

	"github.com/xxzhwl/gaia/framework/server"
)

// RegisterJobAdminRoutes 注册 Jobs 管理 HTTP 路由。
func RegisterJobAdminRoutes(g *server.Server, r *RunJob, authMid server.Plugin) {
	api := g.Group("/api/v1")
	jobs := api.Group("/jobs", server.MakePlugin(authMid))
	jobs.GET("", server.MakeHandler(listJobs(r)))
	jobs.GET("/:id", server.MakeHandler(getJob(r)))
	jobs.POST("", server.MakeHandler(createJob(r)))
	jobs.PUT("/:id", server.MakeHandler(updateJob(r)))
	jobs.DELETE("/:id", server.MakeHandler(deleteJob(r)))
	jobs.POST("/:id/toggle", server.MakeHandler(toggleJob(r)))
	jobs.POST("/:id/execute", server.MakeHandler(executeJob(r)))
	jobs.GET("/records", server.MakeHandler(listRecords(r)))
	jobs.GET("/running", server.MakeHandler(getRunningJobs(r)))
	jobs.GET("/stats", server.MakeHandler(getStats(r)))

	scheduler := api.Group("/scheduler", server.MakePlugin(authMid))
	scheduler.GET("/status", server.MakeHandler(schedulerStatus(r)))
	scheduler.POST("/start", server.MakeHandler(startScheduler(r)))
	scheduler.POST("/stop", server.MakeHandler(stopScheduler(r)))

	svc := api.Group("/services", server.MakePlugin(authMid))
	svc.POST("", server.MakeHandler(registerService()))
	svc.GET("", server.MakeHandler(listServices()))
}

func listJobs(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		page, _ := strconv.ParseInt(req.GetUrlQuery("page"), 10, 64)
		pageSize, _ := strconv.ParseInt(req.GetUrlQuery("page_size"), 10, 64)
		enabledStr := req.GetUrlQuery("enabled")
		var enabled *bool
		if enabledStr != "" {
			v, err := strconv.ParseBool(enabledStr)
			if err == nil {
				enabled = &v
			}
		}
		args := ListJobsArgs{
			Page:       int(page),
			PageSize:   int(pageSize),
			SystemName: req.GetUrlQuery("system_name"),
			JobType:    req.GetUrlQuery("job_type"),
			JobName:    req.GetUrlQuery("job_name"),
			Enabled:    enabled,
		}
		if args.Page <= 0 {
			args.Page = 1
		}
		if args.PageSize <= 0 {
			args.PageSize = 20
		}
		result, err := r.ListJobs(args, req.TraceContext)
		if err != nil {
			return nil, err
		}
		return map[string]any{"jobs": result.List, "total": result.Total}, nil
	}
}

func getJob(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]无效的任务 ID")
		}
		return r.GetJobDetail(id, req.TraceContext)
	}
}

func createJob(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		var body struct {
			SystemName    string `json:"system_name"`
			JobName       string `json:"job_name"`
			JobType       string `json:"job_type"`
			CronExpr      string `json:"cron_expr"`
			ServiceName   string `json:"service_name"`
			ServiceMethod string `json:"service_method"`
			Args          string `json:"args"`
			HookURL       string `json:"hook_url"`
			Timeout       int    `json:"timeout"`
			Enabled       bool   `json:"enabled"`
		}
		if err := req.BindJsonWithChecker(&body); err != nil {
			return nil, err
		}
		id, err := r.AddJob(AddJobArgs{
			JobBase: JobBase{
				SystemName:    body.SystemName,
				JobName:       body.JobName,
				JobType:       body.JobType,
				CronExpr:      body.CronExpr,
				ServiceName:   body.ServiceName,
				ServiceMethod: body.ServiceMethod,
				Args:          []byte(body.Args),
				HookUrl:       body.HookURL,
				Timeout:       body.Timeout,
			},
			Enable: body.Enabled,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id}, nil
	}
}

func updateJob(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]无效的任务 ID")
		}
		var body struct {
			SystemName    string `json:"system_name"`
			JobName       string `json:"job_name"`
			JobType       string `json:"job_type"`
			CronExpr      string `json:"cron_expr"`
			ServiceName   string `json:"service_name"`
			ServiceMethod string `json:"service_method"`
			Args          string `json:"args"`
			HookURL       string `json:"hook_url"`
			Timeout       int    `json:"timeout"`
			Enabled       bool   `json:"enabled"`
		}
		if err := req.BindJsonWithChecker(&body); err != nil {
			return nil, err
		}
		affected, err := r.UpdateJob(UpdateJobArgs{
			JobId: id,
			JobBase: JobBase{
				SystemName:    body.SystemName,
				JobName:       body.JobName,
				JobType:       body.JobType,
				CronExpr:      body.CronExpr,
				ServiceName:   body.ServiceName,
				ServiceMethod: body.ServiceMethod,
				Args:          []byte(body.Args),
				HookUrl:       body.HookURL,
				Timeout:       body.Timeout,
			},
			Enable: body.Enabled,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"affected": affected}, nil
	}
}

func deleteJob(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]无效的任务 ID")
		}
		if err := r.RemoveJob(id); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func toggleJob(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]无效的任务 ID")
		}
		j, err := r.GetJobDetail(id, req.TraceContext)
		if err != nil {
			return nil, err
		}
		if err := r.ToggleJob(id, !j.Enabled, req.TraceContext); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func executeJob(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		id, err := strconv.ParseInt(req.GetUrlParam("id"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("[400]无效的任务 ID")
		}
		if err := r.ExecuteJobImmediately(id, req.TraceContext); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func listRecords(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		jobID, _ := strconv.ParseInt(req.GetUrlQuery("job_id"), 10, 64)
		page, _ := strconv.ParseInt(req.GetUrlQuery("page"), 10, 64)
		pageSize, _ := strconv.ParseInt(req.GetUrlQuery("page_size"), 10, 64)
		if page <= 0 {
			page = 1
		}
		if pageSize <= 0 {
			pageSize = 20
		}
		records, total, err := r.GetJobRecordsWithStatus(
			jobID,
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

func getRunningJobs(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		jobs, err := r.GetRunningJobs(req.TraceContext)
		if err != nil {
			return nil, err
		}
		return map[string]any{"running": jobs}, nil
	}
}

func getStats(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		return r.GetJobStats(req.TraceContext)
	}
}

func schedulerStatus(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		return r.GetHealthInfo(req.TraceContext)
	}
}

func startScheduler(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		r.StartAsync()
		return nil, nil
	}
}

func stopScheduler(r *RunJob) func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		r.Stop()
		return nil, nil
	}
}

func registerService() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		var body struct {
			SystemName  string   `json:"system_name"`
			ServiceName string   `json:"service_name" require:"1"`
			Methods     []string `json:"methods"`
		}
		if err := req.BindJsonWithChecker(&body); err != nil {
			return nil, err
		}
		if err := RegisterCronServiceMeta(body.SystemName, body.ServiceName, body.Methods); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func listServices() func(req server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		systemName := req.GetUrlQuery("system_name")
		registered := ListRegisteredCronServices(systemName)
		svcs := make([]map[string]any, 0, len(registered))
		for _, svc := range registered {
			svcs = append(svcs, map[string]any{
				"system_name":  svc.SystemName,
				"service_name": svc.ServiceName,
				"methods":      svc.Methods,
				"executable":   svc.Executable,
			})
		}
		return map[string]any{"services": svcs}, nil
	}
}
