// Package grpc 提供工作流组件的 gRPC 适配层。
package grpc

import (
	"context"
	"encoding/json"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
)

// Engine 定义 gRPC 适配层需要调用的工作流能力。
type Engine interface {
	CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error)
	StartProcess(ctx context.Context, req engine.StartProcessRequest) (domain.ProcessInstance, error)
	GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error)
	ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error)
	ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error)
	Tasks(ctx context.Context, instanceID string) ([]domain.Task, error)
	Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error)
	Timeline(ctx context.Context, instanceID string) ([]domain.AuditEvent, error)
	Outbox(ctx context.Context) ([]domain.OutboxEvent, error)
	CompleteTask(ctx context.Context, req engine.CompleteTaskRequest) (domain.ProcessInstance, error)
	ClaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	UnclaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	TransferTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	DelegateTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	RejectTask(ctx context.Context, req engine.TaskOperationRequest) (domain.ProcessInstance, error)
	ScanTimeoutTasks(ctx context.Context, req engine.ScanTimeoutTasksRequest) (engine.ScanTimeoutTasksResult, error)
	RegisterAutomationService(ctx context.Context, service automation.Service) (automation.Service, error)
	UnregisterAutomationService(ctx context.Context, serviceID string) error
	ListAutomationServices(ctx context.Context) ([]automation.Service, error)
	ListAutomationTasks(ctx context.Context) ([]automation.Task, error)
}

// RegisterWorkflowService 将工作流服务注册到 gRPC Server。
func RegisterWorkflowService(server googlegrpc.ServiceRegistrar, engine Engine) {
	server.RegisterService(&workflowServiceDesc, &service{engine: engine})
}

// DefinitionRequest 表示流程定义相关 gRPC 请求。
type DefinitionRequest struct {
	DefinitionID string
	Definition   domain.ProcessDefinition
	Filter       domain.DefinitionListFilter
}

// InstanceRequest 表示流程实例相关 gRPC 请求。
type InstanceRequest struct {
	InstanceID string
	Filter     domain.InstanceListFilter
	Start      engine.StartProcessRequest
}

// TaskRequest 表示任务相关 gRPC 请求。
type TaskRequest struct {
	TaskID    string
	Filter    domain.TaskListFilter
	Complete  engine.CompleteTaskRequest
	Operation engine.TaskOperationRequest
}

// AutomationRequest 表示自动化服务相关 gRPC 请求。
type AutomationRequest struct {
	ServiceID string
	Service   automation.Service
}

// EmptyRequest 表示无参数 gRPC 请求。
type EmptyRequest struct{}

type service struct {
	engine Engine
}

// WorkflowServiceServer 是手写 gRPC 描述符使用的服务接口标记。
type WorkflowServiceServer interface {
	mustEmbedWorkflowServiceServer()
}

func (*service) mustEmbedWorkflowServiceServer() {}

type jsonCodec struct{}

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

func (jsonCodec) Name() string {
	return "json"
}

func (jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

var workflowServiceDesc = googlegrpc.ServiceDesc{
	ServiceName: "gaia.workflow.v1.WorkflowService",
	HandlerType: (*WorkflowServiceServer)(nil),
	Methods: []googlegrpc.MethodDesc{
		{MethodName: "CreateDefinition", Handler: unary("CreateDefinition", func(ctx context.Context, s *service, req DefinitionRequest) (any, error) {
			return s.engine.CreateDefinition(ctx, req.Definition)
		})},
		{MethodName: "UpdateDefinition", Handler: unary("UpdateDefinition", func(ctx context.Context, s *service, req DefinitionRequest) (any, error) {
			return s.engine.UpdateDefinition(ctx, req.DefinitionID, req.Definition)
		})},
		{MethodName: "DeployDefinition", Handler: unary("DeployDefinition", func(ctx context.Context, s *service, req DefinitionRequest) (any, error) {
			return s.engine.DeployDefinition(ctx, req.Definition)
		})},
		{MethodName: "GetDefinition", Handler: unary("GetDefinition", func(ctx context.Context, s *service, req DefinitionRequest) (any, error) {
			return s.engine.GetDefinition(ctx, req.DefinitionID)
		})},
		{MethodName: "ListDefinitions", Handler: unary("ListDefinitions", func(ctx context.Context, s *service, req DefinitionRequest) (any, error) {
			return s.engine.ListDefinitions(ctx, req.Filter)
		})},
		{MethodName: "StartProcess", Handler: unary("StartProcess", func(ctx context.Context, s *service, req InstanceRequest) (any, error) {
			return s.engine.StartProcess(ctx, req.Start)
		})},
		{MethodName: "GetInstance", Handler: unary("GetInstance", func(ctx context.Context, s *service, req InstanceRequest) (any, error) {
			return s.engine.GetInstance(ctx, req.InstanceID)
		})},
		{MethodName: "ListInstances", Handler: unary("ListInstances", func(ctx context.Context, s *service, req InstanceRequest) (any, error) {
			return s.engine.ListInstances(ctx, req.Filter)
		})},
		{MethodName: "ListTasks", Handler: unary("ListTasks", func(ctx context.Context, s *service, req TaskRequest) (any, error) {
			return s.engine.ListTasks(ctx, req.Filter)
		})},
		{MethodName: "InstanceTasks", Handler: unary("InstanceTasks", func(ctx context.Context, s *service, req InstanceRequest) (any, error) {
			return s.engine.Tasks(ctx, req.InstanceID)
		})},
		{MethodName: "Variables", Handler: unary("Variables", func(ctx context.Context, s *service, req InstanceRequest) (any, error) {
			return s.engine.Variables(ctx, req.InstanceID)
		})},
		{MethodName: "Timeline", Handler: unary("Timeline", func(ctx context.Context, s *service, req InstanceRequest) (any, error) {
			return s.engine.Timeline(ctx, req.InstanceID)
		})},
		{MethodName: "Outbox", Handler: unary("Outbox", func(ctx context.Context, s *service, req EmptyRequest) (any, error) {
			return s.engine.Outbox(ctx)
		})},
		{MethodName: "CompleteTask", Handler: unary("CompleteTask", func(ctx context.Context, s *service, req TaskRequest) (any, error) {
			return s.engine.CompleteTask(ctx, req.Complete)
		})},
		{MethodName: "ClaimTask", Handler: unary("ClaimTask", func(ctx context.Context, s *service, req TaskRequest) (any, error) {
			return s.engine.ClaimTask(ctx, req.Operation)
		})},
		{MethodName: "UnclaimTask", Handler: unary("UnclaimTask", func(ctx context.Context, s *service, req TaskRequest) (any, error) {
			return s.engine.UnclaimTask(ctx, req.Operation)
		})},
		{MethodName: "TransferTask", Handler: unary("TransferTask", func(ctx context.Context, s *service, req TaskRequest) (any, error) {
			return s.engine.TransferTask(ctx, req.Operation)
		})},
		{MethodName: "DelegateTask", Handler: unary("DelegateTask", func(ctx context.Context, s *service, req TaskRequest) (any, error) {
			return s.engine.DelegateTask(ctx, req.Operation)
		})},
		{MethodName: "RejectTask", Handler: unary("RejectTask", func(ctx context.Context, s *service, req TaskRequest) (any, error) {
			return s.engine.RejectTask(ctx, req.Operation)
		})},
		{MethodName: "ScanTimeoutTasks", Handler: unary("ScanTimeoutTasks", func(ctx context.Context, s *service, req engine.ScanTimeoutTasksRequest) (any, error) {
			return s.engine.ScanTimeoutTasks(ctx, req)
		})},
		{MethodName: "RegisterAutomationService", Handler: unary("RegisterAutomationService", func(ctx context.Context, s *service, req AutomationRequest) (any, error) {
			return s.engine.RegisterAutomationService(ctx, req.Service)
		})},
		{MethodName: "UnregisterAutomationService", Handler: unary("UnregisterAutomationService", func(ctx context.Context, s *service, req AutomationRequest) (any, error) {
			return map[string]bool{"deleted": true}, s.engine.UnregisterAutomationService(ctx, req.ServiceID)
		})},
		{MethodName: "ListAutomationServices", Handler: unary("ListAutomationServices", func(ctx context.Context, s *service, req EmptyRequest) (any, error) {
			return s.engine.ListAutomationServices(ctx)
		})},
		{MethodName: "ListAutomationTasks", Handler: unary("ListAutomationTasks", func(ctx context.Context, s *service, req EmptyRequest) (any, error) {
			return s.engine.ListAutomationTasks(ctx)
		})},
	},
	Streams:  []googlegrpc.StreamDesc{},
	Metadata: "gaia/workflow/v1/workflow.json",
}

func unary[T any](method string, fn func(context.Context, *service, T) (any, error)) googlegrpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor googlegrpc.UnaryServerInterceptor) (any, error) {
		var req T
		if err := dec(&req); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		svc, ok := srv.(*service)
		if !ok || svc.engine == nil {
			return nil, status.Error(codes.Internal, "workflow grpc service engine is nil")
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
			FullMethod: "/gaia.workflow.v1.WorkflowService/" + method,
		}
		return interceptor(ctx, req, info, handler)
	}
}
