package grpc

import (
	"context"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	workflowpb "github.com/xxzhwl/gaia/components/workflow/transport/grpc/proto"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// AutomationWorkerServiceName 是自动化 worker gRPC 服务名称。
	AutomationWorkerServiceName = "gaia.workflow.v1.AutomationWorkerService"
	// AutomationWorkerDispatchMethod 是自动化 worker 执行任务的方法名。
	AutomationWorkerDispatchMethod = "DispatchTask"
	// AutomationWorkerHealthMethod 是自动化 worker 健康检查方法名。
	AutomationWorkerHealthMethod = "Health"
)

// AutomationWorker 定义自动化 worker gRPC 服务需要实现的能力。
type AutomationWorker interface {
	DispatchTask(ctx context.Context, req automation.DispatchRequest) (automation.DispatchResult, error)
	Health(ctx context.Context) (map[string]any, error)
}

// RegisterAutomationWorkerService 将自动化 worker 注册到 gRPC Server。
func RegisterAutomationWorkerService(server googlegrpc.ServiceRegistrar, worker AutomationWorker) {
	workflowpb.RegisterAutomationWorkerServiceServer(server, &automationWorkerService{worker: worker})
}

type automationWorkerService struct {
	workflowpb.UnimplementedAutomationWorkerServiceServer

	worker AutomationWorker
}

func (s *automationWorkerService) DispatchTask(ctx context.Context, req *workflowpb.DispatchTaskRequest) (*workflowpb.DispatchTaskResponse, error) {
	if s.worker == nil {
		return nil, status.Error(codes.Internal, "automation worker grpc service is nil")
	}
	result, err := s.worker.DispatchTask(ctx, dispatchRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return dispatchResultToProto(result)
}

func (s *automationWorkerService) Health(ctx context.Context, _ *workflowpb.EmptyRequest) (*workflowpb.HealthResponse, error) {
	if s.worker == nil {
		return nil, status.Error(codes.Internal, "automation worker grpc service is nil")
	}
	result, err := s.worker.Health(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	data, err := mapToStruct(result)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &workflowpb.HealthResponse{Data: data}, nil
}
