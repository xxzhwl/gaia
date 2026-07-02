package gormstore

import (
	"encoding/json"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

func processDefinitionToModel(def domain.ProcessDefinition) (ProcessDefinitionModel, error) {
	modelJSON, err := json.Marshal(def.Model)
	if err != nil {
		return ProcessDefinitionModel{}, err
	}
	inputsJSON, err := json.Marshal(def.Inputs)
	if err != nil {
		return ProcessDefinitionModel{}, err
	}
	return ProcessDefinitionModel{
		ID:          def.ID,
		Key:         def.Key,
		Name:        def.Name,
		Version:     def.Version,
		Status:      string(def.Status),
		ModelJSON:   modelJSON,
		InputsJSON:  inputsJSON,
		CreatedBy:   def.CreatedBy,
		CreatedAt:   def.CreatedAt,
		DeployedAt:  def.DeployedAt,
		EditVersion: def.EditVersion,
	}, nil
}

func processDefinitionFromModel(model ProcessDefinitionModel) (domain.ProcessDefinition, error) {
	var workflowModel domain.WorkflowModel
	if err := json.Unmarshal(model.ModelJSON, &workflowModel); err != nil {
		return domain.ProcessDefinition{}, err
	}
	var inputs []domain.InputParameter
	if len(model.InputsJSON) > 0 {
		if err := json.Unmarshal(model.InputsJSON, &inputs); err != nil {
			return domain.ProcessDefinition{}, err
		}
	}
	return domain.ProcessDefinition{
		ID:          model.ID,
		Key:         model.Key,
		Name:        model.Name,
		Version:     model.Version,
		Status:      domain.DefinitionStatus(model.Status),
		Model:       workflowModel,
		Inputs:      inputs,
		CreatedBy:   model.CreatedBy,
		CreatedAt:   model.CreatedAt,
		DeployedAt:  model.DeployedAt,
		EditVersion: model.EditVersion,
	}, nil
}

func processInstanceToModel(instance domain.ProcessInstance) *ProcessInstanceModel {
	return &ProcessInstanceModel{
		ID:                instance.ID,
		DefinitionID:      instance.DefinitionID,
		DefinitionKey:     instance.DefinitionKey,
		DefinitionVersion: instance.DefinitionVersion,
		BusinessKey:       instance.BusinessKey,
		TenantID:          instance.TenantID,
		Status:            string(instance.Status),
		StartTime:         instance.StartTime,
		EndTime:           instance.EndTime,
		Starter:           instance.Starter,
		EndNodeID:         instance.EndNodeID,
		FailReason:        instance.FailReason,
		Version:           instance.Version,
	}
}

func processInstanceFromModel(model ProcessInstanceModel) domain.ProcessInstance {
	return domain.ProcessInstance{
		ID:                model.ID,
		DefinitionID:      model.DefinitionID,
		DefinitionKey:     model.DefinitionKey,
		DefinitionVersion: model.DefinitionVersion,
		BusinessKey:       model.BusinessKey,
		TenantID:          model.TenantID,
		Status:            domain.InstanceStatus(model.Status),
		StartTime:         model.StartTime,
		EndTime:           model.EndTime,
		Starter:           model.Starter,
		EndNodeID:         model.EndNodeID,
		FailReason:        model.FailReason,
		Version:           model.Version,
	}
}

func executionToModel(execution domain.Execution) *ExecutionModel {
	return &ExecutionModel{
		ID:         execution.ID,
		InstanceID: execution.InstanceID,
		ParentID:   execution.ParentID,
		NodeID:     execution.NodeID,
		Status:     string(execution.Status),
		JoinKey:    execution.JoinKey,
		CreatedAt:  execution.CreatedAt,
		UpdatedAt:  execution.UpdatedAt,
	}
}

func executionFromModel(model ExecutionModel) domain.Execution {
	return domain.Execution{
		ID:         model.ID,
		InstanceID: model.InstanceID,
		ParentID:   model.ParentID,
		NodeID:     model.NodeID,
		Status:     domain.ExecutionStatus(model.Status),
		JoinKey:    model.JoinKey,
		CreatedAt:  model.CreatedAt,
		UpdatedAt:  model.UpdatedAt,
	}
}

func activityToModel(activity domain.ActivityInstance) *ActivityInstanceModel {
	return &ActivityInstanceModel{
		ID:           activity.ID,
		InstanceID:   activity.InstanceID,
		ExecutionID:  activity.ExecutionID,
		NodeID:       activity.NodeID,
		NodeType:     string(activity.NodeType),
		NodeName:     activity.NodeName,
		Status:       string(activity.Status),
		StartTime:    activity.StartTime,
		EndTime:      activity.EndTime,
		RetryCount:   activity.RetryCount,
		ErrorMessage: activity.ErrorMessage,
	}
}

func activityFromModel(model ActivityInstanceModel) domain.ActivityInstance {
	return domain.ActivityInstance{
		ID:           model.ID,
		InstanceID:   model.InstanceID,
		ExecutionID:  model.ExecutionID,
		NodeID:       model.NodeID,
		NodeType:     domain.NodeType(model.NodeType),
		NodeName:     model.NodeName,
		Status:       domain.ActivityStatus(model.Status),
		StartTime:    model.StartTime,
		EndTime:      model.EndTime,
		RetryCount:   model.RetryCount,
		ErrorMessage: model.ErrorMessage,
	}
}

func taskToModel(task domain.Task) (TaskModel, error) {
	snapshotJSON, err := json.Marshal(task.VariableSnapshot)
	if err != nil {
		return TaskModel{}, err
	}
	return TaskModel{
		ID:                   task.ID,
		InstanceID:           task.InstanceID,
		ActivityInstanceID:   task.ActivityInstanceID,
		NodeID:               task.NodeID,
		Title:                task.Title,
		Type:                 string(task.Type),
		Status:               string(task.Status),
		Assignee:             task.Assignee,
		Owner:                task.Owner,
		VariableSnapshotJSON: snapshotJSON,
		Action:               task.Action,
		Comment:              task.Comment,
		CompletedBy:          task.CompletedBy,
		DelegatedFrom:        task.DelegatedFrom,
		DispatchURL:          task.DispatchURL,
		CallbackToken:        task.CallbackToken,
		TimeoutAt:            task.TimeoutAt,
		ClaimedAt:            task.ClaimedAt,
		RetryCount:           task.RetryCount,
		CreatedAt:            task.CreatedAt,
		CompletedAt:          task.CompletedAt,
	}, nil
}

func taskFromModel(model TaskModel) (domain.Task, error) {
	var snapshot map[string]any
	if len(model.VariableSnapshotJSON) > 0 {
		if err := json.Unmarshal(model.VariableSnapshotJSON, &snapshot); err != nil {
			return domain.Task{}, err
		}
	}
	return domain.Task{
		ID:                 model.ID,
		InstanceID:         model.InstanceID,
		ActivityInstanceID: model.ActivityInstanceID,
		NodeID:             model.NodeID,
		Title:              model.Title,
		Type:               domain.TaskType(model.Type),
		Status:             domain.TaskStatus(model.Status),
		Assignee:           model.Assignee,
		Owner:              model.Owner,
		VariableSnapshot:   snapshot,
		Action:             model.Action,
		Comment:            model.Comment,
		CompletedBy:        model.CompletedBy,
		DelegatedFrom:      model.DelegatedFrom,
		DispatchURL:        model.DispatchURL,
		CallbackToken:      model.CallbackToken,
		TimeoutAt:          model.TimeoutAt,
		ClaimedAt:          model.ClaimedAt,
		RetryCount:         model.RetryCount,
		CreatedAt:          model.CreatedAt,
		CompletedAt:        model.CompletedAt,
	}, nil
}

func asyncTaskBindingToModel(binding domain.AsyncTaskBinding) AsyncTaskBindingModel {
	return AsyncTaskBindingModel{
		ID:                  binding.ID,
		AsyncTaskID:         binding.AsyncTaskID,
		WorkflowTaskID:      binding.WorkflowTaskID,
		ProcessInstanceID:   binding.ProcessInstanceID,
		DefinitionID:        binding.DefinitionID,
		DefinitionKey:       binding.DefinitionKey,
		DefinitionName:      binding.DefinitionName,
		DefinitionVersion:   binding.DefinitionVersion,
		InstanceName:        binding.InstanceName,
		NodeID:              binding.NodeID,
		NodeName:            binding.NodeName,
		AutomationTaskKey:   binding.AutomationTaskKey,
		Theme:               binding.Theme,
		ServiceName:         binding.ServiceName,
		MethodName:          binding.MethodName,
		CallbackToken:       binding.CallbackToken,
		CompleteCallbackURL: binding.CompleteCallbackURL,
		FailCallbackURL:     binding.FailCallbackURL,
		Status:              string(binding.Status),
		CallbackStatus:      string(binding.CallbackStatus),
		LastError:           binding.LastError,
		CreatedAt:           binding.CreatedAt,
		UpdatedAt:           binding.UpdatedAt,
	}
}

func asyncTaskBindingFromModel(model AsyncTaskBindingModel) domain.AsyncTaskBinding {
	return domain.AsyncTaskBinding{
		ID:                  model.ID,
		AsyncTaskID:         model.AsyncTaskID,
		WorkflowTaskID:      model.WorkflowTaskID,
		ProcessInstanceID:   model.ProcessInstanceID,
		DefinitionID:        model.DefinitionID,
		DefinitionKey:       model.DefinitionKey,
		DefinitionName:      model.DefinitionName,
		DefinitionVersion:   model.DefinitionVersion,
		InstanceName:        model.InstanceName,
		NodeID:              model.NodeID,
		NodeName:            model.NodeName,
		AutomationTaskKey:   model.AutomationTaskKey,
		Theme:               model.Theme,
		ServiceName:         model.ServiceName,
		MethodName:          model.MethodName,
		CallbackToken:       model.CallbackToken,
		CompleteCallbackURL: model.CompleteCallbackURL,
		FailCallbackURL:     model.FailCallbackURL,
		Status:              domain.AsyncTaskBindingStatus(model.Status),
		CallbackStatus:      domain.AsyncTaskCallbackStatus(model.CallbackStatus),
		LastError:           model.LastError,
		CreatedAt:           model.CreatedAt,
		UpdatedAt:           model.UpdatedAt,
	}
}

func variableCurrentToModel(variable domain.Variable) (VariableCurrentModel, error) {
	valueJSON, err := json.Marshal(variable.Value)
	if err != nil {
		return VariableCurrentModel{}, err
	}
	return VariableCurrentModel{
		InstanceID:      variable.InstanceID,
		Name:            variable.Name,
		Type:            variable.Type,
		ValueJSON:       valueJSON,
		Scope:           string(variable.Scope),
		UpdatedByNodeID: variable.UpdatedByNodeID,
		UpdatedAt:       variable.UpdatedAt,
	}, nil
}

func variableCurrentFromModel(model VariableCurrentModel) (domain.Variable, error) {
	value, err := decodeAny(model.ValueJSON)
	if err != nil {
		return domain.Variable{}, err
	}
	return domain.Variable{
		InstanceID:      model.InstanceID,
		Name:            model.Name,
		Type:            model.Type,
		Value:           value,
		Scope:           domain.VariableScope(model.Scope),
		UpdatedByNodeID: model.UpdatedByNodeID,
		UpdatedAt:       model.UpdatedAt,
	}, nil
}

func variableHistoryToModel(history domain.VariableHistory) (VariableHistoryModel, error) {
	oldValueJSON, err := json.Marshal(history.OldValue)
	if err != nil {
		return VariableHistoryModel{}, err
	}
	newValueJSON, err := json.Marshal(history.NewValue)
	if err != nil {
		return VariableHistoryModel{}, err
	}
	return VariableHistoryModel{
		ID:           history.ID,
		InstanceID:   history.InstanceID,
		Name:         history.Name,
		OldValueJSON: oldValueJSON,
		NewValueJSON: newValueJSON,
		SourceNodeID: history.SourceNodeID,
		SourceTaskID: history.SourceTaskID,
		CreatedAt:    history.CreatedAt,
	}, nil
}

func outboxToModel(event domain.OutboxEvent) (OutboxEventModel, error) {
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return OutboxEventModel{}, err
	}
	return OutboxEventModel{
		ID:          event.ID,
		EventType:   event.EventType,
		AggregateID: event.AggregateID,
		PayloadJSON: payloadJSON,
		Status:      string(event.Status),
		RetryCount:  event.RetryCount,
		NextRetryAt: event.NextRetryAt,
		CreatedAt:   event.CreatedAt,
	}, nil
}

func outboxFromModel(model OutboxEventModel) (domain.OutboxEvent, error) {
	payload := map[string]any{}
	if len(model.PayloadJSON) > 0 {
		if err := json.Unmarshal(model.PayloadJSON, &payload); err != nil {
			return domain.OutboxEvent{}, err
		}
	}
	return domain.OutboxEvent{
		ID:          model.ID,
		EventType:   model.EventType,
		AggregateID: model.AggregateID,
		Payload:     payload,
		Status:      domain.OutboxStatus(model.Status),
		RetryCount:  model.RetryCount,
		NextRetryAt: model.NextRetryAt,
		CreatedAt:   model.CreatedAt,
	}, nil
}

func auditToModel(event domain.AuditEvent) (AuditEventModel, error) {
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return AuditEventModel{}, err
	}
	return AuditEventModel{
		ID:          event.ID,
		InstanceID:  event.InstanceID,
		EventType:   string(event.EventType),
		NodeID:      event.NodeID,
		TaskID:      event.TaskID,
		PayloadJSON: payloadJSON,
		CreatedAt:   event.CreatedAt,
	}, nil
}

func auditFromModel(model AuditEventModel) (domain.AuditEvent, error) {
	payload := map[string]any{}
	if len(model.PayloadJSON) > 0 {
		if err := json.Unmarshal(model.PayloadJSON, &payload); err != nil {
			return domain.AuditEvent{}, err
		}
	}
	return domain.AuditEvent{
		ID:         model.ID,
		InstanceID: model.InstanceID,
		EventType:  domain.AuditEventType(model.EventType),
		NodeID:     model.NodeID,
		TaskID:     model.TaskID,
		Payload:    payload,
		CreatedAt:  model.CreatedAt,
	}, nil
}

func decodeAny(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}
