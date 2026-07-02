package grpc

import (
	"context"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	workflowpb "github.com/xxzhwl/gaia/components/workflow/transport/grpc/proto"
	googlegrpc "google.golang.org/grpc"
)

// Client 是基于生成式 protobuf/gRPC 契约的 workflow 客户端。
type Client struct {
	cli workflowpb.WorkflowServiceClient
}

// NewClient 创建 workflow gRPC 客户端。
func NewClient(conn googlegrpc.ClientConnInterface) *Client {
	return &Client{cli: workflowpb.NewWorkflowServiceClient(conn)}
}

func (c *Client) CreateDefinition(ctx context.Context, def domain.ProcessDefinition, opts ...googlegrpc.CallOption) (domain.ProcessDefinition, error) {
	req, err := processDefinitionToProto(def)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	resp, err := c.cli.CreateDefinition(ctx, &workflowpb.ProcessDefinitionRequest{Definition: req}, opts...)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromProto(resp)
}

func (c *Client) UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition, opts ...googlegrpc.CallOption) (domain.ProcessDefinition, error) {
	req, err := processDefinitionToProto(def)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	resp, err := c.cli.UpdateDefinition(ctx, &workflowpb.UpdateDefinitionRequest{DefinitionId: definitionID, Definition: req}, opts...)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromProto(resp)
}

func (c *Client) DeployDefinition(ctx context.Context, def domain.ProcessDefinition, opts ...googlegrpc.CallOption) (domain.ProcessDefinition, error) {
	req, err := processDefinitionToProto(def)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	resp, err := c.cli.DeployDefinition(ctx, req, opts...)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromProto(resp)
}

func (c *Client) GetDefinition(ctx context.Context, definitionID string, opts ...googlegrpc.CallOption) (domain.ProcessDefinition, error) {
	resp, err := c.cli.GetDefinition(ctx, &workflowpb.GetDefinitionRequest{DefinitionId: definitionID}, opts...)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromProto(resp)
}

func (c *Client) ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter, opts ...googlegrpc.CallOption) (domain.PageResult[domain.ProcessDefinition], error) {
	resp, err := c.cli.ListDefinitions(ctx, &workflowpb.ListDefinitionsRequest{
		Page:   pageRequestToProto(filter.PageRequest),
		Key:    filter.Key,
		Name:   filter.Name,
		Status: string(filter.Status),
	}, opts...)
	if err != nil {
		return domain.PageResult[domain.ProcessDefinition]{}, err
	}
	result := domain.PageResult[domain.ProcessDefinition]{Total: resp.GetTotal()}
	for _, item := range resp.GetList() {
		def, err := processDefinitionFromProto(item)
		if err != nil {
			return domain.PageResult[domain.ProcessDefinition]{}, err
		}
		result.List = append(result.List, def)
	}
	return result, nil
}

func (c *Client) DisableDefinition(ctx context.Context, definitionID string, opts ...googlegrpc.CallOption) (domain.ProcessDefinition, error) {
	resp, err := c.cli.DisableDefinition(ctx, &workflowpb.GetDefinitionRequest{DefinitionId: definitionID}, opts...)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromProto(resp)
}

func (c *Client) EnableDefinition(ctx context.Context, definitionID string, opts ...googlegrpc.CallOption) (domain.ProcessDefinition, error) {
	resp, err := c.cli.EnableDefinition(ctx, &workflowpb.GetDefinitionRequest{DefinitionId: definitionID}, opts...)
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromProto(resp)
}

func (c *Client) StartProcess(ctx context.Context, req workflowengine.StartProcessRequest, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	pbReq, err := startProcessRequestToProto(req)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	resp, err := c.cli.StartProcess(ctx, pbReq, opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) GetInstance(ctx context.Context, instanceID string, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	resp, err := c.cli.GetInstance(ctx, &workflowpb.GetInstanceRequest{InstanceId: instanceID}, opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) ListInstances(ctx context.Context, filter domain.InstanceListFilter, opts ...googlegrpc.CallOption) (domain.PageResult[domain.ProcessInstance], error) {
	resp, err := c.cli.ListInstances(ctx, &workflowpb.ListInstancesRequest{
		Page:          pageRequestToProto(filter.PageRequest),
		DefinitionKey: filter.DefinitionKey,
		Status:        string(filter.Status),
		TenantId:      filter.TenantID,
	}, opts...)
	if err != nil {
		return domain.PageResult[domain.ProcessInstance]{}, err
	}
	result := domain.PageResult[domain.ProcessInstance]{Total: resp.GetTotal()}
	for _, item := range resp.GetList() {
		result.List = append(result.List, processInstanceFromProto(item))
	}
	return result, nil
}

func (c *Client) ListTasks(ctx context.Context, filter domain.TaskListFilter, opts ...googlegrpc.CallOption) (domain.PageResult[domain.Task], error) {
	resp, err := c.cli.ListTasks(ctx, &workflowpb.ListTasksRequest{
		Page:       pageRequestToProto(filter.PageRequest),
		InstanceId: filter.InstanceID,
		Status:     string(filter.Status),
		Type:       string(filter.Type),
		Assignee:   filter.Assignee,
		TenantId:   filter.TenantID,
	}, opts...)
	if err != nil {
		return domain.PageResult[domain.Task]{}, err
	}
	result := domain.PageResult[domain.Task]{Total: resp.GetTotal()}
	for _, item := range resp.GetList() {
		result.List = append(result.List, taskFromProto(item))
	}
	return result, nil
}

func (c *Client) Tasks(ctx context.Context, instanceID string, opts ...googlegrpc.CallOption) ([]domain.Task, error) {
	resp, err := c.cli.InstanceTasks(ctx, &workflowpb.GetInstanceRequest{InstanceId: instanceID}, opts...)
	if err != nil {
		return nil, err
	}
	tasks := make([]domain.Task, 0, len(resp.GetList()))
	for _, item := range resp.GetList() {
		tasks = append(tasks, taskFromProto(item))
	}
	return tasks, nil
}

func (c *Client) Variables(ctx context.Context, instanceID string, opts ...googlegrpc.CallOption) (map[string]domain.Variable, error) {
	resp, err := c.cli.Variables(ctx, &workflowpb.GetInstanceRequest{InstanceId: instanceID}, opts...)
	if err != nil {
		return nil, err
	}
	return variablesResponseFromProto(resp), nil
}

func (c *Client) VariablesByNames(ctx context.Context, instanceID string, names []string, opts ...googlegrpc.CallOption) (map[string]domain.Variable, error) {
	resp, err := c.cli.VariablesByNames(ctx, &workflowpb.VariableNamesRequest{
		InstanceId: instanceID,
		Names:      append([]string(nil), names...),
	}, opts...)
	if err != nil {
		return nil, err
	}
	return variablesResponseFromProto(resp), nil
}

func (c *Client) UpdateVariables(ctx context.Context, req workflowengine.UpdateVariablesRequest, opts ...googlegrpc.CallOption) (map[string]domain.Variable, error) {
	pbReq, err := updateVariablesRequestToProto(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.cli.UpdateVariables(ctx, pbReq, opts...)
	if err != nil {
		return nil, err
	}
	return variablesResponseFromProto(resp), nil
}

func (c *Client) DeleteVariables(ctx context.Context, req workflowengine.VariableNamesRequest, opts ...googlegrpc.CallOption) (map[string]domain.Variable, error) {
	resp, err := c.cli.DeleteVariables(ctx, &workflowpb.VariableNamesRequest{
		InstanceId: req.InstanceID,
		Names:      append([]string(nil), req.Names...),
	}, opts...)
	if err != nil {
		return nil, err
	}
	return variablesResponseFromProto(resp), nil
}

func (c *Client) SystemVariables(ctx context.Context, instanceID string, opts ...googlegrpc.CallOption) (workflowengine.InstanceSystemVariables, error) {
	resp, err := c.cli.SystemVariables(ctx, &workflowpb.GetInstanceRequest{InstanceId: instanceID}, opts...)
	if err != nil {
		return workflowengine.InstanceSystemVariables{}, err
	}
	return systemVariablesFromProto(resp), nil
}

func (c *Client) Timeline(ctx context.Context, instanceID string, opts ...googlegrpc.CallOption) ([]domain.AuditEvent, error) {
	resp, err := c.cli.Timeline(ctx, &workflowpb.GetInstanceRequest{InstanceId: instanceID}, opts...)
	if err != nil {
		return nil, err
	}
	events := make([]domain.AuditEvent, 0, len(resp.GetList()))
	for _, item := range resp.GetList() {
		events = append(events, auditEventFromProto(item))
	}
	return events, nil
}

func (c *Client) CompleteTask(ctx context.Context, req workflowengine.CompleteTaskRequest, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	pbReq, err := completeTaskRequestToProto(req)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	resp, err := c.cli.CompleteTask(ctx, pbReq, opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) FailTask(ctx context.Context, req workflowengine.FailTaskRequest, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	pbReq, err := failTaskRequestToProto(req)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	resp, err := c.cli.FailTask(ctx, pbReq, opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) RetryTask(ctx context.Context, req workflowengine.RetryTaskRequest, opts ...googlegrpc.CallOption) (domain.Task, error) {
	resp, err := c.cli.RetryTask(ctx, retryTaskRequestToProto(req), opts...)
	if err != nil {
		return domain.Task{}, err
	}
	return taskFromProto(resp), nil
}

func (c *Client) ClaimTask(ctx context.Context, req workflowengine.TaskOperationRequest, opts ...googlegrpc.CallOption) (domain.Task, error) {
	pbReq, err := taskOperationRequestToProto(req)
	if err != nil {
		return domain.Task{}, err
	}
	resp, err := c.cli.ClaimTask(ctx, pbReq, opts...)
	if err != nil {
		return domain.Task{}, err
	}
	return taskFromProto(resp), nil
}

func (c *Client) UnclaimTask(ctx context.Context, req workflowengine.TaskOperationRequest, opts ...googlegrpc.CallOption) (domain.Task, error) {
	pbReq, err := taskOperationRequestToProto(req)
	if err != nil {
		return domain.Task{}, err
	}
	resp, err := c.cli.UnclaimTask(ctx, pbReq, opts...)
	if err != nil {
		return domain.Task{}, err
	}
	return taskFromProto(resp), nil
}

func (c *Client) TransferTask(ctx context.Context, req workflowengine.TaskOperationRequest, opts ...googlegrpc.CallOption) (domain.Task, error) {
	pbReq, err := taskOperationRequestToProto(req)
	if err != nil {
		return domain.Task{}, err
	}
	resp, err := c.cli.TransferTask(ctx, pbReq, opts...)
	if err != nil {
		return domain.Task{}, err
	}
	return taskFromProto(resp), nil
}

func (c *Client) DelegateTask(ctx context.Context, req workflowengine.TaskOperationRequest, opts ...googlegrpc.CallOption) (domain.Task, error) {
	pbReq, err := taskOperationRequestToProto(req)
	if err != nil {
		return domain.Task{}, err
	}
	resp, err := c.cli.DelegateTask(ctx, pbReq, opts...)
	if err != nil {
		return domain.Task{}, err
	}
	return taskFromProto(resp), nil
}

func (c *Client) RejectTask(ctx context.Context, req workflowengine.TaskOperationRequest, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	pbReq, err := taskOperationRequestToProto(req)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	resp, err := c.cli.RejectTask(ctx, pbReq, opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) TerminateInstance(ctx context.Context, req workflowengine.InstanceLifecycleRequest, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	resp, err := c.cli.TerminateInstance(ctx, lifecycleRequestToProto(req), opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) SuspendInstance(ctx context.Context, req workflowengine.InstanceLifecycleRequest, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	resp, err := c.cli.SuspendInstance(ctx, lifecycleRequestToProto(req), opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) ResumeInstance(ctx context.Context, req workflowengine.InstanceLifecycleRequest, opts ...googlegrpc.CallOption) (domain.ProcessInstance, error) {
	resp, err := c.cli.ResumeInstance(ctx, lifecycleRequestToProto(req), opts...)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromProto(resp), nil
}

func (c *Client) ScanTimeoutTasks(ctx context.Context, req workflowengine.ScanTimeoutTasksRequest, opts ...googlegrpc.CallOption) (workflowengine.ScanTimeoutTasksResult, error) {
	resp, err := c.cli.ScanTimeoutTasks(ctx, scanTimeoutRequestToProto(req), opts...)
	if err != nil {
		return workflowengine.ScanTimeoutTasksResult{}, err
	}
	return scanTimeoutResultFromProto(resp)
}

func (c *Client) Outbox(ctx context.Context, filter domain.OutboxListFilter, opts ...googlegrpc.CallOption) (domain.PageResult[domain.OutboxEvent], error) {
	resp, err := c.cli.Outbox(ctx, &workflowpb.OutboxRequest{
		Page:      pageRequestToProto(filter.PageRequest),
		EventType: filter.EventType,
		Status:    string(filter.Status),
	}, opts...)
	if err != nil {
		return domain.PageResult[domain.OutboxEvent]{}, err
	}
	result := domain.PageResult[domain.OutboxEvent]{Total: resp.GetTotal()}
	for _, item := range resp.GetList() {
		result.List = append(result.List, outboxEventFromProto(item))
	}
	return result, nil
}

func (c *Client) PurgeOutbox(ctx context.Context, limit int, opts ...googlegrpc.CallOption) (int64, error) {
	resp, err := c.cli.PurgeOutbox(ctx, &workflowpb.PurgeOutboxRequest{Limit: int32(limit)}, opts...)
	if err != nil {
		return 0, err
	}
	return resp.GetPurged(), nil
}

func (c *Client) RegisterAutomationService(ctx context.Context, service automation.Service, opts ...googlegrpc.CallOption) (automation.Service, error) {
	req, err := automationServiceToProto(service)
	if err != nil {
		return automation.Service{}, err
	}
	resp, err := c.cli.RegisterAutomationService(ctx, req, opts...)
	if err != nil {
		return automation.Service{}, err
	}
	return automationServiceFromProto(resp)
}

func (c *Client) UnregisterAutomationService(ctx context.Context, serviceID string, opts ...googlegrpc.CallOption) error {
	_, err := c.cli.UnregisterAutomationService(ctx, &workflowpb.AutomationServiceID{ServiceId: serviceID}, opts...)
	return err
}

func (c *Client) ListAutomationServices(ctx context.Context, opts ...googlegrpc.CallOption) ([]automation.Service, error) {
	resp, err := c.cli.ListAutomationServices(ctx, &workflowpb.EmptyRequest{}, opts...)
	if err != nil {
		return nil, err
	}
	services := make([]automation.Service, 0, len(resp.GetList()))
	for _, item := range resp.GetList() {
		service, err := automationServiceFromProto(item)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	return services, nil
}

func (c *Client) ListAutomationTasks(ctx context.Context, opts ...googlegrpc.CallOption) ([]automation.Task, error) {
	resp, err := c.cli.ListAutomationTasks(ctx, &workflowpb.EmptyRequest{}, opts...)
	if err != nil {
		return nil, err
	}
	tasks := make([]automation.Task, 0, len(resp.GetList()))
	for _, item := range resp.GetList() {
		tasks = append(tasks, automationTaskFromProto(item))
	}
	return tasks, nil
}

// AutomationWorkerClient 是基于生成式 protobuf/gRPC 契约的 worker 客户端。
type AutomationWorkerClient struct {
	cli workflowpb.AutomationWorkerServiceClient
}

// NewAutomationWorkerClient 创建自动化 worker gRPC 客户端。
func NewAutomationWorkerClient(conn googlegrpc.ClientConnInterface) *AutomationWorkerClient {
	return &AutomationWorkerClient{cli: workflowpb.NewAutomationWorkerServiceClient(conn)}
}

func (c *AutomationWorkerClient) DispatchTask(ctx context.Context, req automation.DispatchRequest, opts ...googlegrpc.CallOption) (automation.DispatchResult, error) {
	pbReq, err := dispatchRequestToProto(req)
	if err != nil {
		return automation.DispatchResult{}, err
	}
	resp, err := c.cli.DispatchTask(ctx, pbReq, opts...)
	if err != nil {
		return automation.DispatchResult{}, err
	}
	return dispatchResultFromProto(resp), nil
}
