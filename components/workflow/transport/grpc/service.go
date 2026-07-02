// Package grpc 提供工作流组件的 gRPC 适配层。
package grpc

import (
	"context"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
	workflowpb "github.com/xxzhwl/gaia/components/workflow/transport/grpc/proto"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// WorkflowServiceName 是工作流引擎对外暴露的 gRPC 服务名。
	WorkflowServiceName = "gaia.workflow.v1.WorkflowService"
)

// Engine 定义 gRPC 适配层需要调用的工作流能力。
type Engine interface {
	CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	DisableDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	EnableDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error)
	StartProcess(ctx context.Context, req engine.StartProcessRequest) (domain.ProcessInstance, error)
	GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error)
	ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error)
	ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error)
	Tasks(ctx context.Context, instanceID string) ([]domain.Task, error)
	Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error)
	VariablesByNames(ctx context.Context, instanceID string, names []string) (map[string]domain.Variable, error)
	UpdateVariables(ctx context.Context, req engine.UpdateVariablesRequest) (map[string]domain.Variable, error)
	DeleteVariables(ctx context.Context, req engine.VariableNamesRequest) (map[string]domain.Variable, error)
	SystemVariables(ctx context.Context, instanceID string) (engine.InstanceSystemVariables, error)
	Timeline(ctx context.Context, instanceID string) ([]domain.AuditEvent, error)
	Outbox(ctx context.Context, filter domain.OutboxListFilter) (domain.PageResult[domain.OutboxEvent], error)
	PurgeOutbox(ctx context.Context, limit int) (int64, error)
	CompleteTask(ctx context.Context, req engine.CompleteTaskRequest) (domain.ProcessInstance, error)
	FailTask(ctx context.Context, req engine.FailTaskRequest) (domain.ProcessInstance, error)
	RetryTask(ctx context.Context, req engine.RetryTaskRequest) (domain.Task, error)
	ClaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	UnclaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	TransferTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	DelegateTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	RejectTask(ctx context.Context, req engine.TaskOperationRequest) (domain.ProcessInstance, error)
	ScanTimeoutTasks(ctx context.Context, req engine.ScanTimeoutTasksRequest) (engine.ScanTimeoutTasksResult, error)
	TerminateInstance(ctx context.Context, req engine.InstanceLifecycleRequest) (domain.ProcessInstance, error)
	SuspendInstance(ctx context.Context, req engine.InstanceLifecycleRequest) (domain.ProcessInstance, error)
	ResumeInstance(ctx context.Context, req engine.InstanceLifecycleRequest) (domain.ProcessInstance, error)
	RegisterAutomationService(ctx context.Context, service automation.Service) (automation.Service, error)
	UnregisterAutomationService(ctx context.Context, serviceID string) error
	ListAutomationServices(ctx context.Context) ([]automation.Service, error)
	ListAutomationTasks(ctx context.Context) ([]automation.Task, error)
}

// RegisterWorkflowService 将工作流服务注册到 gRPC Server。
func RegisterWorkflowService(server googlegrpc.ServiceRegistrar, engine Engine) {
	workflowpb.RegisterWorkflowServiceServer(server, &service{engine: engine})
}

type service struct {
	workflowpb.UnimplementedWorkflowServiceServer

	engine Engine
}

func (s *service) CreateDefinition(ctx context.Context, req *workflowpb.ProcessDefinitionRequest) (*workflowpb.ProcessDefinition, error) {
	if s.engine == nil {
		return nil, status.Error(codes.Internal, "workflow grpc service engine is nil")
	}
	def, err := processDefinitionFromProto(req.GetDefinition())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.engine.CreateDefinition(ctx, def)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processDefinitionToProto(result)
}

func (s *service) UpdateDefinition(ctx context.Context, req *workflowpb.UpdateDefinitionRequest) (*workflowpb.ProcessDefinition, error) {
	def, err := processDefinitionFromProto(req.GetDefinition())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.engine.UpdateDefinition(ctx, req.GetDefinitionId(), def)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processDefinitionToProto(result)
}

func (s *service) DeployDefinition(ctx context.Context, req *workflowpb.ProcessDefinition) (*workflowpb.ProcessDefinition, error) {
	def, err := processDefinitionFromProto(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.engine.DeployDefinition(ctx, def)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processDefinitionToProto(result)
}

func (s *service) GetDefinition(ctx context.Context, req *workflowpb.GetDefinitionRequest) (*workflowpb.ProcessDefinition, error) {
	result, err := s.engine.GetDefinition(ctx, req.GetDefinitionId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processDefinitionToProto(result)
}

func (s *service) ListDefinitions(ctx context.Context, req *workflowpb.ListDefinitionsRequest) (*workflowpb.ProcessDefinitionPage, error) {
	result, err := s.engine.ListDefinitions(ctx, domain.DefinitionListFilter{
		PageRequest: pageRequestFromProto(req.GetPage()),
		Key:         req.GetKey(),
		Name:        req.GetName(),
		Status:      domain.DefinitionStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.ProcessDefinition, 0, len(result.List))
	for _, def := range result.List {
		item, err := processDefinitionToProto(def)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		list = append(list, item)
	}
	return &workflowpb.ProcessDefinitionPage{List: list, Total: result.Total}, nil
}

func (s *service) DisableDefinition(ctx context.Context, req *workflowpb.GetDefinitionRequest) (*workflowpb.ProcessDefinition, error) {
	result, err := s.engine.DisableDefinition(ctx, req.GetDefinitionId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processDefinitionToProto(result)
}

func (s *service) EnableDefinition(ctx context.Context, req *workflowpb.GetDefinitionRequest) (*workflowpb.ProcessDefinition, error) {
	result, err := s.engine.EnableDefinition(ctx, req.GetDefinitionId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processDefinitionToProto(result)
}

func (s *service) StartProcess(ctx context.Context, req *workflowpb.StartProcessRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.StartProcess(ctx, startProcessRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) GetInstance(ctx context.Context, req *workflowpb.GetInstanceRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.GetInstance(ctx, req.GetInstanceId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) ListInstances(ctx context.Context, req *workflowpb.ListInstancesRequest) (*workflowpb.ProcessInstancePage, error) {
	result, err := s.engine.ListInstances(ctx, domain.InstanceListFilter{
		PageRequest:   pageRequestFromProto(req.GetPage()),
		DefinitionKey: req.GetDefinitionKey(),
		Status:        domain.InstanceStatus(req.GetStatus()),
		TenantID:      req.GetTenantId(),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.ProcessInstance, 0, len(result.List))
	for _, instance := range result.List {
		list = append(list, processInstanceToProto(instance))
	}
	return &workflowpb.ProcessInstancePage{List: list, Total: result.Total}, nil
}

func (s *service) TerminateInstance(ctx context.Context, req *workflowpb.InstanceLifecycleRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.TerminateInstance(ctx, lifecycleRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) SuspendInstance(ctx context.Context, req *workflowpb.InstanceLifecycleRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.SuspendInstance(ctx, lifecycleRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) ResumeInstance(ctx context.Context, req *workflowpb.InstanceLifecycleRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.ResumeInstance(ctx, lifecycleRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) ListTasks(ctx context.Context, req *workflowpb.ListTasksRequest) (*workflowpb.TaskPage, error) {
	result, err := s.engine.ListTasks(ctx, domain.TaskListFilter{
		PageRequest: pageRequestFromProto(req.GetPage()),
		InstanceID:  req.GetInstanceId(),
		Status:      domain.TaskStatus(req.GetStatus()),
		Type:        domain.TaskType(req.GetType()),
		Assignee:    req.GetAssignee(),
		TenantID:    req.GetTenantId(),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.Task, 0, len(result.List))
	for _, task := range result.List {
		item, err := taskToProto(task)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		list = append(list, item)
	}
	return &workflowpb.TaskPage{List: list, Total: result.Total}, nil
}

func (s *service) InstanceTasks(ctx context.Context, req *workflowpb.GetInstanceRequest) (*workflowpb.TaskList, error) {
	result, err := s.engine.Tasks(ctx, req.GetInstanceId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.Task, 0, len(result))
	for _, task := range result {
		item, err := taskToProto(task)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		list = append(list, item)
	}
	return &workflowpb.TaskList{List: list}, nil
}

func (s *service) Variables(ctx context.Context, req *workflowpb.GetInstanceRequest) (*workflowpb.VariablesResponse, error) {
	result, err := s.engine.Variables(ctx, req.GetInstanceId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp, err := variablesResponseToProto(result)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (s *service) VariablesByNames(ctx context.Context, req *workflowpb.VariableNamesRequest) (*workflowpb.VariablesResponse, error) {
	result, err := s.engine.VariablesByNames(ctx, req.GetInstanceId(), req.GetNames())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp, err := variablesResponseToProto(result)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (s *service) UpdateVariables(ctx context.Context, req *workflowpb.UpdateVariablesRequest) (*workflowpb.VariablesResponse, error) {
	result, err := s.engine.UpdateVariables(ctx, updateVariablesRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp, err := variablesResponseToProto(result)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (s *service) DeleteVariables(ctx context.Context, req *workflowpb.VariableNamesRequest) (*workflowpb.VariablesResponse, error) {
	result, err := s.engine.DeleteVariables(ctx, variableNamesRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp, err := variablesResponseToProto(result)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (s *service) SystemVariables(ctx context.Context, req *workflowpb.GetInstanceRequest) (*workflowpb.InstanceSystemVariables, error) {
	result, err := s.engine.SystemVariables(ctx, req.GetInstanceId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return systemVariablesToProto(result), nil
}

func (s *service) Timeline(ctx context.Context, req *workflowpb.GetInstanceRequest) (*workflowpb.AuditEventList, error) {
	result, err := s.engine.Timeline(ctx, req.GetInstanceId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.AuditEvent, 0, len(result))
	for _, event := range result {
		item, err := auditEventToProto(event)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		list = append(list, item)
	}
	return &workflowpb.AuditEventList{List: list}, nil
}

func (s *service) CompleteTask(ctx context.Context, req *workflowpb.CompleteTaskRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.CompleteTask(ctx, completeTaskRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) FailTask(ctx context.Context, req *workflowpb.FailTaskRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.FailTask(ctx, failTaskRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) RetryTask(ctx context.Context, req *workflowpb.RetryTaskRequest) (*workflowpb.Task, error) {
	result, err := s.engine.RetryTask(ctx, retryTaskRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToProto(result)
}

func (s *service) ClaimTask(ctx context.Context, req *workflowpb.TaskOperationRequest) (*workflowpb.Task, error) {
	result, err := s.engine.ClaimTask(ctx, taskOperationRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToProto(result)
}

func (s *service) UnclaimTask(ctx context.Context, req *workflowpb.TaskOperationRequest) (*workflowpb.Task, error) {
	result, err := s.engine.UnclaimTask(ctx, taskOperationRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToProto(result)
}

func (s *service) TransferTask(ctx context.Context, req *workflowpb.TaskOperationRequest) (*workflowpb.Task, error) {
	result, err := s.engine.TransferTask(ctx, taskOperationRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToProto(result)
}

func (s *service) DelegateTask(ctx context.Context, req *workflowpb.TaskOperationRequest) (*workflowpb.Task, error) {
	result, err := s.engine.DelegateTask(ctx, taskOperationRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToProto(result)
}

func (s *service) RejectTask(ctx context.Context, req *workflowpb.TaskOperationRequest) (*workflowpb.ProcessInstance, error) {
	result, err := s.engine.RejectTask(ctx, taskOperationRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return processInstanceToProto(result), nil
}

func (s *service) ScanTimeoutTasks(ctx context.Context, req *workflowpb.ScanTimeoutTasksRequest) (*workflowpb.ScanTimeoutTasksResult, error) {
	result, err := s.engine.ScanTimeoutTasks(ctx, scanTimeoutRequestFromProto(req))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return scanTimeoutResultToProto(result)
}

func (s *service) Outbox(ctx context.Context, req *workflowpb.OutboxRequest) (*workflowpb.OutboxEventPage, error) {
	result, err := s.engine.Outbox(ctx, domain.OutboxListFilter{
		PageRequest: pageRequestFromProto(req.GetPage()),
		EventType:   req.GetEventType(),
		Status:      domain.OutboxStatus(req.GetStatus()),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.OutboxEvent, 0, len(result.List))
	for _, event := range result.List {
		item, err := outboxEventToProto(event)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		list = append(list, item)
	}
	return &workflowpb.OutboxEventPage{List: list, Total: result.Total}, nil
}

func (s *service) PurgeOutbox(ctx context.Context, req *workflowpb.PurgeOutboxRequest) (*workflowpb.PurgeOutboxResponse, error) {
	count, err := s.engine.PurgeOutbox(ctx, int(req.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &workflowpb.PurgeOutboxResponse{Purged: count}, nil
}

func (s *service) RegisterAutomationService(ctx context.Context, req *workflowpb.AutomationService) (*workflowpb.AutomationService, error) {
	service, err := automationServiceFromProto(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	result, err := s.engine.RegisterAutomationService(ctx, service)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return automationServiceToProto(result)
}

func (s *service) UnregisterAutomationService(ctx context.Context, req *workflowpb.AutomationServiceID) (*workflowpb.DeleteResponse, error) {
	if err := s.engine.UnregisterAutomationService(ctx, req.GetServiceId()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &workflowpb.DeleteResponse{Deleted: true}, nil
}

func (s *service) ListAutomationServices(ctx context.Context, _ *workflowpb.EmptyRequest) (*workflowpb.AutomationServiceList, error) {
	result, err := s.engine.ListAutomationServices(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.AutomationService, 0, len(result))
	for _, item := range result {
		pb, err := automationServiceToProto(item)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		list = append(list, pb)
	}
	return &workflowpb.AutomationServiceList{List: list}, nil
}

func (s *service) ListAutomationTasks(ctx context.Context, _ *workflowpb.EmptyRequest) (*workflowpb.AutomationTaskList, error) {
	result, err := s.engine.ListAutomationTasks(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	list := make([]*workflowpb.AutomationTask, 0, len(result))
	for _, item := range result {
		pb, err := automationTaskToProto(item)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		list = append(list, pb)
	}
	return &workflowpb.AutomationTaskList{List: list}, nil
}
