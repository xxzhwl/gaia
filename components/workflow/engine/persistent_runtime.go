package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/persistence"
)

// PersistentRuntime 是基于仓储接口的持久化流程运行时。
type PersistentRuntime struct {
	uow   persistence.UnitOfWork
	clock domain.Clock
	ids   IDGenerator
}

// NewPersistentRuntime 创建持久化流程运行时。
func NewPersistentRuntime(uow persistence.UnitOfWork, clock domain.Clock, ids IDGenerator) *PersistentRuntime {
	if clock == nil {
		clock = domain.SystemClock{}
	}
	if ids == nil {
		ids = RandomIDGenerator{}
	}
	return &PersistentRuntime{uow: uow, clock: clock, ids: ids}
}

// CreateDefinition 创建草稿流程定义。
func (r *PersistentRuntime) CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	if err := validateDefinitionMetadata(def); err != nil {
		return domain.ProcessDefinition{}, err
	}
	var saved domain.ProcessDefinition
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		version, err := repos.Definitions().NextVersion(ctx, def.Key)
		if err != nil {
			return err
		}
		now := r.clock.Now()
		def.ID = r.ids.Next("def")
		def.Version = version
		def.Status = domain.DefinitionStatusDraft
		def.Model = normalizeWorkflowModel(def.Model)
		def.CreatedAt = now
		def.DeployedAt = nil
		if err := repos.Definitions().Save(ctx, def); err != nil {
			return err
		}
		saved = def
		return nil
	})
	return saved, err
}

// UpdateDefinition 更新已有流程定义内容。
func (r *PersistentRuntime) UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	var saved domain.ProcessDefinition
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		existing, err := repos.Definitions().Get(ctx, definitionID)
		if err != nil {
			return err
		}
		if strings.TrimSpace(def.Name) == "" {
			return fmt.Errorf("definition name is required")
		}
		existing.Name = strings.TrimSpace(def.Name)
		existing.Model = normalizeWorkflowModel(def.Model)
		existing.Inputs = def.Inputs
		if err := repos.Definitions().Update(ctx, existing); err != nil {
			return err
		}
		saved = existing
		return nil
	})
	return saved, err
}

// DeployDefinition 部署流程定义，并始终生成新的可运行版本。
func (r *PersistentRuntime) DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	if def.ID != "" {
		existing, err := r.uow.View(ctx).Definitions().Get(ctx, def.ID)
		if err != nil {
			return domain.ProcessDefinition{}, err
		}
		def.Key = existing.Key
		def.CreatedBy = existing.CreatedBy
		if strings.TrimSpace(def.Name) == "" {
			def.Name = existing.Name
		}
	}
	if err := domain.ValidateDefinition(def); err != nil {
		return domain.ProcessDefinition{}, err
	}
	var saved domain.ProcessDefinition
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		version, err := repos.Definitions().NextVersion(ctx, def.Key)
		if err != nil {
			return err
		}
		now := r.clock.Now()
		def.ID = r.ids.Next("def")
		def.Version = version
		def.Status = domain.DefinitionStatusDeployed
		def.CreatedAt = now
		def.DeployedAt = &now
		if err := repos.Definitions().Save(ctx, def); err != nil {
			return err
		}
		saved = def
		return nil
	})
	return saved, err
}

// GetDefinition 获取流程定义。
func (r *PersistentRuntime) GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error) {
	return r.uow.View(ctx).Definitions().Get(ctx, definitionID)
}

// StartProcess 启动流程实例并在事务中推进开始节点。
func (r *PersistentRuntime) StartProcess(ctx context.Context, req StartProcessRequest) (domain.ProcessInstance, error) {
	var result domain.ProcessInstance
	var workflowErr error
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		def, err := r.findDefinition(ctx, repos, req.DefinitionKey, req.DefinitionVersion)
		if err != nil {
			return err
		}
		req.Variables = variablesWithDefaults(def.Inputs, req.Variables)
		if err := validateInputs(def.Inputs, req.Variables); err != nil {
			return err
		}
		startNode, ok := def.Model.StartNode()
		if !ok {
			return fmt.Errorf("definition %s has no start event", def.ID)
		}

		now := r.clock.Now()
		instance := domain.ProcessInstance{
			ID:                r.ids.Next("pi"),
			DefinitionID:      def.ID,
			DefinitionKey:     def.Key,
			DefinitionVersion: def.Version,
			BusinessKey:       req.BusinessKey,
			TenantID:          req.TenantID,
			Status:            domain.InstanceStatusRunning,
			StartTime:         now,
			Starter:           req.Starter,
			Version:           1,
		}
		if err := repos.Instances().Save(ctx, instance); err != nil {
			return err
		}
		if err := r.auditEvent(ctx, repos, instance.ID, domain.AuditInstanceStarted, "", "", map[string]any{
			"definitionKey":     def.Key,
			"definitionVersion": def.Version,
			"businessKey":       req.BusinessKey,
		}); err != nil {
			return err
		}
		if err := r.setVariable(ctx, repos, instance.ID, "sys.instanceId", instance.ID, domain.VariableScopeSystem, "", ""); err != nil {
			return err
		}
		if err := r.setVariable(ctx, repos, instance.ID, "sys.definitionKey", def.Key, domain.VariableScopeSystem, "", ""); err != nil {
			return err
		}
		if err := r.setVariable(ctx, repos, instance.ID, "sys.definitionVersion", def.Version, domain.VariableScopeSystem, "", ""); err != nil {
			return err
		}
		if err := r.setVariable(ctx, repos, instance.ID, "sys.starter", req.Starter, domain.VariableScopeSystem, "", ""); err != nil {
			return err
		}
		if err := r.setVariable(ctx, repos, instance.ID, "sys.status", string(instance.Status), domain.VariableScopeSystem, "", ""); err != nil {
			return err
		}
		for name, value := range req.Variables {
			if err := r.setVariable(ctx, repos, instance.ID, name, value, domain.VariableScopeBusiness, "", ""); err != nil {
				return err
			}
		}

		exec := domain.Execution{
			ID:         r.ids.Next("exe"),
			InstanceID: instance.ID,
			NodeID:     startNode.ID,
			Status:     domain.ExecutionStatusActive,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
			return err
		}
		if err := r.enterNode(ctx, repos, def, instance.ID, exec.ID, startNode.ID); err != nil {
			if failErr := r.failInstance(ctx, repos, instance.ID, err); failErr != nil {
				return failErr
			}
			workflowErr = err
		}
		latest, err := repos.Instances().Get(ctx, instance.ID)
		if err != nil {
			return err
		}
		result = latest
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, workflowErr
}

// CompleteTask 完成任务并继续推进流程。
func (r *PersistentRuntime) CompleteTask(ctx context.Context, req CompleteTaskRequest) (domain.ProcessInstance, error) {
	var result domain.ProcessInstance
	var workflowErr error
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		task, err := repos.Tasks().Get(ctx, req.TaskID)
		if err != nil {
			return err
		}
		if task.Status == domain.TaskStatusCompleted {
			instance, err := repos.Instances().Get(ctx, task.InstanceID)
			if err != nil {
				return err
			}
			result = instance
			return nil
		}
		if task.Status != domain.TaskStatusWaiting && task.Status != domain.TaskStatusDispatched && task.Status != domain.TaskStatusCreated && task.Status != domain.TaskStatusClaimed {
			return fmt.Errorf("task %s status %s cannot be completed", task.ID, task.Status)
		}
		instance, err := repos.Instances().Get(ctx, task.InstanceID)
		if err != nil {
			return err
		}
		if instance.Status != domain.InstanceStatusRunning {
			return fmt.Errorf("process instance %s is not running", instance.ID)
		}

		now := r.clock.Now()
		task.Action = strings.TrimSpace(req.Action)
		task.Comment = strings.TrimSpace(req.Comment)
		task.CompletedBy = strings.TrimSpace(req.Operator)
		task.CompletedAt = &now
		completed, err := repos.Tasks().CompleteIfOpen(ctx, task, now)
		if err != nil {
			return err
		}
		if !completed {
			latestTask, err := repos.Tasks().Get(ctx, task.ID)
			if err != nil {
				return err
			}
			if latestTask.Status == domain.TaskStatusCompleted {
				instance, err := repos.Instances().Get(ctx, latestTask.InstanceID)
				if err != nil {
					return err
				}
				result = instance
				return nil
			}
			return fmt.Errorf("task %s status %s cannot be completed", latestTask.ID, latestTask.Status)
		}

		for name, value := range req.Variables {
			if err := r.setVariable(ctx, repos, task.InstanceID, name, value, domain.VariableScopeBusiness, task.NodeID, task.ID); err != nil {
				return err
			}
		}

		task.Status = domain.TaskStatusCompleted
		eventType := domain.AuditTaskCompleted
		if strings.EqualFold(task.Action, "reject") || strings.EqualFold(task.Action, "rejected") {
			eventType = domain.AuditTaskRejected
		}
		if err := r.auditEvent(ctx, repos, task.InstanceID, eventType, task.NodeID, task.ID, taskAuditPayload(task)); err != nil {
			return err
		}
		if err := repos.Outbox().Save(ctx, newTaskLifecycleOutbox(r.ids, r.clock.Now(), string(domain.OutboxEventTaskCompleted), task)); err != nil {
			return err
		}
		if err := repos.Outbox().Save(ctx, newNotificationOutbox(r.ids, r.clock.Now(), "task_completed", task)); err != nil {
			return err
		}

		activity, err := repos.Instances().GetActivity(ctx, task.ActivityInstanceID)
		if err != nil {
			return err
		}
		activity.Status = domain.ActivityStatusCompleted
		activity.EndTime = &now
		if err := repos.Instances().SaveActivity(ctx, activity); err != nil {
			return err
		}

		exec, err := repos.Instances().GetExecution(ctx, activity.ExecutionID)
		if err != nil {
			return err
		}
		exec.Status = domain.ExecutionStatusActive
		exec.UpdatedAt = now
		if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
			return err
		}

		def, err := repos.Definitions().FindDeployedVersion(ctx, instance.DefinitionKey, instance.DefinitionVersion)
		if err != nil {
			return err
		}
		if err := r.leaveNode(ctx, repos, def, task.InstanceID, exec.ID, task.NodeID); err != nil {
			if failErr := r.failInstance(ctx, repos, task.InstanceID, err); failErr != nil {
				return failErr
			}
			workflowErr = err
		}
		latest, err := repos.Instances().Get(ctx, task.InstanceID)
		if err != nil {
			return err
		}
		result = latest
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, workflowErr
}

// ClaimTask 认领人工待办。
func (r *PersistentRuntime) ClaimTask(ctx context.Context, req TaskOperationRequest) (domain.Task, error) {
	var result domain.Task
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		task, err := repos.Tasks().Get(ctx, req.TaskID)
		if err != nil {
			return err
		}
		if task.Type != domain.TaskTypeUser {
			return fmt.Errorf("task %s is not a user task", task.ID)
		}
		if task.Status == domain.TaskStatusCompleted || task.Status == domain.TaskStatusCanceled || task.Status == domain.TaskStatusRejected {
			return fmt.Errorf("task %s status %s cannot be claimed", task.ID, task.Status)
		}
		operator := strings.TrimSpace(req.Operator)
		if operator == "" {
			return fmt.Errorf("operator is required")
		}
		if task.Assignee != "" && task.Assignee != operator {
			return fmt.Errorf("task %s has been assigned to %s", task.ID, task.Assignee)
		}
		now := r.clock.Now()
		task.Assignee = operator
		task.Status = domain.TaskStatusClaimed
		task.ClaimedAt = &now
		if err := repos.Tasks().Update(ctx, task); err != nil {
			return err
		}
		if err := r.auditEvent(ctx, repos, task.InstanceID, domain.AuditTaskClaimed, task.NodeID, task.ID, taskAuditPayload(task)); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

// UnclaimTask 取消认领人工待办。
func (r *PersistentRuntime) UnclaimTask(ctx context.Context, req TaskOperationRequest) (domain.Task, error) {
	var result domain.Task
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		task, err := repos.Tasks().Get(ctx, req.TaskID)
		if err != nil {
			return err
		}
		if task.Type != domain.TaskTypeUser {
			return fmt.Errorf("task %s is not a user task", task.ID)
		}
		if task.Status != domain.TaskStatusClaimed {
			return fmt.Errorf("task %s status %s cannot be unclaimed", task.ID, task.Status)
		}
		task.Assignee = ""
		task.Status = domain.TaskStatusWaiting
		task.ClaimedAt = nil
		if err := repos.Tasks().Update(ctx, task); err != nil {
			return err
		}
		if err := r.auditEvent(ctx, repos, task.InstanceID, domain.AuditTaskUnclaimed, task.NodeID, task.ID, taskAuditPayload(task)); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

// TransferTask 转办人工待办。
func (r *PersistentRuntime) TransferTask(ctx context.Context, req TaskOperationRequest) (domain.Task, error) {
	return r.assignUserTask(ctx, req, domain.AuditTaskTransferred, false)
}

// DelegateTask 委托人工待办。
func (r *PersistentRuntime) DelegateTask(ctx context.Context, req TaskOperationRequest) (domain.Task, error) {
	return r.assignUserTask(ctx, req, domain.AuditTaskDelegated, true)
}

// RejectTask 以驳回动作完成人工待办。
func (r *PersistentRuntime) RejectTask(ctx context.Context, req TaskOperationRequest) (domain.ProcessInstance, error) {
	if strings.TrimSpace(req.Action) == "" {
		req.Action = "reject"
	}
	return r.CompleteTask(ctx, CompleteTaskRequest{
		TaskID:    req.TaskID,
		Operator:  req.Operator,
		Action:    req.Action,
		Comment:   req.Comment,
		Variables: req.Variables,
	})
}

// ScanTimeoutTasks 扫描已到期且仍打开的任务，并写入超时和通知 outbox。
func (r *PersistentRuntime) ScanTimeoutTasks(ctx context.Context, req ScanTimeoutTasksRequest) (ScanTimeoutTasksResult, error) {
	now := req.Now
	if now.IsZero() {
		now = r.clock.Now()
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	var result ScanTimeoutTasksResult
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		tasks, err := repos.Tasks().ListOpenDue(ctx, now, limit)
		if err != nil {
			return err
		}
		result.Scanned = len(tasks)
		for _, task := range tasks {
			exists, err := repos.Outbox().Exists(ctx, string(domain.OutboxEventTaskTimeout), task.ID)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			if err := r.auditEvent(ctx, repos, task.InstanceID, domain.AuditTaskTimedOut, task.NodeID, task.ID, taskAuditPayload(task)); err != nil {
				return err
			}
			timeoutEvent := newTaskLifecycleOutbox(r.ids, r.clock.Now(), string(domain.OutboxEventTaskTimeout), task)
			if err := repos.Outbox().Save(ctx, timeoutEvent); err != nil {
				return err
			}
			notificationEvent := newNotificationOutbox(r.ids, r.clock.Now(), "task_timeout", task)
			if err := repos.Outbox().Save(ctx, notificationEvent); err != nil {
				return err
			}
			result.TimedOut++
			result.Events = append(result.Events, timeoutEvent, notificationEvent)
		}
		return nil
	})
	return result, err
}

func (r *PersistentRuntime) assignUserTask(ctx context.Context, req TaskOperationRequest, eventType domain.AuditEventType, delegate bool) (domain.Task, error) {
	var result domain.Task
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		task, err := repos.Tasks().Get(ctx, req.TaskID)
		if err != nil {
			return err
		}
		if task.Type != domain.TaskTypeUser {
			return fmt.Errorf("task %s is not a user task", task.ID)
		}
		if task.Status == domain.TaskStatusCompleted || task.Status == domain.TaskStatusCanceled || task.Status == domain.TaskStatusRejected {
			return fmt.Errorf("task %s status %s cannot be assigned", task.ID, task.Status)
		}
		target := strings.TrimSpace(req.TargetAssignee)
		if target == "" {
			return fmt.Errorf("target assignee is required")
		}
		now := r.clock.Now()
		task.Owner = strings.TrimSpace(req.Operator)
		if delegate {
			task.DelegatedFrom = task.Assignee
		}
		task.Assignee = target
		task.Comment = strings.TrimSpace(req.Comment)
		task.Status = domain.TaskStatusClaimed
		task.ClaimedAt = &now
		if err := repos.Tasks().Update(ctx, task); err != nil {
			return err
		}
		if err := r.auditEvent(ctx, repos, task.InstanceID, eventType, task.NodeID, task.ID, taskAuditPayload(task)); err != nil {
			return err
		}
		result = task
		return nil
	})
	return result, err
}

// GetInstance 获取流程实例。
func (r *PersistentRuntime) GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error) {
	return r.uow.View(ctx).Instances().Get(ctx, instanceID)
}

// ListDefinitions 分页查询流程定义。
func (r *PersistentRuntime) ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error) {
	return r.uow.View(ctx).Definitions().List(ctx, filter)
}

// ListInstances 分页查询流程实例。
func (r *PersistentRuntime) ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error) {
	return r.uow.View(ctx).Instances().List(ctx, filter)
}

// ListTasks 分页查询任务。
func (r *PersistentRuntime) ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error) {
	return r.uow.View(ctx).Tasks().List(ctx, filter)
}

// Variables 查询流程实例当前变量。
func (r *PersistentRuntime) Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error) {
	return r.uow.View(ctx).Variables().CurrentByInstance(ctx, instanceID)
}

// Tasks 查询流程实例下的全部任务。
func (r *PersistentRuntime) Tasks(ctx context.Context, instanceID string) ([]domain.Task, error) {
	return r.uow.View(ctx).Tasks().ListByInstance(ctx, instanceID)
}

// AuditTrail 查询流程实例审计时间线。
func (r *PersistentRuntime) AuditTrail(ctx context.Context, instanceID string) ([]domain.AuditEvent, error) {
	return r.uow.View(ctx).Audit().ListByInstance(ctx, instanceID)
}

// OutboxEvents 查询当前 outbox 事件。
func (r *PersistentRuntime) OutboxEvents(ctx context.Context) ([]domain.OutboxEvent, error) {
	return r.uow.View(ctx).Outbox().List(ctx)
}

// ClaimOutboxBatch 领取一批可投递的 outbox 事件。
func (r *PersistentRuntime) ClaimOutboxBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	return r.claimOutboxBatchByType(ctx, string(domain.OutboxEventExternalTaskDispatch), limit)
}

// ClaimNotificationOutboxBatch 领取一批通知 outbox 事件。
func (r *PersistentRuntime) ClaimNotificationOutboxBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	return r.claimOutboxBatchByType(ctx, string(domain.OutboxEventNotificationRequested), limit)
}

func (r *PersistentRuntime) claimOutboxBatchByType(ctx context.Context, eventType string, limit int) ([]domain.OutboxEvent, error) {
	var events []domain.OutboxEvent
	err := r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		claimed, err := repos.Outbox().ClaimBatchByType(ctx, eventType, limit)
		if err != nil {
			return err
		}
		events = claimed
		return nil
	})
	return events, err
}

// MarkOutboxSent 标记 outbox 事件投递成功。
func (r *PersistentRuntime) MarkOutboxSent(ctx context.Context, eventID string) error {
	return r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		return repos.Outbox().MarkSent(ctx, eventID)
	})
}

// MarkOutboxFailed 标记 outbox 事件投递失败并设置下次重试时间。
func (r *PersistentRuntime) MarkOutboxFailed(ctx context.Context, req MarkOutboxFailedRequest) error {
	return r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		return repos.Outbox().MarkFailed(ctx, req.EventID, req.NextRetryAt)
	})
}

// MarkOutboxDead 标记 outbox 事件进入死信状态。
func (r *PersistentRuntime) MarkOutboxDead(ctx context.Context, eventID string) error {
	return r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		return repos.Outbox().MarkDead(ctx, eventID)
	})
}

// MarkTaskDispatched 标记外部任务已投递。
func (r *PersistentRuntime) MarkTaskDispatched(ctx context.Context, taskID string) error {
	return r.uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		task, err := repos.Tasks().Get(ctx, taskID)
		if err != nil {
			return err
		}
		if task.Type != domain.TaskTypeExternal {
			return fmt.Errorf("task %s is not an external task", taskID)
		}
		if task.Status == domain.TaskStatusCompleted || task.Status == domain.TaskStatusCanceled {
			return nil
		}
		task.Status = domain.TaskStatusDispatched
		if err := repos.Tasks().Update(ctx, task); err != nil {
			return err
		}
		return r.auditEvent(ctx, repos, task.InstanceID, domain.AuditTaskDispatched, task.NodeID, task.ID, nil)
	})
}

func (r *PersistentRuntime) enterNode(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID, nodeID string) error {
	node, ok := def.Model.Nodes[nodeID]
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}
	if err := r.auditEvent(ctx, repos, instanceID, domain.AuditNodeEntered, nodeID, "", map[string]any{"nodeType": node.Type}); err != nil {
		return err
	}

	switch node.Type {
	case domain.NodeTypeStartEvent:
		return r.leaveNode(ctx, repos, def, instanceID, executionID, nodeID)
	case domain.NodeTypeEndEvent:
		return r.completeInstance(ctx, repos, instanceID, node.ID)
	case domain.NodeTypeUserTask:
		_, err := r.createTask(ctx, repos, instanceID, executionID, node, domain.TaskTypeUser)
		return err
	case domain.NodeTypeServiceTask:
		task, err := r.createTask(ctx, repos, instanceID, executionID, node, domain.TaskTypeExternal)
		if err != nil {
			return err
		}
		return r.createDispatchOutbox(ctx, repos, instanceID, task, node)
	case domain.NodeTypeExclusiveGateway:
		return r.enterGateway(ctx, repos, def, instanceID, executionID, node)
	case domain.NodeTypeParallelGateway:
		return r.enterGateway(ctx, repos, def, instanceID, executionID, node)
	case domain.NodeTypeInclusiveGateway:
		return r.enterGateway(ctx, repos, def, instanceID, executionID, node)
	default:
		return fmt.Errorf("unsupported node type %s", node.Type)
	}
}

func (r *PersistentRuntime) leaveNode(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID, nodeID string) error {
	outgoing := def.Model.Outgoing(nodeID)
	if len(outgoing) == 0 {
		exec, err := repos.Instances().GetExecution(ctx, executionID)
		if err != nil {
			return err
		}
		exec.Status = domain.ExecutionStatusCompleted
		exec.UpdatedAt = r.clock.Now()
		return repos.Instances().SaveExecution(ctx, exec)
	}
	if len(outgoing) > 1 {
		return fmt.Errorf("node %s has %d outgoing sequence flows; use an exclusive gateway for branching", nodeID, len(outgoing))
	}
	exec, err := repos.Instances().GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	exec.NodeID = outgoing[0].TargetRef
	exec.UpdatedAt = r.clock.Now()
	if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
		return err
	}
	return r.enterNode(ctx, repos, def, instanceID, executionID, outgoing[0].TargetRef)
}

func (r *PersistentRuntime) enterGateway(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID string, node domain.Node) error {
	if len(def.Model.Incoming(node.ID)) > 1 {
		ready, err := r.arriveGatewayJoin(ctx, repos, instanceID, executionID, node)
		if err != nil || !ready {
			return err
		}
	}
	switch node.Type {
	case domain.NodeTypeExclusiveGateway:
		return r.evaluateExclusiveGateway(ctx, repos, def, instanceID, executionID, node)
	case domain.NodeTypeParallelGateway:
		return r.evaluateParallelGateway(ctx, repos, def, instanceID, executionID, node)
	case domain.NodeTypeInclusiveGateway:
		return r.evaluateInclusiveGateway(ctx, repos, def, instanceID, executionID, node)
	default:
		return fmt.Errorf("unsupported gateway type %s", node.Type)
	}
}

func (r *PersistentRuntime) arriveGatewayJoin(ctx context.Context, repos persistence.Repositories, instanceID, executionID string, node domain.Node) (bool, error) {
	exec, err := repos.Instances().GetExecution(ctx, executionID)
	if err != nil {
		return false, err
	}
	exec.NodeID = node.ID
	exec.Status = domain.ExecutionStatusWaiting
	exec.UpdatedAt = r.clock.Now()
	if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
		return false, err
	}
	if exec.JoinKey == "" {
		exec.Status = domain.ExecutionStatusActive
		return true, repos.Instances().SaveExecution(ctx, exec)
	}

	executions, err := repos.Instances().ExecutionsByInstance(ctx, instanceID)
	if err != nil {
		return false, err
	}
	active := make([]domain.Execution, 0)
	for _, current := range executions {
		if current.JoinKey == exec.JoinKey && current.Status != domain.ExecutionStatusCompleted && current.Status != domain.ExecutionStatusCanceled {
			active = append(active, current)
		}
	}
	for _, current := range active {
		if current.NodeID != node.ID || current.Status != domain.ExecutionStatusWaiting {
			return false, nil
		}
	}
	for _, current := range active {
		if current.ID == executionID {
			continue
		}
		current.Status = domain.ExecutionStatusCompleted
		current.UpdatedAt = r.clock.Now()
		if err := repos.Instances().SaveExecution(ctx, current); err != nil {
			return false, err
		}
	}
	exec.Status = domain.ExecutionStatusActive
	exec.JoinKey = ""
	exec.UpdatedAt = r.clock.Now()
	if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
		return false, err
	}
	return true, nil
}

func (r *PersistentRuntime) evaluateExclusiveGateway(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID string, node domain.Node) error {
	outgoing := def.Model.Outgoing(node.ID)
	if len(outgoing) == 1 {
		return r.moveExecutionTo(ctx, repos, def, instanceID, executionID, outgoing[0].TargetRef)
	}
	var defaultFlow *domain.SequenceFlow
	var selected *domain.SequenceFlow
	values, err := r.variableValues(ctx, repos, instanceID)
	if err != nil {
		return err
	}
	for i := range outgoing {
		flow := outgoing[i]
		if flow.Default {
			defaultFlow = &flow
			continue
		}
		matched, err := evalCondition(flow.Condition, values)
		if err != nil {
			return err
		}
		if matched {
			selected = &flow
			break
		}
	}
	if selected == nil {
		selected = defaultFlow
	}
	if selected == nil {
		return fmt.Errorf("exclusive gateway %s has no matched outgoing flow", node.ID)
	}
	if err := r.auditEvent(ctx, repos, instanceID, domain.AuditGatewayEvaluated, node.ID, "", map[string]any{
		"selectedFlowId": selected.ID,
		"targetNodeId":   selected.TargetRef,
	}); err != nil {
		return err
	}
	return r.moveExecutionTo(ctx, repos, def, instanceID, executionID, selected.TargetRef)
}

func (r *PersistentRuntime) evaluateParallelGateway(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID string, node domain.Node) error {
	outgoing := def.Model.Outgoing(node.ID)
	if len(outgoing) == 1 {
		return r.moveExecutionTo(ctx, repos, def, instanceID, executionID, outgoing[0].TargetRef)
	}
	targets := make([]string, 0, len(outgoing))
	for _, flow := range outgoing {
		targets = append(targets, flow.TargetRef)
	}
	if err := r.auditEvent(ctx, repos, instanceID, domain.AuditGatewayEvaluated, node.ID, "", map[string]any{
		"selectedFlowIds": flowIDs(outgoing),
		"targetNodeIds":   targets,
	}); err != nil {
		return err
	}
	return r.forkGateway(ctx, repos, def, instanceID, executionID, node, outgoing)
}

func (r *PersistentRuntime) evaluateInclusiveGateway(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID string, node domain.Node) error {
	outgoing := def.Model.Outgoing(node.ID)
	if len(outgoing) == 1 {
		return r.moveExecutionTo(ctx, repos, def, instanceID, executionID, outgoing[0].TargetRef)
	}
	var defaultFlow *domain.SequenceFlow
	selected := make([]domain.SequenceFlow, 0)
	values, err := r.variableValues(ctx, repos, instanceID)
	if err != nil {
		return err
	}
	for i := range outgoing {
		flow := outgoing[i]
		if flow.Default {
			defaultFlow = &flow
			continue
		}
		matched, err := evalCondition(flow.Condition, values)
		if err != nil {
			return err
		}
		if matched {
			selected = append(selected, flow)
		}
	}
	if len(selected) == 0 && defaultFlow != nil {
		selected = append(selected, *defaultFlow)
	}
	if len(selected) == 0 {
		return fmt.Errorf("inclusive gateway %s has no matched outgoing flow", node.ID)
	}
	targets := make([]string, 0, len(selected))
	for _, flow := range selected {
		targets = append(targets, flow.TargetRef)
	}
	if err := r.auditEvent(ctx, repos, instanceID, domain.AuditGatewayEvaluated, node.ID, "", map[string]any{
		"selectedFlowIds": flowIDs(selected),
		"targetNodeIds":   targets,
	}); err != nil {
		return err
	}
	return r.forkGateway(ctx, repos, def, instanceID, executionID, node, selected)
}

func (r *PersistentRuntime) forkGateway(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID string, node domain.Node, flows []domain.SequenceFlow) error {
	if len(flows) == 0 {
		return fmt.Errorf("gateway %s has no outgoing sequence flow", node.ID)
	}
	exec, err := repos.Instances().GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	joinKey := exec.JoinKey
	if joinKey == "" && len(flows) > 1 {
		joinKey = r.ids.Next("join")
	}
	for index, flow := range flows {
		nextExecutionID := executionID
		if index > 0 {
			now := r.clock.Now()
			nextExecutionID = r.ids.Next("exe")
			if err := repos.Instances().SaveExecution(ctx, domain.Execution{
				ID:         nextExecutionID,
				InstanceID: instanceID,
				ParentID:   executionID,
				Status:     domain.ExecutionStatusActive,
				JoinKey:    joinKey,
				CreatedAt:  now,
				UpdatedAt:  now,
			}); err != nil {
				return err
			}
		}
		nextExec, err := repos.Instances().GetExecution(ctx, nextExecutionID)
		if err != nil {
			return err
		}
		nextExec.NodeID = flow.TargetRef
		nextExec.Status = domain.ExecutionStatusActive
		nextExec.JoinKey = joinKey
		nextExec.UpdatedAt = r.clock.Now()
		if err := repos.Instances().SaveExecution(ctx, nextExec); err != nil {
			return err
		}
		if err := r.enterNode(ctx, repos, def, instanceID, nextExecutionID, flow.TargetRef); err != nil {
			return err
		}
	}
	return nil
}

func (r *PersistentRuntime) moveExecutionTo(ctx context.Context, repos persistence.Repositories, def domain.ProcessDefinition, instanceID, executionID, nodeID string) error {
	exec, err := repos.Instances().GetExecution(ctx, executionID)
	if err != nil {
		return err
	}
	exec.NodeID = nodeID
	exec.Status = domain.ExecutionStatusActive
	exec.UpdatedAt = r.clock.Now()
	if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
		return err
	}
	return r.enterNode(ctx, repos, def, instanceID, executionID, nodeID)
}

func (r *PersistentRuntime) createTask(ctx context.Context, repos persistence.Repositories, instanceID, executionID string, node domain.Node, taskType domain.TaskType) (domain.Task, error) {
	now := r.clock.Now()
	activity := domain.ActivityInstance{
		ID:          r.ids.Next("act"),
		InstanceID:  instanceID,
		ExecutionID: executionID,
		NodeID:      node.ID,
		NodeType:    node.Type,
		NodeName:    node.Name,
		Status:      domain.ActivityStatusWaiting,
		StartTime:   now,
	}
	values, err := r.variableValues(ctx, repos, instanceID)
	if err != nil {
		return domain.Task{}, err
	}
	task := buildTaskFromNode(r.ids, r.clock.Now, instanceID, activity.ID, node, taskType, values)
	if node.TimeoutSeconds > 0 {
		timeout := now.Add(time.Duration(node.TimeoutSeconds) * time.Second)
		task.TimeoutAt = &timeout
	}
	exec, err := repos.Instances().GetExecution(ctx, executionID)
	if err != nil {
		return domain.Task{}, err
	}
	exec.Status = domain.ExecutionStatusWaiting
	exec.UpdatedAt = now
	if err := repos.Instances().SaveActivity(ctx, activity); err != nil {
		return domain.Task{}, err
	}
	if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
		return domain.Task{}, err
	}
	if err := repos.Tasks().Save(ctx, task); err != nil {
		return domain.Task{}, err
	}
	if err := r.auditEvent(ctx, repos, instanceID, domain.AuditTaskCreated, node.ID, task.ID, taskAuditPayload(task)); err != nil {
		return domain.Task{}, err
	}
	if err := repos.Outbox().Save(ctx, newTaskLifecycleOutbox(r.ids, r.clock.Now(), string(domain.OutboxEventTaskCreated), task)); err != nil {
		return domain.Task{}, err
	}
	if err := repos.Outbox().Save(ctx, newNotificationOutbox(r.ids, r.clock.Now(), "task_created", task)); err != nil {
		return domain.Task{}, err
	}
	return task, nil
}

func (r *PersistentRuntime) createDispatchOutbox(ctx context.Context, repos persistence.Repositories, instanceID string, task domain.Task, node domain.Node) error {
	values, err := r.variableValues(ctx, repos, instanceID)
	if err != nil {
		return err
	}
	payloadVars, err := buildInputPayload(node, values)
	if err != nil {
		return err
	}
	event := domain.OutboxEvent{
		ID:          r.ids.Next("outbox"),
		EventType:   string(domain.OutboxEventExternalTaskDispatch),
		AggregateID: task.ID,
		Payload: map[string]any{
			"taskId":            task.ID,
			"processInstanceId": instanceID,
			"nodeId":            node.ID,
			"automationTaskKey": node.AutomationTaskKey,
			"dispatchUrl":       task.DispatchURL,
			"variables":         payloadVars,
			"callbackToken":     task.CallbackToken,
		},
		Status:    domain.OutboxStatusNew,
		CreatedAt: r.clock.Now(),
	}
	return repos.Outbox().Save(ctx, event)
}

func (r *PersistentRuntime) completeInstance(ctx context.Context, repos persistence.Repositories, instanceID, endNodeID string) error {
	now := r.clock.Now()
	instance, err := repos.Instances().Get(ctx, instanceID)
	if err != nil {
		return err
	}
	instance.Status = domain.InstanceStatusCompleted
	instance.EndTime = &now
	instance.EndNodeID = endNodeID
	instance.Version++
	if err := repos.Instances().Update(ctx, instance); err != nil {
		return err
	}
	if err := r.setVariable(ctx, repos, instanceID, "sys.status", string(instance.Status), domain.VariableScopeSystem, endNodeID, ""); err != nil {
		return err
	}
	executions, err := repos.Instances().ExecutionsByInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	for _, exec := range executions {
		if exec.Status == domain.ExecutionStatusCompleted {
			continue
		}
		exec.Status = domain.ExecutionStatusCompleted
		exec.UpdatedAt = now
		if err := repos.Instances().SaveExecution(ctx, exec); err != nil {
			return err
		}
	}
	tasks, err := repos.Tasks().ListByInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Status == domain.TaskStatusCompleted {
			continue
		}
		task.Status = domain.TaskStatusCanceled
		if err := repos.Tasks().Update(ctx, task); err != nil {
			return err
		}
	}
	return r.auditEvent(ctx, repos, instanceID, domain.AuditInstanceCompleted, endNodeID, "", nil)
}

func (r *PersistentRuntime) failInstance(ctx context.Context, repos persistence.Repositories, instanceID string, cause error) error {
	now := r.clock.Now()
	instance, err := repos.Instances().Get(ctx, instanceID)
	if err != nil {
		return err
	}
	instance.Status = domain.InstanceStatusFailed
	instance.EndTime = &now
	instance.FailReason = cause.Error()
	instance.Version++
	if err := repos.Instances().Update(ctx, instance); err != nil {
		return err
	}
	if err := r.setVariable(ctx, repos, instanceID, "sys.status", string(instance.Status), domain.VariableScopeSystem, "", ""); err != nil {
		return err
	}
	return r.auditEvent(ctx, repos, instanceID, domain.AuditInstanceFailed, "", "", map[string]any{"reason": cause.Error()})
}

func (r *PersistentRuntime) setVariable(ctx context.Context, repos persistence.Repositories, instanceID, name string, value any, scope domain.VariableScope, nodeID, taskID string) error {
	now := r.clock.Now()
	current, err := repos.Variables().CurrentByInstance(ctx, instanceID)
	if err != nil {
		return err
	}
	old := current[name]
	variable := domain.Variable{
		InstanceID:      instanceID,
		Name:            name,
		Type:            fmt.Sprintf("%T", value),
		Value:           value,
		Scope:           scope,
		UpdatedByNodeID: nodeID,
		UpdatedAt:       now,
	}
	if err := repos.Variables().UpsertCurrent(ctx, variable); err != nil {
		return err
	}
	if err := repos.Variables().AppendHistory(ctx, domain.VariableHistory{
		ID:           r.ids.Next("varhist"),
		InstanceID:   instanceID,
		Name:         name,
		OldValue:     old.Value,
		NewValue:     value,
		SourceNodeID: nodeID,
		SourceTaskID: taskID,
		CreatedAt:    now,
	}); err != nil {
		return err
	}
	return r.auditEvent(ctx, repos, instanceID, domain.AuditVariableUpdated, nodeID, taskID, map[string]any{"name": name, "scope": scope})
}

func (r *PersistentRuntime) auditEvent(ctx context.Context, repos persistence.Repositories, instanceID string, eventType domain.AuditEventType, nodeID, taskID string, payload map[string]any) error {
	return repos.Audit().Append(ctx, domain.AuditEvent{
		ID:         r.ids.Next("audit"),
		InstanceID: instanceID,
		EventType:  eventType,
		NodeID:     nodeID,
		TaskID:     taskID,
		Payload:    payload,
		CreatedAt:  r.clock.Now(),
	})
}

func (r *PersistentRuntime) findDefinition(ctx context.Context, repos persistence.Repositories, key string, version int) (domain.ProcessDefinition, error) {
	if version == 0 {
		return repos.Definitions().FindLatestDeployed(ctx, key)
	}
	return repos.Definitions().FindDeployedVersion(ctx, key, version)
}

func (r *PersistentRuntime) variableValues(ctx context.Context, repos persistence.Repositories, instanceID string) (map[string]any, error) {
	variables, err := repos.Variables().CurrentByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	for name, variable := range variables {
		result[name] = variable.Value
	}
	return result, nil
}

func sortTasksByCreatedAt(tasks []domain.Task) {
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.Before(tasks[j].CreatedAt) })
}
