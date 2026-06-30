package grpc

import (
	"context"

	"github.com/xxzhwl/gaia/components/workflow/automation"
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
	server.RegisterService(&automationWorkerServiceDesc, &automationWorkerService{worker: worker})
}

type automationWorkerService struct {
	worker AutomationWorker
}

// AutomationWorkerServer 是手写 gRPC 描述符使用的 worker 服务接口标记。
type AutomationWorkerServer interface {
	mustEmbedAutomationWorkerServer()
}

func (*automationWorkerService) mustEmbedAutomationWorkerServer() {}

var automationWorkerServiceDesc = googlegrpc.ServiceDesc{
	ServiceName: AutomationWorkerServiceName,
	HandlerType: (*AutomationWorkerServer)(nil),
	Methods: []googlegrpc.MethodDesc{
		{MethodName: AutomationWorkerDispatchMethod, Handler: workerUnary("DispatchTask", func(ctx context.Context, s *automationWorkerService, req automation.DispatchRequest) (any, error) {
			return s.worker.DispatchTask(ctx, req)
		})},
		{MethodName: AutomationWorkerHealthMethod, Handler: workerUnary("Health", func(ctx context.Context, s *automationWorkerService, req EmptyRequest) (any, error) {
			return s.worker.Health(ctx)
		})},
	},
	Streams:  []googlegrpc.StreamDesc{},
	Metadata: "gaia/workflow/v1/automation_worker.json",
}

func workerUnary[T any](method string, fn func(context.Context, *automationWorkerService, T) (any, error)) googlegrpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor googlegrpc.UnaryServerInterceptor) (any, error) {
		var req T
		if err := dec(&req); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		svc, ok := srv.(*automationWorkerService)
		if !ok || svc.worker == nil {
			return nil, status.Error(codes.Internal, "automation worker grpc service is nil")
		}
		handler := func(ctx context.Context, request any) (any, error) {
			result, err := fn(ctx, svc, request.(T))
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			return result, nil
		}
		if interceptor == nil {
			return handler(ctx, req)
		}
		info := &googlegrpc.UnaryServerInfo{
			Server:     srv,
			FullMethod: "/" + AutomationWorkerServiceName + "/" + method,
		}
		return interceptor(ctx, req, info, handler)
	}
}
