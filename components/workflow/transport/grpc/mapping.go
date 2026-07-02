package grpc

import (
	"fmt"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	workflowpb "github.com/xxzhwl/gaia/components/workflow/transport/grpc/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func pageRequestFromProto(req *workflowpb.PageRequest) domain.PageRequest {
	if req == nil {
		return domain.PageRequest{}
	}
	return domain.PageRequest{
		Page:     int(req.GetPage()),
		PageSize: int(req.GetPageSize()),
	}
}

func pageRequestToProto(req domain.PageRequest) *workflowpb.PageRequest {
	return &workflowpb.PageRequest{
		Page:     int32(req.Page),
		PageSize: int32(req.PageSize),
	}
}

func processDefinitionToProto(def domain.ProcessDefinition) (*workflowpb.ProcessDefinition, error) {
	model := workflowModelToProto(def.Model)
	inputs := make([]*workflowpb.InputParameter, 0, len(def.Inputs))
	for _, input := range def.Inputs {
		value, err := valueToProto(input.DefaultValue)
		if err != nil {
			return nil, fmt.Errorf("definition input %s default value: %w", input.Key, err)
		}
		inputs = append(inputs, &workflowpb.InputParameter{
			Key:          input.Key,
			Name:         input.Name,
			Type:         input.Type,
			Required:     input.Required,
			DefaultValue: value,
		})
	}
	return &workflowpb.ProcessDefinition{
		Id:          def.ID,
		Key:         def.Key,
		Name:        def.Name,
		Version:     int32(def.Version),
		Status:      string(def.Status),
		Model:       model,
		Inputs:      inputs,
		CreatedBy:   def.CreatedBy,
		CreatedAt:   timeToProto(def.CreatedAt),
		DeployedAt:  timePtrToProto(def.DeployedAt),
		EditVersion: int32(def.EditVersion),
	}, nil
}

func processDefinitionFromProto(pb *workflowpb.ProcessDefinition) (domain.ProcessDefinition, error) {
	if pb == nil {
		return domain.ProcessDefinition{}, nil
	}
	inputs := make([]domain.InputParameter, 0, len(pb.GetInputs()))
	for _, input := range pb.GetInputs() {
		inputs = append(inputs, domain.InputParameter{
			Key:          input.GetKey(),
			Name:         input.GetName(),
			Type:         input.GetType(),
			Required:     input.GetRequired(),
			DefaultValue: valueFromProto(input.GetDefaultValue()),
		})
	}
	return domain.ProcessDefinition{
		ID:          pb.GetId(),
		Key:         pb.GetKey(),
		Name:        pb.GetName(),
		Version:     int(pb.GetVersion()),
		Status:      domain.DefinitionStatus(pb.GetStatus()),
		Model:       workflowModelFromProto(pb.GetModel()),
		Inputs:      inputs,
		CreatedBy:   pb.GetCreatedBy(),
		CreatedAt:   timeFromProto(pb.GetCreatedAt()),
		DeployedAt:  timePtrFromProto(pb.GetDeployedAt()),
		EditVersion: int(pb.GetEditVersion()),
	}, nil
}

func workflowModelToProto(model domain.WorkflowModel) *workflowpb.WorkflowModel {
	nodes := make(map[string]*workflowpb.Node, len(model.Nodes))
	for key, node := range model.Nodes {
		nodes[key] = nodeToProto(node)
	}
	flows := make([]*workflowpb.SequenceFlow, 0, len(model.SequenceFlows))
	for _, flow := range model.SequenceFlows {
		flows = append(flows, &workflowpb.SequenceFlow{
			Id:        flow.ID,
			Name:      flow.Name,
			SourceRef: flow.SourceRef,
			TargetRef: flow.TargetRef,
			Condition: flow.Condition,
			Default:   flow.Default,
		})
	}
	return &workflowpb.WorkflowModel{Nodes: nodes, SequenceFlows: flows}
}

func workflowModelFromProto(pb *workflowpb.WorkflowModel) domain.WorkflowModel {
	if pb == nil {
		return domain.WorkflowModel{}
	}
	nodes := make(map[string]domain.Node, len(pb.GetNodes()))
	for key, node := range pb.GetNodes() {
		nodes[key] = nodeFromProto(node)
	}
	flows := make([]domain.SequenceFlow, 0, len(pb.GetSequenceFlows()))
	for _, flow := range pb.GetSequenceFlows() {
		flows = append(flows, domain.SequenceFlow{
			ID:        flow.GetId(),
			Name:      flow.GetName(),
			SourceRef: flow.GetSourceRef(),
			TargetRef: flow.GetTargetRef(),
			Condition: flow.GetCondition(),
			Default:   flow.GetDefault(),
		})
	}
	return domain.WorkflowModel{Nodes: nodes, SequenceFlows: flows}
}

func nodeToProto(node domain.Node) *workflowpb.Node {
	mappings := make([]*workflowpb.InputMapping, 0, len(node.InputMappings))
	for _, mapping := range node.InputMappings {
		mappings = append(mappings, &workflowpb.InputMapping{
			Parameter:  mapping.Parameter,
			Expression: mapping.Expression,
		})
	}
	variables, _ := mapToStruct(node.SLAPolicy.Variables)
	return &workflowpb.Node{
		Id:                  node.ID,
		Type:                string(node.Type),
		Name:                node.Name,
		InputMappings:       mappings,
		OutputVariables:     append([]string(nil), node.OutputVariables...),
		Endpoint:            node.Endpoint,
		AutomationServiceId: node.AutomationServiceID,
		AutomationTaskKey:   node.AutomationTaskKey,
		AssigneeExpression:  node.AssigneeExpression,
		TimeoutSeconds:      int32(node.TimeoutSeconds),
		RetryPolicy: &workflowpb.RetryPolicy{
			MaxAttempts:    int32(node.RetryPolicy.MaxAttempts),
			BackoffSeconds: int32(node.RetryPolicy.BackoffSeconds),
		},
		SlaPolicy: &workflowpb.SLAPolicy{
			Action:    string(node.SLAPolicy.Action),
			Operator:  node.SLAPolicy.Operator,
			Reason:    node.SLAPolicy.Reason,
			Variables: variables,
		},
	}
}

func nodeFromProto(pb *workflowpb.Node) domain.Node {
	if pb == nil {
		return domain.Node{}
	}
	mappings := make([]domain.InputMapping, 0, len(pb.GetInputMappings()))
	for _, mapping := range pb.GetInputMappings() {
		mappings = append(mappings, domain.InputMapping{
			Parameter:  mapping.GetParameter(),
			Expression: mapping.GetExpression(),
		})
	}
	retry := pb.GetRetryPolicy()
	sla := pb.GetSlaPolicy()
	var policy domain.SLAPolicy
	if sla != nil {
		policy = domain.SLAPolicy{
			Action:    domain.SLAAction(sla.GetAction()),
			Operator:  sla.GetOperator(),
			Reason:    sla.GetReason(),
			Variables: structToMap(sla.GetVariables()),
		}
	}
	return domain.Node{
		ID:                  pb.GetId(),
		Type:                domain.NodeType(pb.GetType()),
		Name:                pb.GetName(),
		InputMappings:       mappings,
		OutputVariables:     append([]string(nil), pb.GetOutputVariables()...),
		Endpoint:            pb.GetEndpoint(),
		AutomationServiceID: pb.GetAutomationServiceId(),
		AutomationTaskKey:   pb.GetAutomationTaskKey(),
		AssigneeExpression:  pb.GetAssigneeExpression(),
		TimeoutSeconds:      int(pb.GetTimeoutSeconds()),
		RetryPolicy: domain.RetryPolicy{
			MaxAttempts:    int(retry.GetMaxAttempts()),
			BackoffSeconds: int(retry.GetBackoffSeconds()),
		},
		SLAPolicy: policy,
	}
}

func startProcessRequestToProto(req workflowengine.StartProcessRequest) (*workflowpb.StartProcessRequest, error) {
	variables, err := mapToStruct(req.Variables)
	if err != nil {
		return nil, err
	}
	return &workflowpb.StartProcessRequest{
		DefinitionKey:     req.DefinitionKey,
		DefinitionVersion: int32(req.DefinitionVersion),
		BusinessKey:       req.BusinessKey,
		TenantId:          req.TenantID,
		Starter:           req.Starter,
		Variables:         variables,
	}, nil
}

func startProcessRequestFromProto(pb *workflowpb.StartProcessRequest) workflowengine.StartProcessRequest {
	if pb == nil {
		return workflowengine.StartProcessRequest{}
	}
	return workflowengine.StartProcessRequest{
		DefinitionKey:     pb.GetDefinitionKey(),
		DefinitionVersion: int(pb.GetDefinitionVersion()),
		BusinessKey:       pb.GetBusinessKey(),
		TenantID:          pb.GetTenantId(),
		Starter:           pb.GetStarter(),
		Variables:         structToMap(pb.GetVariables()),
	}
}

func processInstanceToProto(instance domain.ProcessInstance) *workflowpb.ProcessInstance {
	return &workflowpb.ProcessInstance{
		Id:                instance.ID,
		DefinitionId:      instance.DefinitionID,
		DefinitionKey:     instance.DefinitionKey,
		DefinitionVersion: int32(instance.DefinitionVersion),
		BusinessKey:       instance.BusinessKey,
		TenantId:          instance.TenantID,
		Status:            string(instance.Status),
		StartTime:         timeToProto(instance.StartTime),
		EndTime:           timePtrToProto(instance.EndTime),
		Starter:           instance.Starter,
		EndNodeId:         instance.EndNodeID,
		FailReason:        instance.FailReason,
		Version:           int32(instance.Version),
	}
}

func processInstanceFromProto(pb *workflowpb.ProcessInstance) domain.ProcessInstance {
	if pb == nil {
		return domain.ProcessInstance{}
	}
	return domain.ProcessInstance{
		ID:                pb.GetId(),
		DefinitionID:      pb.GetDefinitionId(),
		DefinitionKey:     pb.GetDefinitionKey(),
		DefinitionVersion: int(pb.GetDefinitionVersion()),
		BusinessKey:       pb.GetBusinessKey(),
		TenantID:          pb.GetTenantId(),
		Status:            domain.InstanceStatus(pb.GetStatus()),
		StartTime:         timeFromProto(pb.GetStartTime()),
		EndTime:           timePtrFromProto(pb.GetEndTime()),
		Starter:           pb.GetStarter(),
		EndNodeID:         pb.GetEndNodeId(),
		FailReason:        pb.GetFailReason(),
		Version:           int(pb.GetVersion()),
	}
}

func taskToProto(task domain.Task) (*workflowpb.Task, error) {
	snapshot, err := mapToStruct(task.VariableSnapshot)
	if err != nil {
		return nil, err
	}
	return &workflowpb.Task{
		Id:                 task.ID,
		InstanceId:         task.InstanceID,
		ActivityInstanceId: task.ActivityInstanceID,
		NodeId:             task.NodeID,
		Title:              task.Title,
		Type:               string(task.Type),
		Status:             string(task.Status),
		Assignee:           task.Assignee,
		Owner:              task.Owner,
		VariableSnapshot:   snapshot,
		Action:             task.Action,
		Comment:            task.Comment,
		CompletedBy:        task.CompletedBy,
		DelegatedFrom:      task.DelegatedFrom,
		DispatchUrl:        task.DispatchURL,
		CallbackToken:      task.CallbackToken,
		TimeoutAt:          timePtrToProto(task.TimeoutAt),
		ClaimedAt:          timePtrToProto(task.ClaimedAt),
		RetryCount:         int32(task.RetryCount),
		CreatedAt:          timeToProto(task.CreatedAt),
		CompletedAt:        timePtrToProto(task.CompletedAt),
	}, nil
}

func taskFromProto(pb *workflowpb.Task) domain.Task {
	if pb == nil {
		return domain.Task{}
	}
	return domain.Task{
		ID:                 pb.GetId(),
		InstanceID:         pb.GetInstanceId(),
		ActivityInstanceID: pb.GetActivityInstanceId(),
		NodeID:             pb.GetNodeId(),
		Title:              pb.GetTitle(),
		Type:               domain.TaskType(pb.GetType()),
		Status:             domain.TaskStatus(pb.GetStatus()),
		Assignee:           pb.GetAssignee(),
		Owner:              pb.GetOwner(),
		VariableSnapshot:   structToMap(pb.GetVariableSnapshot()),
		Action:             pb.GetAction(),
		Comment:            pb.GetComment(),
		CompletedBy:        pb.GetCompletedBy(),
		DelegatedFrom:      pb.GetDelegatedFrom(),
		DispatchURL:        pb.GetDispatchUrl(),
		CallbackToken:      pb.GetCallbackToken(),
		TimeoutAt:          timePtrFromProto(pb.GetTimeoutAt()),
		ClaimedAt:          timePtrFromProto(pb.GetClaimedAt()),
		RetryCount:         int(pb.GetRetryCount()),
		CreatedAt:          timeFromProto(pb.GetCreatedAt()),
		CompletedAt:        timePtrFromProto(pb.GetCompletedAt()),
	}
}

func completeTaskRequestToProto(req workflowengine.CompleteTaskRequest) (*workflowpb.CompleteTaskRequest, error) {
	variables, err := mapToStruct(req.Variables)
	if err != nil {
		return nil, err
	}
	return &workflowpb.CompleteTaskRequest{
		TaskId:        req.TaskID,
		Operator:      req.Operator,
		Action:        req.Action,
		Comment:       req.Comment,
		Variables:     variables,
		CallbackToken: req.CallbackToken,
	}, nil
}

func completeTaskRequestFromProto(pb *workflowpb.CompleteTaskRequest) workflowengine.CompleteTaskRequest {
	if pb == nil {
		return workflowengine.CompleteTaskRequest{}
	}
	return workflowengine.CompleteTaskRequest{
		TaskID:        pb.GetTaskId(),
		Operator:      pb.GetOperator(),
		Action:        pb.GetAction(),
		Comment:       pb.GetComment(),
		Variables:     structToMap(pb.GetVariables()),
		CallbackToken: pb.GetCallbackToken(),
	}
}

func failTaskRequestToProto(req workflowengine.FailTaskRequest) (*workflowpb.FailTaskRequest, error) {
	variables, err := mapToStruct(req.Variables)
	if err != nil {
		return nil, err
	}
	return &workflowpb.FailTaskRequest{
		TaskId:        req.TaskID,
		Operator:      req.Operator,
		ErrorCode:     req.ErrorCode,
		Message:       req.Message,
		Retryable:     req.Retryable,
		Variables:     variables,
		CallbackToken: req.CallbackToken,
		WorkerTaskId:  req.WorkerTaskID,
	}, nil
}

func failTaskRequestFromProto(pb *workflowpb.FailTaskRequest) workflowengine.FailTaskRequest {
	if pb == nil {
		return workflowengine.FailTaskRequest{}
	}
	return workflowengine.FailTaskRequest{
		TaskID:        pb.GetTaskId(),
		Operator:      pb.GetOperator(),
		ErrorCode:     pb.GetErrorCode(),
		Message:       pb.GetMessage(),
		Retryable:     pb.GetRetryable(),
		Variables:     structToMap(pb.GetVariables()),
		CallbackToken: pb.GetCallbackToken(),
		WorkerTaskID:  pb.GetWorkerTaskId(),
	}
}

func retryTaskRequestToProto(req workflowengine.RetryTaskRequest) *workflowpb.RetryTaskRequest {
	return &workflowpb.RetryTaskRequest{
		TaskId:   req.TaskID,
		Operator: req.Operator,
		Reason:   req.Reason,
	}
}

func retryTaskRequestFromProto(pb *workflowpb.RetryTaskRequest) workflowengine.RetryTaskRequest {
	if pb == nil {
		return workflowengine.RetryTaskRequest{}
	}
	return workflowengine.RetryTaskRequest{
		TaskID:   pb.GetTaskId(),
		Operator: pb.GetOperator(),
		Reason:   pb.GetReason(),
	}
}

func taskOperationRequestToProto(req workflowengine.TaskOperationRequest) (*workflowpb.TaskOperationRequest, error) {
	variables, err := mapToStruct(req.Variables)
	if err != nil {
		return nil, err
	}
	return &workflowpb.TaskOperationRequest{
		TaskId:         req.TaskID,
		Operator:       req.Operator,
		TargetAssignee: req.TargetAssignee,
		Action:         req.Action,
		Comment:        req.Comment,
		Variables:      variables,
	}, nil
}

func taskOperationRequestFromProto(pb *workflowpb.TaskOperationRequest) workflowengine.TaskOperationRequest {
	if pb == nil {
		return workflowengine.TaskOperationRequest{}
	}
	return workflowengine.TaskOperationRequest{
		TaskID:         pb.GetTaskId(),
		Operator:       pb.GetOperator(),
		TargetAssignee: pb.GetTargetAssignee(),
		Action:         pb.GetAction(),
		Comment:        pb.GetComment(),
		Variables:      structToMap(pb.GetVariables()),
	}
}

func lifecycleRequestToProto(req workflowengine.InstanceLifecycleRequest) *workflowpb.InstanceLifecycleRequest {
	return &workflowpb.InstanceLifecycleRequest{
		InstanceId: req.InstanceID,
		Operator:   req.Operator,
		Reason:     req.Reason,
	}
}

func lifecycleRequestFromProto(pb *workflowpb.InstanceLifecycleRequest) workflowengine.InstanceLifecycleRequest {
	if pb == nil {
		return workflowengine.InstanceLifecycleRequest{}
	}
	return workflowengine.InstanceLifecycleRequest{
		InstanceID: pb.GetInstanceId(),
		Operator:   pb.GetOperator(),
		Reason:     pb.GetReason(),
	}
}

func scanTimeoutRequestToProto(req workflowengine.ScanTimeoutTasksRequest) *workflowpb.ScanTimeoutTasksRequest {
	return &workflowpb.ScanTimeoutTasksRequest{Now: timeToProto(req.Now), Limit: int32(req.Limit)}
}

func scanTimeoutRequestFromProto(pb *workflowpb.ScanTimeoutTasksRequest) workflowengine.ScanTimeoutTasksRequest {
	if pb == nil {
		return workflowengine.ScanTimeoutTasksRequest{}
	}
	return workflowengine.ScanTimeoutTasksRequest{
		Now:   timeFromProto(pb.GetNow()),
		Limit: int(pb.GetLimit()),
	}
}

func scanTimeoutResultToProto(result workflowengine.ScanTimeoutTasksResult) (*workflowpb.ScanTimeoutTasksResult, error) {
	events := make([]*workflowpb.OutboxEvent, 0, len(result.Events))
	for _, event := range result.Events {
		pb, err := outboxEventToProto(event)
		if err != nil {
			return nil, err
		}
		events = append(events, pb)
	}
	return &workflowpb.ScanTimeoutTasksResult{
		Scanned:  int32(result.Scanned),
		TimedOut: int32(result.TimedOut),
		Events:   events,
	}, nil
}

func scanTimeoutResultFromProto(pb *workflowpb.ScanTimeoutTasksResult) (workflowengine.ScanTimeoutTasksResult, error) {
	if pb == nil {
		return workflowengine.ScanTimeoutTasksResult{}, nil
	}
	events := make([]domain.OutboxEvent, 0, len(pb.GetEvents()))
	for _, event := range pb.GetEvents() {
		events = append(events, outboxEventFromProto(event))
	}
	return workflowengine.ScanTimeoutTasksResult{
		Scanned:  int(pb.GetScanned()),
		TimedOut: int(pb.GetTimedOut()),
		Events:   events,
	}, nil
}

func variableToProto(variable domain.Variable) (*workflowpb.Variable, error) {
	value, err := valueToProto(variable.Value)
	if err != nil {
		return nil, err
	}
	return &workflowpb.Variable{
		InstanceId:      variable.InstanceID,
		Name:            variable.Name,
		Type:            variable.Type,
		Value:           value,
		Scope:           string(variable.Scope),
		UpdatedByNodeId: variable.UpdatedByNodeID,
		UpdatedAt:       timeToProto(variable.UpdatedAt),
	}, nil
}

func variableFromProto(pb *workflowpb.Variable) domain.Variable {
	if pb == nil {
		return domain.Variable{}
	}
	return domain.Variable{
		InstanceID:      pb.GetInstanceId(),
		Name:            pb.GetName(),
		Type:            pb.GetType(),
		Value:           valueFromProto(pb.GetValue()),
		Scope:           domain.VariableScope(pb.GetScope()),
		UpdatedByNodeID: pb.GetUpdatedByNodeId(),
		UpdatedAt:       timeFromProto(pb.GetUpdatedAt()),
	}
}

func variablesResponseToProto(result map[string]domain.Variable) (*workflowpb.VariablesResponse, error) {
	variables := make(map[string]*workflowpb.Variable, len(result))
	for name, variable := range result {
		item, err := variableToProto(variable)
		if err != nil {
			return nil, err
		}
		variables[name] = item
	}
	return &workflowpb.VariablesResponse{Variables: variables}, nil
}

func variablesResponseFromProto(resp *workflowpb.VariablesResponse) map[string]domain.Variable {
	if resp == nil {
		return map[string]domain.Variable{}
	}
	variables := make(map[string]domain.Variable, len(resp.GetVariables()))
	for name, item := range resp.GetVariables() {
		variables[name] = variableFromProto(item)
	}
	return variables
}

func variableNamesRequestFromProto(pb *workflowpb.VariableNamesRequest) workflowengine.VariableNamesRequest {
	if pb == nil {
		return workflowengine.VariableNamesRequest{}
	}
	return workflowengine.VariableNamesRequest{
		InstanceID: pb.GetInstanceId(),
		Names:      append([]string(nil), pb.GetNames()...),
	}
}

func updateVariablesRequestToProto(req workflowengine.UpdateVariablesRequest) (*workflowpb.UpdateVariablesRequest, error) {
	variables, err := mapToStruct(req.Variables)
	if err != nil {
		return nil, err
	}
	return &workflowpb.UpdateVariablesRequest{
		InstanceId: req.InstanceID,
		Variables:  variables,
		Operator:   req.Operator,
	}, nil
}

func updateVariablesRequestFromProto(pb *workflowpb.UpdateVariablesRequest) workflowengine.UpdateVariablesRequest {
	if pb == nil {
		return workflowengine.UpdateVariablesRequest{}
	}
	return workflowengine.UpdateVariablesRequest{
		InstanceID: pb.GetInstanceId(),
		Variables:  structToMap(pb.GetVariables()),
		Operator:   pb.GetOperator(),
	}
}

func systemVariablesToProto(system workflowengine.InstanceSystemVariables) *workflowpb.InstanceSystemVariables {
	return &workflowpb.InstanceSystemVariables{
		InstanceId:        system.InstanceID,
		DefinitionId:      system.DefinitionID,
		DefinitionKey:     system.DefinitionKey,
		DefinitionName:    system.DefinitionName,
		DefinitionVersion: int32(system.DefinitionVersion),
		InstanceName:      system.InstanceName,
		TenantId:          system.TenantID,
		Starter:           system.Starter,
		StartTime:         timeToProto(system.StartTime),
	}
}

func systemVariablesFromProto(pb *workflowpb.InstanceSystemVariables) workflowengine.InstanceSystemVariables {
	if pb == nil {
		return workflowengine.InstanceSystemVariables{}
	}
	return workflowengine.InstanceSystemVariables{
		InstanceID:        pb.GetInstanceId(),
		DefinitionID:      pb.GetDefinitionId(),
		DefinitionKey:     pb.GetDefinitionKey(),
		DefinitionName:    pb.GetDefinitionName(),
		DefinitionVersion: int(pb.GetDefinitionVersion()),
		InstanceName:      pb.GetInstanceName(),
		TenantID:          pb.GetTenantId(),
		Starter:           pb.GetStarter(),
		StartTime:         timeFromProto(pb.GetStartTime()),
	}
}

func auditEventToProto(event domain.AuditEvent) (*workflowpb.AuditEvent, error) {
	payload, err := mapToStruct(event.Payload)
	if err != nil {
		return nil, err
	}
	return &workflowpb.AuditEvent{
		Id:         event.ID,
		InstanceId: event.InstanceID,
		EventType:  string(event.EventType),
		NodeId:     event.NodeID,
		TaskId:     event.TaskID,
		Payload:    payload,
		CreatedAt:  timeToProto(event.CreatedAt),
	}, nil
}

func auditEventFromProto(pb *workflowpb.AuditEvent) domain.AuditEvent {
	if pb == nil {
		return domain.AuditEvent{}
	}
	return domain.AuditEvent{
		ID:         pb.GetId(),
		InstanceID: pb.GetInstanceId(),
		EventType:  domain.AuditEventType(pb.GetEventType()),
		NodeID:     pb.GetNodeId(),
		TaskID:     pb.GetTaskId(),
		Payload:    structToMap(pb.GetPayload()),
		CreatedAt:  timeFromProto(pb.GetCreatedAt()),
	}
}

func outboxEventToProto(event domain.OutboxEvent) (*workflowpb.OutboxEvent, error) {
	payload, err := mapToStruct(event.Payload)
	if err != nil {
		return nil, err
	}
	return &workflowpb.OutboxEvent{
		Id:          event.ID,
		EventType:   event.EventType,
		AggregateId: event.AggregateID,
		Payload:     payload,
		Status:      string(event.Status),
		RetryCount:  int32(event.RetryCount),
		NextRetryAt: timePtrToProto(event.NextRetryAt),
		CreatedAt:   timeToProto(event.CreatedAt),
	}, nil
}

func outboxEventFromProto(pb *workflowpb.OutboxEvent) domain.OutboxEvent {
	if pb == nil {
		return domain.OutboxEvent{}
	}
	return domain.OutboxEvent{
		ID:          pb.GetId(),
		EventType:   pb.GetEventType(),
		AggregateID: pb.GetAggregateId(),
		Payload:     structToMap(pb.GetPayload()),
		Status:      domain.OutboxStatus(pb.GetStatus()),
		RetryCount:  int(pb.GetRetryCount()),
		NextRetryAt: timePtrFromProto(pb.GetNextRetryAt()),
		CreatedAt:   timeFromProto(pb.GetCreatedAt()),
	}
}

func automationServiceToProto(service automation.Service) (*workflowpb.AutomationService, error) {
	tasks := make([]*workflowpb.AutomationTask, 0, len(service.Tasks))
	for _, task := range service.Tasks {
		pb, err := automationTaskToProto(task)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, pb)
	}
	return &workflowpb.AutomationService{
		Id:           service.ID,
		Name:         service.Name,
		BaseUrl:      service.BaseURL,
		HealthUrl:    service.HealthURL,
		Protocol:     service.Protocol,
		Version:      service.Version,
		Tags:         append([]string(nil), service.Tags...),
		Tasks:        tasks,
		RegisteredAt: timeToProto(service.RegisteredAt),
		UpdatedAt:    timeToProto(service.UpdatedAt),
		TtlSeconds:   int32(service.TTLSeconds),
	}, nil
}

func automationServiceFromProto(pb *workflowpb.AutomationService) (automation.Service, error) {
	if pb == nil {
		return automation.Service{}, nil
	}
	tasks := make([]automation.Task, 0, len(pb.GetTasks()))
	for _, task := range pb.GetTasks() {
		tasks = append(tasks, automationTaskFromProto(task))
	}
	return automation.Service{
		ID:           pb.GetId(),
		Name:         pb.GetName(),
		BaseURL:      pb.GetBaseUrl(),
		HealthURL:    pb.GetHealthUrl(),
		Protocol:     pb.GetProtocol(),
		Version:      pb.GetVersion(),
		Tags:         append([]string(nil), pb.GetTags()...),
		Tasks:        tasks,
		RegisteredAt: timeFromProto(pb.GetRegisteredAt()),
		UpdatedAt:    timeFromProto(pb.GetUpdatedAt()),
		TTLSeconds:   int(pb.GetTtlSeconds()),
	}, nil
}

func automationTaskToProto(task automation.Task) (*workflowpb.AutomationTask, error) {
	inputs, err := automationParametersToProto(task.InputSchema)
	if err != nil {
		return nil, err
	}
	outputs, err := automationParametersToProto(task.OutputSchema)
	if err != nil {
		return nil, err
	}
	return &workflowpb.AutomationTask{
		ServiceId:    task.ServiceID,
		Key:          task.Key,
		Name:         task.Name,
		Description:  task.Description,
		Method:       task.Method,
		Endpoint:     task.Endpoint,
		InputSchema:  inputs,
		OutputSchema: outputs,
	}, nil
}

func automationTaskFromProto(pb *workflowpb.AutomationTask) automation.Task {
	if pb == nil {
		return automation.Task{}
	}
	return automation.Task{
		ServiceID:    pb.GetServiceId(),
		Key:          pb.GetKey(),
		Name:         pb.GetName(),
		Description:  pb.GetDescription(),
		Method:       pb.GetMethod(),
		Endpoint:     pb.GetEndpoint(),
		InputSchema:  automationParametersFromProto(pb.GetInputSchema()),
		OutputSchema: automationParametersFromProto(pb.GetOutputSchema()),
	}
}

func automationParametersToProto(parameters []automation.Parameter) ([]*workflowpb.AutomationParameter, error) {
	result := make([]*workflowpb.AutomationParameter, 0, len(parameters))
	for _, parameter := range parameters {
		value, err := valueToProto(parameter.DefaultValue)
		if err != nil {
			return nil, err
		}
		result = append(result, &workflowpb.AutomationParameter{
			Key:          parameter.Key,
			Name:         parameter.Name,
			Type:         parameter.Type,
			Required:     parameter.Required,
			DefaultValue: value,
			Description:  parameter.Description,
		})
	}
	return result, nil
}

func automationParametersFromProto(parameters []*workflowpb.AutomationParameter) []automation.Parameter {
	result := make([]automation.Parameter, 0, len(parameters))
	for _, parameter := range parameters {
		result = append(result, automation.Parameter{
			Key:          parameter.GetKey(),
			Name:         parameter.GetName(),
			Type:         parameter.GetType(),
			Required:     parameter.GetRequired(),
			DefaultValue: valueFromProto(parameter.GetDefaultValue()),
			Description:  parameter.GetDescription(),
		})
	}
	return result
}

func dispatchRequestToProto(req automation.DispatchRequest) (*workflowpb.DispatchTaskRequest, error) {
	variables, err := mapToStruct(req.Variables)
	if err != nil {
		return nil, err
	}
	return &workflowpb.DispatchTaskRequest{
		TaskId:            req.TaskID,
		ProcessInstanceId: req.ProcessInstanceID,
		NodeId:            req.NodeID,
		AutomationTaskKey: req.AutomationTaskKey,
		Variables:         variables,
		CallbackUrl:       req.CallbackURL,
		CallbackToken:     req.CallbackToken,
		FailCallbackUrl:   req.FailCallbackURL,
		DispatchMode:      req.DispatchMode,
	}, nil
}

func dispatchRequestFromProto(pb *workflowpb.DispatchTaskRequest) automation.DispatchRequest {
	if pb == nil {
		return automation.DispatchRequest{}
	}
	return automation.DispatchRequest{
		TaskID:            pb.GetTaskId(),
		ProcessInstanceID: pb.GetProcessInstanceId(),
		NodeID:            pb.GetNodeId(),
		AutomationTaskKey: pb.GetAutomationTaskKey(),
		Variables:         structToMap(pb.GetVariables()),
		CallbackURL:       pb.GetCallbackUrl(),
		CallbackToken:     pb.GetCallbackToken(),
		FailCallbackURL:   pb.GetFailCallbackUrl(),
		DispatchMode:      pb.GetDispatchMode(),
	}
}

func dispatchResultToProto(result automation.DispatchResult) (*workflowpb.DispatchTaskResponse, error) {
	variables, err := mapToStruct(result.Variables)
	if err != nil {
		return nil, err
	}
	return &workflowpb.DispatchTaskResponse{
		TaskId:            result.TaskID,
		ProcessInstanceId: result.ProcessInstanceID,
		NodeId:            result.NodeID,
		Completed:         result.Completed,
		Variables:         variables,
	}, nil
}

func dispatchResultFromProto(pb *workflowpb.DispatchTaskResponse) automation.DispatchResult {
	if pb == nil {
		return automation.DispatchResult{}
	}
	return automation.DispatchResult{
		TaskID:            pb.GetTaskId(),
		ProcessInstanceID: pb.GetProcessInstanceId(),
		NodeID:            pb.GetNodeId(),
		Completed:         pb.GetCompleted(),
		Variables:         structToMap(pb.GetVariables()),
	}
}

func mapToStruct(value map[string]any) (*structpb.Struct, error) {
	if len(value) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(value)
}

func structToMap(value *structpb.Struct) map[string]any {
	if value == nil {
		return nil
	}
	return value.AsMap()
}

func valueToProto(value any) (*structpb.Value, error) {
	if value == nil {
		return nil, nil
	}
	return structpb.NewValue(value)
}

func valueFromProto(value *structpb.Value) any {
	if value == nil {
		return nil
	}
	return value.AsInterface()
}

func timeToProto(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func timeFromProto(value *timestamppb.Timestamp) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.AsTime()
}

func timePtrToProto(value *time.Time) *timestamppb.Timestamp {
	if value == nil || value.IsZero() {
		return nil
	}
	return timestamppb.New(*value)
}

func timePtrFromProto(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	t := value.AsTime()
	return &t
}
