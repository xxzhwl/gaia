// Package jobs gRPC 管理服务实现
// @author wanlizhan
// @created 2025/6/28
package jobs

import (
	"context"
	"database/sql"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	jobspb "github.com/xxzhwl/gaia/components/jobs/pb"
)

// JobAdminServer gRPC 管理服务。
type JobAdminServer struct {
	jobspb.UnimplementedJobAdminServer
	runJob *RunJob
}

// RegisterJobAdminServer 注册 JobAdmin gRPC 服务到给定 gRPC Server。
func RegisterJobAdminServer(s grpc.ServiceRegistrar, r *RunJob) {
	jobspb.RegisterJobAdminServer(s, &JobAdminServer{runJob: r})
}

func (s *JobAdminServer) ListJobs(ctx context.Context, req *jobspb.ListJobsReq) (*jobspb.ListJobsResp, error) {
	args := ListJobsArgs{
		Page:       int(req.Page),
		PageSize:   int(req.PageSize),
		SystemName: req.SystemName,
		JobType:    req.JobType,
		JobName:    req.JobName,
	}
	if req.Enabled != nil {
		args.Enabled = req.Enabled
	}
	if page := int(req.Page); page <= 0 {
		args.Page = 1
	}
	if ps := int(req.PageSize); ps <= 0 {
		args.PageSize = 20
	}
	result, err := s.runJob.ListJobs(args, ctx)
	if err != nil {
		return nil, err
	}
	jobs := make([]*jobspb.JobItem, 0, len(result.List))
	for _, j := range result.List {
		jobs = append(jobs, jobToProtoItem(j))
	}
	return &jobspb.ListJobsResp{Jobs: jobs, Total: result.Total}, nil
}

func (s *JobAdminServer) GetJob(ctx context.Context, req *jobspb.GetJobReq) (*jobspb.JobItem, error) {
	j, err := s.runJob.GetJobDetail(req.Id, ctx)
	if err != nil {
		return nil, err
	}
	return jobToProtoItem(j), nil
}

func (s *JobAdminServer) CreateJob(ctx context.Context, req *jobspb.CreateJobReq) (*jobspb.CreateJobResp, error) {
	id, err := s.runJob.AddJob(AddJobArgs{
		JobBase: JobBase{
			SystemName:    req.SystemName,
			JobName:       req.JobName,
			JobType:       req.JobType,
			CronExpr:      req.CronExpr,
			ServiceName:   req.ServiceName,
			ServiceMethod: req.ServiceMethod,
			Args:          []byte(req.Args),
			HookUrl:       req.HookUrl,
			Timeout:       int(req.Timeout),
		},
		Enable: req.Enabled,
	})
	if err != nil {
		return nil, err
	}
	return &jobspb.CreateJobResp{Id: id}, nil
}

func (s *JobAdminServer) UpdateJob(ctx context.Context, req *jobspb.UpdateJobReq) (*jobspb.UpdateJobResp, error) {
	affected, err := s.runJob.UpdateJob(UpdateJobArgs{
		JobId: req.Id,
		JobBase: JobBase{
			SystemName:    req.SystemName,
			JobName:       req.JobName,
			JobType:       req.JobType,
			CronExpr:      req.CronExpr,
			ServiceName:   req.ServiceName,
			ServiceMethod: req.ServiceMethod,
			Args:          []byte(req.Args),
			HookUrl:       req.HookUrl,
			Timeout:       int(req.Timeout),
		},
		Enable: req.Enabled,
	})
	if err != nil {
		return nil, err
	}
	return &jobspb.UpdateJobResp{Affected: affected}, nil
}

func (s *JobAdminServer) DeleteJob(ctx context.Context, req *jobspb.DeleteJobReq) (*jobspb.Empty, error) {
	if err := s.runJob.RemoveJob(req.Id); err != nil {
		return nil, err
	}
	return &jobspb.Empty{}, nil
}

func (s *JobAdminServer) ToggleJob(ctx context.Context, req *jobspb.ToggleJobReq) (*jobspb.Empty, error) {
	enabled := false
	if req.Enabled != nil {
		enabled = req.GetEnabled()
	} else {
		j, err := s.runJob.GetJobDetail(req.Id, ctx)
		if err != nil {
			return nil, err
		}
		enabled = !j.Enabled
	}
	if err := s.runJob.ToggleJob(req.Id, enabled, ctx); err != nil {
		return nil, err
	}
	return &jobspb.Empty{}, nil
}

func (s *JobAdminServer) ExecuteJob(ctx context.Context, req *jobspb.ExecuteJobReq) (*jobspb.Empty, error) {
	if err := s.runJob.ExecuteJobImmediately(req.Id, ctx); err != nil {
		return nil, err
	}
	return &jobspb.Empty{}, nil
}

func (s *JobAdminServer) ListRecords(ctx context.Context, req *jobspb.ListRecordsReq) (*jobspb.ListRecordsResp, error) {
	page, pageSize := int(req.Page), int(req.PageSize)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	records, total, err := s.runJob.GetJobRecordsWithStatus(req.JobId, req.Status, page, pageSize, ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*jobspb.JobRecordItem, 0, len(records))
	for _, r := range records {
		items = append(items, &jobspb.JobRecordItem{
			Id:            r.Id,
			SystemName:    r.SystemName,
			JobName:       r.JobName,
			JobType:       r.JobType,
			ServiceName:   r.ServiceName,
			ServiceMethod: r.ServiceMethod,
			Result:        r.JobResultFlag,
			Error:         r.RunErr,
			RunResult:     r.RunResult,
			DurationMs:    r.DurationMs,
			InstanceId:    r.InstanceId,
			CreatedAt:     timeToProto(r.CreateTime),
			UpdatedAt:     timeToProto(r.UpdateTime),
		})
	}
	return &jobspb.ListRecordsResp{Records: items, Total: total}, nil
}

func (s *JobAdminServer) GetRunningJobs(ctx context.Context, _ *jobspb.Empty) (*jobspb.RunningJobsResp, error) {
	jobs, err := s.runJob.GetRunningJobs(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*jobspb.RunningJobItem, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, &jobspb.RunningJobItem{
			Id:            j.Id,
			SystemName:    j.SystemName,
			JobName:       j.JobName,
			JobType:       j.JobType,
			ServiceName:   j.ServiceName,
			ServiceMethod: j.ServiceMethod,
			RunStatus:     j.RunStatus,
			LeaseOwner:    j.LeaseOwner,
			LastRunTime:   nullTimeToProto(j.LastRunTime),
		})
	}
	return &jobspb.RunningJobsResp{Running: items}, nil
}

func (s *JobAdminServer) GetStats(ctx context.Context, _ *jobspb.Empty) (*jobspb.JobStatsResp, error) {
	stats, err := s.runJob.GetJobStats(ctx)
	if err != nil {
		return nil, err
	}
	return &jobspb.JobStatsResp{
		Total:    stats.Total,
		Enabled:  stats.Enabled,
		Disabled: stats.Disabled,
		Running:  stats.Running,
		ByType:   stats.ByType,
	}, nil
}

func (s *JobAdminServer) GetSchedulerStatus(ctx context.Context, _ *jobspb.Empty) (*jobspb.SchedulerStatusResp, error) {
	info, err := s.runJob.GetHealthInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &jobspb.SchedulerStatusResp{
		InstanceId:   info.InstanceId,
		Healthy:      info.Healthy,
		ActiveJobs:   int32(len(s.runJob.currentCronServiceJobMap) + len(s.runJob.currentCronHookJobMap)),
		ExecCount:    info.MetricsSnap.ExecTotal,
		SuccessCount: info.MetricsSnap.SuccessTotal,
		FailedCount:  info.MetricsSnap.FailedTotal,
		PanicCount:   info.MetricsSnap.PanicTotal,
		SkipCount:    info.MetricsSnap.SkipTotal,
		TimeoutCount: info.MetricsSnap.TimeoutTotal,
	}, nil
}

func (s *JobAdminServer) StartScheduler(ctx context.Context, _ *jobspb.Empty) (*jobspb.Empty, error) {
	s.runJob.StartAsync()
	return &jobspb.Empty{}, nil
}

func (s *JobAdminServer) StopScheduler(ctx context.Context, _ *jobspb.Empty) (*jobspb.Empty, error) {
	s.runJob.Stop()
	return &jobspb.Empty{}, nil
}

func (s *JobAdminServer) RegisterService(ctx context.Context, req *jobspb.RegisterServiceReq) (*jobspb.Empty, error) {
	if err := RegisterCronServiceMeta(req.SystemName, req.ServiceName, req.Methods); err != nil {
		return nil, err
	}
	return &jobspb.Empty{}, nil
}

func (s *JobAdminServer) ListServices(ctx context.Context, req *jobspb.ListServicesReq) (*jobspb.ListServicesResp, error) {
	registered := ListRegisteredCronServices(req.SystemName)
	svcs := make([]*jobspb.RegisteredService, 0, len(registered))
	for _, svc := range registered {
		svcs = append(svcs, &jobspb.RegisteredService{
			SystemName:  svc.SystemName,
			ServiceName: svc.ServiceName,
			Methods:     svc.Methods,
			Executable:  svc.Executable,
		})
	}
	return &jobspb.ListServicesResp{Services: svcs}, nil
}

func (s *JobAdminServer) GetHealth(ctx context.Context, _ *jobspb.Empty) (*jobspb.HealthResp, error) {
	info, err := s.runJob.GetHealthInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &jobspb.HealthResp{
		Healthy:        info.Healthy,
		InstanceId:     info.InstanceId,
		DbSchema:       info.DBSchema,
		HaDisabled:     info.HADisabled,
		RunningJobNum:  int32(info.RunningJobNum),
		WaitJobNum:     int32(info.WaitJobNum),
		DisabledJobNum: int32(info.DisabledJobNum),
		MyOwnedJobNum:  int32(info.MyOwnedJobNum),
	}, nil
}

func jobToProtoItem(j job) *jobspb.JobItem {
	return &jobspb.JobItem{
		Id:            j.Id,
		SystemName:    j.SystemName,
		JobName:       j.JobName,
		JobType:       j.JobType,
		CronExpr:      j.CronExpr,
		Enabled:       j.Enabled,
		ServiceName:   j.ServiceName,
		ServiceMethod: j.ServiceMethod,
		Args:          string(j.Args),
		HookUrl:       j.HookUrl,
		Timeout:       int32(j.Timeout),
		RunStatus:     j.RunStatus,
		LeaseOwner:    j.LeaseOwner,
		CreatedAt:     timeToProto(j.CreateTime),
		UpdatedAt:     timeToProto(j.UpdateTime),
		LastRunTime:   nullTimeToProto(j.LastRunTime),
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
