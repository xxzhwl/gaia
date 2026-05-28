// Package asynctask gRPC 管理服务实现
// @author wanlizhan
// @created 2025/6/28
package asynctask

import (
	"context"
	"database/sql"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	asynctaskpb "github.com/xxzhwl/gaia/components/asynctask/pb"
)

// AsyncTaskAdminServer gRPC 管理服务。
type AsyncTaskAdminServer struct {
	asynctaskpb.UnimplementedAsyncTaskAdminServer
}

// RegisterAsyncTaskAdminServer 注册 AsyncTaskAdmin gRPC 服务到给定 gRPC Server。
func RegisterAsyncTaskAdminServer(s grpc.ServiceRegistrar) {
	asynctaskpb.RegisterAsyncTaskAdminServer(s, &AsyncTaskAdminServer{})
}

func (s *AsyncTaskAdminServer) SubmitTask(ctx context.Context, req *asynctaskpb.SubmitTaskReq) (*asynctaskpb.SubmitTaskResp, error) {
	task, err := SubmitTask(req.Theme, TaskBaseInfo{
		ServiceName:  req.ServiceName,
		MethodName:   req.MethodName,
		TaskName:     req.TaskName,
		Arg:          req.Arg,
		Priority:     req.Priority,
		TenantId:     req.TenantId,
		MaxRetryTime: int(req.MaxRetry),
	})
	if err != nil {
		return nil, err
	}
	return &asynctaskpb.SubmitTaskResp{Id: task.Id}, nil
}

func (s *AsyncTaskAdminServer) GetTask(ctx context.Context, req *asynctaskpb.GetTaskReq) (*asynctaskpb.TaskItem, error) {
	task, err := GetTaskDetail(req.Id, ctx)
	if err != nil {
		return nil, err
	}
	return taskModelToProto(task), nil
}

func (s *AsyncTaskAdminServer) ListTasks(ctx context.Context, req *asynctaskpb.ListTasksReq) (*asynctaskpb.ListTasksResp, error) {
	args := ListTasksArgs{
		Page:       int(req.Page),
		PageSize:   int(req.PageSize),
		TaskName:   req.TaskName,
		TaskStatus: req.TaskStatuses,
		TenantId:   req.TenantId,
	}
	if len(args.TaskStatus) == 0 {
		args.TaskStatus = parseStatusFilter(req.Status)
	}
	if page := int(req.Page); page <= 0 {
		args.Page = 1
	}
	if ps := int(req.PageSize); ps <= 0 {
		args.PageSize = 20
	}
	result, err := ListTasks(args, req.Theme, ctx)
	if err != nil {
		return nil, err
	}
	tasks := make([]*asynctaskpb.TaskItem, 0, len(result.List))
	for _, t := range result.List {
		tasks = append(tasks, taskModelToProto(t))
	}
	return &asynctaskpb.ListTasksResp{Tasks: tasks, Total: result.Total}, nil
}

func parseStatusFilter(status string) []string {
	if status == "" {
		return nil
	}
	return []string{status}
}

func (s *AsyncTaskAdminServer) RetryTask(ctx context.Context, req *asynctaskpb.RetryTaskReq) (*asynctaskpb.Empty, error) {
	if err := RetryTask(req.Id, ctx); err != nil {
		return nil, err
	}
	return &asynctaskpb.Empty{}, nil
}

func (s *AsyncTaskAdminServer) CancelTask(ctx context.Context, req *asynctaskpb.CancelTaskReq) (*asynctaskpb.Empty, error) {
	if err := CancelTask(req.Id, ctx); err != nil {
		return nil, err
	}
	return &asynctaskpb.Empty{}, nil
}

func (s *AsyncTaskAdminServer) ListRecords(ctx context.Context, req *asynctaskpb.ListRecordsReq) (*asynctaskpb.ListRecordsResp, error) {
	page, pageSize := int(req.Page), int(req.PageSize)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	records, total, err := GetTaskExecRecordsWithStatus(req.TaskId, req.Status, page, pageSize, ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*asynctaskpb.TaskRecordItem, 0, len(records))
	for _, r := range records {
		items = append(items, &asynctaskpb.TaskRecordItem{
			Id:           r.Id,
			TaskId:       r.TaskId,
			TaskName:     r.TaskName,
			Status:       r.TaskStatus,
			ErrorMessage: r.LastErrMsg,
			DurationMs:   r.LastRunDuration,
			CreatedAt:    timeToProto(r.LastRunTime),
			LogId:        r.LogId,
			Result:       r.LastResult,
		})
	}
	return &asynctaskpb.ListRecordsResp{Records: items, Total: total}, nil
}

func (s *AsyncTaskAdminServer) GetStats(ctx context.Context, _ *asynctaskpb.Empty) (*asynctaskpb.TaskStatsResp, error) {
	var pending, running, success, failed int64
	for _, sch := range GetAllSchedulerInfo() {
		pending += int64(sch.Status.ExecTasks - sch.Status.ExecSuccess - sch.Status.ExecFails)
		running += int64(sch.Status.RunningWorkers)
		success += sch.Status.ExecSuccess
		failed += sch.Status.ExecFails
	}
	return &asynctaskpb.TaskStatsResp{
		TotalPending: pending,
		TotalRunning: running,
		TotalSuccess: success,
		TotalFailed:  failed,
	}, nil
}

func (s *AsyncTaskAdminServer) ListSchedulers(ctx context.Context, _ *asynctaskpb.Empty) (*asynctaskpb.ListSchedulersResp, error) {
	infos := GetAllSchedulerInfo()
	items := make([]*asynctaskpb.SchedulerInfo, 0, len(infos))
	for _, info := range infos {
		items = append(items, schedulerInfoToProto(info.Theme, info.Running, info.Status))
	}
	return &asynctaskpb.ListSchedulersResp{Schedulers: items}, nil
}

func (s *AsyncTaskAdminServer) GetScheduler(ctx context.Context, req *asynctaskpb.GetSchedulerReq) (*asynctaskpb.SchedulerInfo, error) {
	sch := GetScheduler(req.Theme)
	if sch == nil {
		return nil, nil
	}
	status := sch.GetStatusInfo()
	return schedulerInfoToProto(sch.Theme, sch.IsRunning(), status), nil
}

func (s *AsyncTaskAdminServer) StartScheduler(ctx context.Context, req *asynctaskpb.StartSchedulerReq) (*asynctaskpb.Empty, error) {
	wc := int(req.WorkerCount)
	if wc <= 0 {
		wc = 5
	}
	sch := GetScheduler(req.Theme)
	if sch != nil {
		sch.Resume()
		return &asynctaskpb.Empty{}, nil
	}
	StartScheduler(ctx, req.Theme, WithWorkerNum(wc))
	return &asynctaskpb.Empty{}, nil
}

func (s *AsyncTaskAdminServer) StopScheduler(ctx context.Context, req *asynctaskpb.StopSchedulerReq) (*asynctaskpb.Empty, error) {
	StopScheduler(req.Theme)
	return &asynctaskpb.Empty{}, nil
}

func (s *AsyncTaskAdminServer) GetAllMetrics(ctx context.Context, _ *asynctaskpb.Empty) (*asynctaskpb.AllMetricsResp, error) {
	snapshots := GetAllMetricsSnapshot()
	metrics := make([]*asynctaskpb.MetricItem, 0, len(snapshots)*10)
	for _, snap := range snapshots {
		metrics = append(metrics,
			&asynctaskpb.MetricItem{Name: snap.Theme + ".queue_depth", Value: int64(snap.QueueDepth)},
			&asynctaskpb.MetricItem{Name: snap.Theme + ".retry_count", Value: snap.RetryCount},
			&asynctaskpb.MetricItem{Name: snap.Theme + ".panic_count", Value: snap.PanicCount},
			&asynctaskpb.MetricItem{Name: snap.Theme + ".queue_drop", Value: snap.QueueDropCount},
			&asynctaskpb.MetricItem{Name: snap.Theme + ".db_error", Value: snap.DBErrorCount},
			&asynctaskpb.MetricItem{Name: snap.Theme + ".alarm_fired", Value: snap.AlarmFiredCount},
		)
	}
	return &asynctaskpb.AllMetricsResp{Metrics: metrics}, nil
}

func taskModelToProto(t TaskModel) *asynctaskpb.TaskItem {
	return &asynctaskpb.TaskItem{
		Id:              t.Id,
		TaskName:        t.TaskName,
		Theme:           t.SystemName,
		ServiceName:     t.ServiceName,
		MethodName:      t.MethodName,
		Arg:             t.Arg,
		Status:          t.TaskStatus,
		MaxRetry:        int32(t.MaxRetryTime),
		RetryCount:      int32(t.RetryTime),
		ErrorMessage:    t.LastErrMsg,
		Priority:        t.Priority,
		TenantId:        t.TenantId,
		CreatedAt:       timeToProto(t.CreateAt),
		UpdatedAt:       timeToProto(t.UpdateAt),
		LogId:           t.LogId,
		LastRunTime:     nullTimeToProto(t.LastRunTime),
		LastRunEndTime:  nullTimeToProto(t.LastRunEndTime),
		LastRunDuration: t.LastRunDuration,
		LastResult:      t.LastResult,
	}
}

func timeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func nullTimeToProto(t sql.NullTime) *timestamppb.Timestamp {
	if !t.Valid {
		return nil
	}
	return timeToProto(t.Time)
}

func schedulerInfoToProto(theme string, running bool, status SchedulerStatusInfo) *asynctaskpb.SchedulerInfo {
	return &asynctaskpb.SchedulerInfo{
		Theme:          theme,
		IsRunning:      running,
		WorkerCount:    status.AllWorkers,
		QueueDepth:     status.PushTasks - status.PullTasks,
		SuccessTotal:   status.ExecSuccess,
		FailedTotal:    status.ExecFails,
		Scans:          status.Scans,
		PushTasks:      status.PushTasks,
		PullTasks:      status.PullTasks,
		ExecTasks:      status.ExecTasks,
		ExecSuccess:    status.ExecSuccess,
		ExecFails:      status.ExecFails,
		RunningWorkers: status.RunningWorkers,
		AllWorkers:     status.AllWorkers,
	}
}
