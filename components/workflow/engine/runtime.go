package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

// Runtime 是内存版流程运行时，适合单进程测试和轻量本地场景。
type Runtime struct {
	mu sync.Mutex

	clock domain.Clock
	ids   IDGenerator

	definitions        map[string]domain.ProcessDefinition
	latestByKey        map[string]string
	instances          map[string]domain.ProcessInstance
	executions         map[string]domain.Execution
	activities         map[string]domain.ActivityInstance
	tasks              map[string]domain.Task
	variables          map[string]map[string]domain.Variable
	variableHistory    []domain.VariableHistory
	outbox             map[string]domain.OutboxEvent
	audit              []domain.AuditEvent
	activityByTaskID   map[string]string
	definitionsByInst  map[string]domain.ProcessDefinition
	executionsByInstID map[string][]string
	tasksByInstID      map[string][]string
}

// NewRuntime 创建内存版流程运行时。
func NewRuntime(clock domain.Clock, ids IDGenerator) *Runtime {
	if clock == nil {
		clock = domain.SystemClock{}
	}
	if ids == nil {
		ids = &SequenceIDGenerator{}
	}
	return &Runtime{
		clock:              clock,
		ids:                ids,
		definitions:        map[string]domain.ProcessDefinition{},
		latestByKey:        map[string]string{},
		instances:          map[string]domain.ProcessInstance{},
		executions:         map[string]domain.Execution{},
		activities:         map[string]domain.ActivityInstance{},
		tasks:              map[string]domain.Task{},
		variables:          map[string]map[string]domain.Variable{},
		outbox:             map[string]domain.OutboxEvent{},
		activityByTaskID:   map[string]string{},
		definitionsByInst:  map[string]domain.ProcessDefinition{},
		executionsByInstID: map[string][]string{},
		tasksByInstID:      map[string][]string{},
	}
}

// StartProcessRequest 表示启动流程实例的请求。
type StartProcessRequest struct {
	DefinitionKey     string         `json:"definitionKey"`
	DefinitionVersion int            `json:"definitionVersion"`
	BusinessKey       string         `json:"businessKey"`
	TenantID          string         `json:"tenantId"`
	Starter           string         `json:"starter"`
	Variables         map[string]any `json:"variables"`
}

// CompleteTaskRequest 表示完成任务并写回变量的请求。
type CompleteTaskRequest struct {
	TaskID    string         `json:"taskId"`
	Operator  string         `json:"operator"`
	Action    string         `json:"action"`
	Comment   string         `json:"comment"`
	Variables map[string]any `json:"variables"`
}

// TaskOperationRequest 表示人工待办操作请求。
type TaskOperationRequest struct {
	TaskID         string         `json:"taskId"`
	Operator       string         `json:"operator"`
	TargetAssignee string         `json:"targetAssignee"`
	Action         string         `json:"action"`
	Comment        string         `json:"comment"`
	Variables      map[string]any `json:"variables"`
}

// MarkOutboxFailedRequest 表示标记 outbox 投递失败的请求。
type MarkOutboxFailedRequest struct {
	EventID     string
	NextRetryAt *time.Time
}

// ScanTimeoutTasksRequest 表示扫描超时任务的请求。
type ScanTimeoutTasksRequest struct {
	Now   time.Time
	Limit int
}

// ScanTimeoutTasksResult 表示超时任务扫描结果。
type ScanTimeoutTasksResult struct {
	Scanned  int
	TimedOut int
	Events   []domain.OutboxEvent
}

// CreateDefinition 创建草稿流程定义。
func (r *Runtime) CreateDefinition(_ context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := validateDefinitionMetadata(def); err != nil {
		return domain.ProcessDefinition{}, err
	}
	now := r.clock.Now()
	def.ID = r.ids.Next("def")
	def.Version = r.nextDefinitionVersion(def.Key)
	def.Status = domain.DefinitionStatusDraft
	def.Model = normalizeWorkflowModel(def.Model)
	def.CreatedAt = now
	def.DeployedAt = nil
	r.definitions[def.ID] = def
	return def, nil
}

// UpdateDefinition 更新已有流程定义内容。
func (r *Runtime) UpdateDefinition(_ context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.definitions[definitionID]
	if !ok {
		return domain.ProcessDefinition{}, fmt.Errorf("process definition %s not found", definitionID)
	}
	if strings.TrimSpace(def.Name) == "" {
		return domain.ProcessDefinition{}, fmt.Errorf("definition name is required")
	}
	existing.Name = strings.TrimSpace(def.Name)
	existing.Model = normalizeWorkflowModel(def.Model)
	existing.Inputs = def.Inputs
	r.definitions[definitionID] = existing
	return existing, nil
}

// DeployDefinition 部署流程定义，并始终生成新的可运行版本。
func (r *Runtime) DeployDefinition(_ context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if def.ID != "" {
		existing, ok := r.definitions[def.ID]
		if !ok {
			return domain.ProcessDefinition{}, fmt.Errorf("process definition %s not found", def.ID)
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
	now := r.clock.Now()
	def.ID = r.ids.Next("def")
	def.Version = r.nextDefinitionVersion(def.Key)
	def.CreatedAt = now
	def.Status = domain.DefinitionStatusDeployed
	def.DeployedAt = &now
	r.definitions[def.ID] = def
	r.latestByKey[def.Key] = def.ID
	return def, nil
}

// GetDefinition 获取流程定义。
func (r *Runtime) GetDefinition(_ context.Context, definitionID string) (domain.ProcessDefinition, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	def, ok := r.definitions[definitionID]
	return def, ok
}

// StartProcess 启动流程实例并从开始节点推进。
func (r *Runtime) StartProcess(_ context.Context, req StartProcessRequest) (domain.ProcessInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	def, err := r.findDefinition(req.DefinitionKey, req.DefinitionVersion)
	if err != nil {
		return domain.ProcessInstance{}, err
	}
	req.Variables = variablesWithDefaults(def.Inputs, req.Variables)
	if err := validateInputs(def.Inputs, req.Variables); err != nil {
		return domain.ProcessInstance{}, err
	}
	startNode, ok := def.Model.StartNode()
	if !ok {
		return domain.ProcessInstance{}, fmt.Errorf("definition %s has no start event", def.ID)
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
	r.instances[instance.ID] = instance
	r.definitionsByInst[instance.ID] = def
	r.variables[instance.ID] = map[string]domain.Variable{}
	r.auditEvent(instance.ID, domain.AuditInstanceStarted, "", "", map[string]any{
		"definitionKey":     def.Key,
		"definitionVersion": def.Version,
		"businessKey":       req.BusinessKey,
	})

	r.setVariable(instance.ID, "sys.instanceId", instance.ID, domain.VariableScopeSystem, "", "")
	r.setVariable(instance.ID, "sys.definitionKey", def.Key, domain.VariableScopeSystem, "", "")
	r.setVariable(instance.ID, "sys.definitionVersion", def.Version, domain.VariableScopeSystem, "", "")
	r.setVariable(instance.ID, "sys.starter", req.Starter, domain.VariableScopeSystem, "", "")
	r.setVariable(instance.ID, "sys.status", string(instance.Status), domain.VariableScopeSystem, "", "")
	for name, value := range req.Variables {
		r.setVariable(instance.ID, name, value, domain.VariableScopeBusiness, "", "")
	}

	exec := domain.Execution{
		ID:         r.ids.Next("exe"),
		InstanceID: instance.ID,
		NodeID:     startNode.ID,
		Status:     domain.ExecutionStatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.executions[exec.ID] = exec
	r.executionsByInstID[instance.ID] = append(r.executionsByInstID[instance.ID], exec.ID)
	if err := r.enterNode(instance.ID, exec.ID, startNode.ID); err != nil {
		return r.failInstance(instance.ID, err), err
	}
	return r.instances[instance.ID], nil
}

// CompleteTask 完成任务并继续推进流程。
func (r *Runtime) CompleteTask(_ context.Context, req CompleteTaskRequest) (domain.ProcessInstance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	task, ok := r.tasks[req.TaskID]
	if !ok {
		return domain.ProcessInstance{}, fmt.Errorf("task %s not found", req.TaskID)
	}
	if task.Status == domain.TaskStatusCompleted {
		return r.instances[task.InstanceID], nil
	}
	if task.Status != domain.TaskStatusWaiting && task.Status != domain.TaskStatusDispatched && task.Status != domain.TaskStatusCreated && task.Status != domain.TaskStatusClaimed {
		return domain.ProcessInstance{}, fmt.Errorf("task %s status %s cannot be completed", task.ID, task.Status)
	}
	instance := r.instances[task.InstanceID]
	if instance.Status != domain.InstanceStatusRunning {
		return instance, fmt.Errorf("process instance %s is not running", instance.ID)
	}

	for name, value := range req.Variables {
		r.setVariable(task.InstanceID, name, value, domain.VariableScopeBusiness, task.NodeID, task.ID)
	}

	now := r.clock.Now()
	task.Status = domain.TaskStatusCompleted
	task.Action = strings.TrimSpace(req.Action)
	task.Comment = strings.TrimSpace(req.Comment)
	task.CompletedBy = strings.TrimSpace(req.Operator)
	task.CompletedAt = &now
	r.tasks[task.ID] = task
	eventType := domain.AuditTaskCompleted
	if strings.EqualFold(task.Action, "reject") || strings.EqualFold(task.Action, "rejected") {
		eventType = domain.AuditTaskRejected
	}
	r.auditEvent(task.InstanceID, eventType, task.NodeID, task.ID, taskAuditPayload(task))
	r.createTaskLifecycleOutbox(string(domain.OutboxEventTaskCompleted), task)
	r.createNotificationOutbox("task_completed", task)

	activity := r.activities[task.ActivityInstanceID]
	activity.Status = domain.ActivityStatusCompleted
	activity.EndTime = &now
	r.activities[activity.ID] = activity

	exec := r.executions[activity.ExecutionID]
	exec.Status = domain.ExecutionStatusActive
	exec.UpdatedAt = now
	r.executions[exec.ID] = exec

	if err := r.leaveNode(task.InstanceID, exec.ID, task.NodeID); err != nil {
		return r.failInstance(task.InstanceID, err), err
	}
	return r.instances[task.InstanceID], nil
}

// ClaimTask 认领人工待办。
func (r *Runtime) ClaimTask(_ context.Context, req TaskOperationRequest) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[req.TaskID]
	if !ok {
		return domain.Task{}, fmt.Errorf("task %s not found", req.TaskID)
	}
	if task.Type != domain.TaskTypeUser {
		return domain.Task{}, fmt.Errorf("task %s is not a user task", task.ID)
	}
	if task.Status == domain.TaskStatusCompleted || task.Status == domain.TaskStatusCanceled || task.Status == domain.TaskStatusRejected {
		return domain.Task{}, fmt.Errorf("task %s status %s cannot be claimed", task.ID, task.Status)
	}
	operator := strings.TrimSpace(req.Operator)
	if operator == "" {
		return domain.Task{}, fmt.Errorf("operator is required")
	}
	if task.Assignee != "" && task.Assignee != operator {
		return domain.Task{}, fmt.Errorf("task %s has been assigned to %s", task.ID, task.Assignee)
	}
	now := r.clock.Now()
	task.Assignee = operator
	task.Status = domain.TaskStatusClaimed
	task.ClaimedAt = &now
	r.tasks[task.ID] = task
	r.auditEvent(task.InstanceID, domain.AuditTaskClaimed, task.NodeID, task.ID, taskAuditPayload(task))
	return task, nil
}

// UnclaimTask 取消认领人工待办。
func (r *Runtime) UnclaimTask(_ context.Context, req TaskOperationRequest) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[req.TaskID]
	if !ok {
		return domain.Task{}, fmt.Errorf("task %s not found", req.TaskID)
	}
	if task.Type != domain.TaskTypeUser {
		return domain.Task{}, fmt.Errorf("task %s is not a user task", task.ID)
	}
	if task.Status != domain.TaskStatusClaimed {
		return domain.Task{}, fmt.Errorf("task %s status %s cannot be unclaimed", task.ID, task.Status)
	}
	task.Assignee = ""
	task.Status = domain.TaskStatusWaiting
	task.ClaimedAt = nil
	r.tasks[task.ID] = task
	r.auditEvent(task.InstanceID, domain.AuditTaskUnclaimed, task.NodeID, task.ID, taskAuditPayload(task))
	return task, nil
}

// TransferTask 转办人工待办。
func (r *Runtime) TransferTask(_ context.Context, req TaskOperationRequest) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[req.TaskID]
	if !ok {
		return domain.Task{}, fmt.Errorf("task %s not found", req.TaskID)
	}
	return r.assignUserTask(task, req, domain.AuditTaskTransferred)
}

// DelegateTask 委托人工待办。
func (r *Runtime) DelegateTask(_ context.Context, req TaskOperationRequest) (domain.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[req.TaskID]
	if !ok {
		return domain.Task{}, fmt.Errorf("task %s not found", req.TaskID)
	}
	task.DelegatedFrom = task.Assignee
	return r.assignUserTask(task, req, domain.AuditTaskDelegated)
}

// RejectTask 以驳回动作完成人工待办。
func (r *Runtime) RejectTask(ctx context.Context, req TaskOperationRequest) (domain.ProcessInstance, error) {
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
func (r *Runtime) ScanTimeoutTasks(_ context.Context, req ScanTimeoutTasksRequest) (ScanTimeoutTasksResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := req.Now
	if now.IsZero() {
		now = r.clock.Now()
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}
	tasks := make([]domain.Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if task.TimeoutAt == nil || task.TimeoutAt.After(now) || !taskOpen(task.Status) {
			continue
		}
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].TimeoutAt.Before(*tasks[j].TimeoutAt) })
	if len(tasks) > limit {
		tasks = tasks[:limit]
	}

	result := ScanTimeoutTasksResult{Scanned: len(tasks)}
	for _, task := range tasks {
		if r.outboxExists(string(domain.OutboxEventTaskTimeout), task.ID) {
			continue
		}
		r.auditEvent(task.InstanceID, domain.AuditTaskTimedOut, task.NodeID, task.ID, taskAuditPayload(task))
		timeoutEvent := r.createTaskLifecycleOutbox(string(domain.OutboxEventTaskTimeout), task)
		notificationEvent := r.createNotificationOutbox("task_timeout", task)
		result.TimedOut++
		result.Events = append(result.Events, timeoutEvent, notificationEvent)
	}
	return result, nil
}

func (r *Runtime) assignUserTask(task domain.Task, req TaskOperationRequest, eventType domain.AuditEventType) (domain.Task, error) {
	if task.Type != domain.TaskTypeUser {
		return domain.Task{}, fmt.Errorf("task %s is not a user task", task.ID)
	}
	if task.Status == domain.TaskStatusCompleted || task.Status == domain.TaskStatusCanceled || task.Status == domain.TaskStatusRejected {
		return domain.Task{}, fmt.Errorf("task %s status %s cannot be assigned", task.ID, task.Status)
	}
	target := strings.TrimSpace(req.TargetAssignee)
	if target == "" {
		return domain.Task{}, fmt.Errorf("target assignee is required")
	}
	now := r.clock.Now()
	task.Owner = strings.TrimSpace(req.Operator)
	task.Assignee = target
	task.Comment = strings.TrimSpace(req.Comment)
	task.Status = domain.TaskStatusClaimed
	task.ClaimedAt = &now
	r.tasks[task.ID] = task
	r.auditEvent(task.InstanceID, eventType, task.NodeID, task.ID, taskAuditPayload(task))
	return task, nil
}

// GetInstance 获取流程实例。
func (r *Runtime) GetInstance(instanceID string) (domain.ProcessInstance, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	instance, ok := r.instances[instanceID]
	return instance, ok
}

// ListDefinitions 分页查询流程定义。
func (r *Runtime) ListDefinitions(filter domain.DefinitionListFilter) domain.PageResult[domain.ProcessDefinition] {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domain.ProcessDefinition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		if filter.Key != "" && !strings.Contains(definition.Key, filter.Key) {
			continue
		}
		if filter.Name != "" && !strings.Contains(definition.Name, filter.Name) {
			continue
		}
		if filter.Status != "" && definition.Status != filter.Status {
			continue
		}
		items = append(items, definition)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return pageSlice(items, filter.PageRequest)
}

// ListInstances 分页查询流程实例。
func (r *Runtime) ListInstances(filter domain.InstanceListFilter) domain.PageResult[domain.ProcessInstance] {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domain.ProcessInstance, 0, len(r.instances))
	for _, instance := range r.instances {
		if filter.DefinitionKey != "" && !strings.Contains(instance.DefinitionKey, filter.DefinitionKey) {
			continue
		}
		if filter.Status != "" && instance.Status != filter.Status {
			continue
		}
		items = append(items, instance)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartTime.After(items[j].StartTime) })
	return pageSlice(items, filter.PageRequest)
}

// ListTasks 分页查询任务。
func (r *Runtime) ListTasks(filter domain.TaskListFilter) domain.PageResult[domain.Task] {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domain.Task, 0, len(r.tasks))
	for _, task := range r.tasks {
		if filter.InstanceID != "" && task.InstanceID != filter.InstanceID {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		if filter.Type != "" && task.Type != filter.Type {
			continue
		}
		if filter.Assignee != "" && task.Assignee != filter.Assignee {
			continue
		}
		items = append(items, task)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return pageSlice(items, filter.PageRequest)
}

// Variables 查询流程实例当前变量。
func (r *Runtime) Variables(instanceID string) map[string]domain.Variable {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneVariableMap(r.variables[instanceID])
}

// Tasks 查询流程实例下的全部任务。
func (r *Runtime) Tasks(instanceID string) []domain.Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := append([]string(nil), r.tasksByInstID[instanceID]...)
	tasks := make([]domain.Task, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, r.tasks[id])
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.Before(tasks[j].CreatedAt) })
	return tasks
}

// AuditTrail 查询流程实例审计时间线。
func (r *Runtime) AuditTrail(instanceID string) []domain.AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]domain.AuditEvent, 0)
	for _, event := range r.audit {
		if event.InstanceID == instanceID {
			events = append(events, event)
		}
	}
	return events
}

// OutboxEvents 查询当前 outbox 事件。
func (r *Runtime) OutboxEvents() []domain.OutboxEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]domain.OutboxEvent, 0, len(r.outbox))
	for _, event := range r.outbox {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].CreatedAt.Before(events[j].CreatedAt) })
	return events
}

// ClaimOutboxBatch 领取一批可投递的 outbox 事件。
func (r *Runtime) ClaimOutboxBatch(limit int) []domain.OutboxEvent {
	return r.claimOutboxBatchByType(string(domain.OutboxEventExternalTaskDispatch), limit)
}

// ClaimNotificationOutboxBatch 领取一批通知 outbox 事件。
func (r *Runtime) ClaimNotificationOutboxBatch(limit int) []domain.OutboxEvent {
	return r.claimOutboxBatchByType(string(domain.OutboxEventNotificationRequested), limit)
}

func (r *Runtime) claimOutboxBatchByType(eventType string, limit int) []domain.OutboxEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 {
		limit = 10
	}
	now := r.clock.Now()
	events := make([]domain.OutboxEvent, 0)
	ids := make([]string, 0, len(r.outbox))
	for id := range r.outbox {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if len(events) >= limit {
			break
		}
		event := r.outbox[id]
		if event.EventType != eventType || (event.Status != domain.OutboxStatusNew && event.Status != domain.OutboxStatusFailed) {
			continue
		}
		if event.NextRetryAt != nil && event.NextRetryAt.After(now) {
			continue
		}
		event.Status = domain.OutboxStatusProcessing
		r.outbox[id] = event
		events = append(events, event)
	}
	return events
}

// MarkOutboxSent 标记 outbox 事件投递成功。
func (r *Runtime) MarkOutboxSent(eventID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.outbox[eventID]
	if !ok {
		return fmt.Errorf("outbox event %s not found", eventID)
	}
	event.Status = domain.OutboxStatusSent
	r.outbox[eventID] = event
	return nil
}

// MarkOutboxFailed 标记 outbox 事件投递失败并设置下次重试时间。
func (r *Runtime) MarkOutboxFailed(req MarkOutboxFailedRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.outbox[req.EventID]
	if !ok {
		return fmt.Errorf("outbox event %s not found", req.EventID)
	}
	event.Status = domain.OutboxStatusFailed
	event.RetryCount++
	event.NextRetryAt = req.NextRetryAt
	r.outbox[req.EventID] = event
	return nil
}

// MarkOutboxDead 标记 outbox 事件进入死信状态。
func (r *Runtime) MarkOutboxDead(eventID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.outbox[eventID]
	if !ok {
		return fmt.Errorf("outbox event %s not found", eventID)
	}
	event.Status = domain.OutboxStatusDead
	event.RetryCount++
	r.outbox[eventID] = event
	return nil
}

// MarkTaskDispatched 标记外部任务已投递。
func (r *Runtime) MarkTaskDispatched(taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}
	if task.Type != domain.TaskTypeExternal {
		return fmt.Errorf("task %s is not an external task", taskID)
	}
	if task.Status == domain.TaskStatusCompleted || task.Status == domain.TaskStatusCanceled {
		return nil
	}
	task.Status = domain.TaskStatusDispatched
	r.tasks[taskID] = task
	r.auditEvent(task.InstanceID, domain.AuditTaskDispatched, task.NodeID, task.ID, nil)
	return nil
}

func (r *Runtime) enterNode(instanceID, executionID, nodeID string) error {
	def := r.definitionsByInst[instanceID]
	node, ok := def.Model.Nodes[nodeID]
	if !ok {
		return fmt.Errorf("node %s not found", nodeID)
	}
	r.auditEvent(instanceID, domain.AuditNodeEntered, nodeID, "", map[string]any{"nodeType": node.Type})

	switch node.Type {
	case domain.NodeTypeStartEvent:
		return r.leaveNode(instanceID, executionID, nodeID)
	case domain.NodeTypeEndEvent:
		return r.completeInstance(instanceID, node.ID)
	case domain.NodeTypeUserTask:
		r.createTask(instanceID, executionID, node, domain.TaskTypeUser)
		return nil
	case domain.NodeTypeServiceTask:
		task := r.createTask(instanceID, executionID, node, domain.TaskTypeExternal)
		return r.createDispatchOutbox(instanceID, task, node)
	case domain.NodeTypeExclusiveGateway:
		return r.enterGateway(instanceID, executionID, node)
	case domain.NodeTypeParallelGateway:
		return r.enterGateway(instanceID, executionID, node)
	case domain.NodeTypeInclusiveGateway:
		return r.enterGateway(instanceID, executionID, node)
	default:
		return fmt.Errorf("unsupported node type %s", node.Type)
	}
}

func (r *Runtime) leaveNode(instanceID, executionID, nodeID string) error {
	def := r.definitionsByInst[instanceID]
	outgoing := def.Model.Outgoing(nodeID)
	if len(outgoing) == 0 {
		exec := r.executions[executionID]
		exec.Status = domain.ExecutionStatusCompleted
		exec.UpdatedAt = r.clock.Now()
		r.executions[executionID] = exec
		return nil
	}
	if len(outgoing) > 1 {
		return fmt.Errorf("node %s has %d outgoing sequence flows; use an exclusive gateway for branching", nodeID, len(outgoing))
	}
	nextNodeID := outgoing[0].TargetRef
	exec := r.executions[executionID]
	exec.NodeID = nextNodeID
	exec.UpdatedAt = r.clock.Now()
	r.executions[executionID] = exec
	return r.enterNode(instanceID, executionID, nextNodeID)
}

func (r *Runtime) enterGateway(instanceID, executionID string, node domain.Node) error {
	def := r.definitionsByInst[instanceID]
	if len(def.Model.Incoming(node.ID)) > 1 {
		ready, err := r.arriveGatewayJoin(instanceID, executionID, node)
		if err != nil || !ready {
			return err
		}
	}
	switch node.Type {
	case domain.NodeTypeExclusiveGateway:
		return r.evaluateExclusiveGateway(instanceID, executionID, node)
	case domain.NodeTypeParallelGateway:
		return r.evaluateParallelGateway(instanceID, executionID, node)
	case domain.NodeTypeInclusiveGateway:
		return r.evaluateInclusiveGateway(instanceID, executionID, node)
	default:
		return fmt.Errorf("unsupported gateway type %s", node.Type)
	}
}

func (r *Runtime) arriveGatewayJoin(instanceID, executionID string, node domain.Node) (bool, error) {
	exec := r.executions[executionID]
	exec.NodeID = node.ID
	exec.Status = domain.ExecutionStatusWaiting
	exec.UpdatedAt = r.clock.Now()
	r.executions[executionID] = exec

	if exec.JoinKey == "" {
		exec.Status = domain.ExecutionStatusActive
		r.executions[executionID] = exec
		return true, nil
	}
	active := make([]domain.Execution, 0)
	for _, id := range r.executionsByInstID[instanceID] {
		current := r.executions[id]
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
		r.executions[current.ID] = current
	}
	exec.Status = domain.ExecutionStatusActive
	exec.JoinKey = ""
	exec.UpdatedAt = r.clock.Now()
	r.executions[executionID] = exec
	return true, nil
}

func (r *Runtime) evaluateExclusiveGateway(instanceID, executionID string, node domain.Node) error {
	def := r.definitionsByInst[instanceID]
	outgoing := def.Model.Outgoing(node.ID)
	if len(outgoing) == 1 {
		return r.moveExecutionTo(instanceID, executionID, outgoing[0].TargetRef)
	}
	var defaultFlow *domain.SequenceFlow
	var selected *domain.SequenceFlow
	values := r.variableValues(instanceID)
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
	r.auditEvent(instanceID, domain.AuditGatewayEvaluated, node.ID, "", map[string]any{
		"selectedFlowId": selected.ID,
		"targetNodeId":   selected.TargetRef,
	})

	return r.moveExecutionTo(instanceID, executionID, selected.TargetRef)
}

func (r *Runtime) evaluateParallelGateway(instanceID, executionID string, node domain.Node) error {
	def := r.definitionsByInst[instanceID]
	outgoing := def.Model.Outgoing(node.ID)
	if len(outgoing) == 1 {
		return r.moveExecutionTo(instanceID, executionID, outgoing[0].TargetRef)
	}
	targets := make([]string, 0, len(outgoing))
	for _, flow := range outgoing {
		targets = append(targets, flow.TargetRef)
	}
	r.auditEvent(instanceID, domain.AuditGatewayEvaluated, node.ID, "", map[string]any{
		"selectedFlowIds": flowIDs(outgoing),
		"targetNodeIds":   targets,
	})
	return r.forkGateway(instanceID, executionID, node, outgoing)
}

func (r *Runtime) evaluateInclusiveGateway(instanceID, executionID string, node domain.Node) error {
	def := r.definitionsByInst[instanceID]
	outgoing := def.Model.Outgoing(node.ID)
	if len(outgoing) == 1 {
		return r.moveExecutionTo(instanceID, executionID, outgoing[0].TargetRef)
	}
	var defaultFlow *domain.SequenceFlow
	selected := make([]domain.SequenceFlow, 0)
	values := r.variableValues(instanceID)
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
	r.auditEvent(instanceID, domain.AuditGatewayEvaluated, node.ID, "", map[string]any{
		"selectedFlowIds": flowIDs(selected),
		"targetNodeIds":   targets,
	})
	return r.forkGateway(instanceID, executionID, node, selected)
}

func (r *Runtime) forkGateway(instanceID, executionID string, node domain.Node, flows []domain.SequenceFlow) error {
	if len(flows) == 0 {
		return fmt.Errorf("gateway %s has no outgoing sequence flow", node.ID)
	}
	joinKey := r.executions[executionID].JoinKey
	if joinKey == "" && len(flows) > 1 {
		joinKey = r.ids.Next("join")
	}
	for index, flow := range flows {
		nextExecutionID := executionID
		if index > 0 {
			now := r.clock.Now()
			nextExecutionID = r.ids.Next("exe")
			r.executions[nextExecutionID] = domain.Execution{
				ID:         nextExecutionID,
				InstanceID: instanceID,
				ParentID:   executionID,
				Status:     domain.ExecutionStatusActive,
				JoinKey:    joinKey,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			r.executionsByInstID[instanceID] = append(r.executionsByInstID[instanceID], nextExecutionID)
		}
		exec := r.executions[nextExecutionID]
		exec.NodeID = flow.TargetRef
		exec.Status = domain.ExecutionStatusActive
		exec.JoinKey = joinKey
		exec.UpdatedAt = r.clock.Now()
		r.executions[nextExecutionID] = exec
		if err := r.enterNode(instanceID, nextExecutionID, flow.TargetRef); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) moveExecutionTo(instanceID, executionID, nodeID string) error {
	exec := r.executions[executionID]
	exec.NodeID = nodeID
	exec.Status = domain.ExecutionStatusActive
	exec.UpdatedAt = r.clock.Now()
	r.executions[executionID] = exec
	return r.enterNode(instanceID, executionID, nodeID)
}

func (r *Runtime) createTask(instanceID, executionID string, node domain.Node, taskType domain.TaskType) domain.Task {
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
	task := buildTaskFromNode(r.ids, r.clock.Now, instanceID, activity.ID, node, taskType, r.variableValues(instanceID))
	if node.TimeoutSeconds > 0 {
		timeout := now.Add(time.Duration(node.TimeoutSeconds) * time.Second)
		task.TimeoutAt = &timeout
	}

	exec := r.executions[executionID]
	exec.Status = domain.ExecutionStatusWaiting
	exec.UpdatedAt = now

	r.activities[activity.ID] = activity
	r.executions[executionID] = exec
	r.tasks[task.ID] = task
	r.tasksByInstID[instanceID] = append(r.tasksByInstID[instanceID], task.ID)
	r.activityByTaskID[task.ID] = activity.ID
	r.auditEvent(instanceID, domain.AuditTaskCreated, node.ID, task.ID, taskAuditPayload(task))
	r.createTaskLifecycleOutbox(string(domain.OutboxEventTaskCreated), task)
	r.createNotificationOutbox("task_created", task)
	return task
}

func (r *Runtime) createDispatchOutbox(instanceID string, task domain.Task, node domain.Node) error {
	values := r.variableValues(instanceID)
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
	r.outbox[event.ID] = event
	return nil
}

func (r *Runtime) completeInstance(instanceID, endNodeID string) error {
	now := r.clock.Now()
	instance := r.instances[instanceID]
	instance.Status = domain.InstanceStatusCompleted
	instance.EndTime = &now
	instance.EndNodeID = endNodeID
	instance.Version++
	r.instances[instanceID] = instance
	r.setVariable(instanceID, "sys.status", string(instance.Status), domain.VariableScopeSystem, endNodeID, "")
	for _, execID := range r.executionsByInstID[instanceID] {
		exec := r.executions[execID]
		if exec.Status != domain.ExecutionStatusCompleted {
			exec.Status = domain.ExecutionStatusCompleted
			exec.UpdatedAt = now
			r.executions[execID] = exec
		}
	}
	for _, taskID := range r.tasksByInstID[instanceID] {
		task := r.tasks[taskID]
		if task.Status != domain.TaskStatusCompleted {
			task.Status = domain.TaskStatusCanceled
			r.tasks[taskID] = task
		}
	}
	r.auditEvent(instanceID, domain.AuditInstanceCompleted, endNodeID, "", nil)
	return nil
}

func (r *Runtime) failInstance(instanceID string, cause error) domain.ProcessInstance {
	now := r.clock.Now()
	instance := r.instances[instanceID]
	instance.Status = domain.InstanceStatusFailed
	instance.EndTime = &now
	instance.FailReason = cause.Error()
	instance.Version++
	r.instances[instanceID] = instance
	r.setVariable(instanceID, "sys.status", string(instance.Status), domain.VariableScopeSystem, "", "")
	r.auditEvent(instanceID, domain.AuditInstanceFailed, "", "", map[string]any{"reason": cause.Error()})
	return instance
}

func (r *Runtime) setVariable(instanceID, name string, value any, scope domain.VariableScope, nodeID, taskID string) {
	now := r.clock.Now()
	if r.variables[instanceID] == nil {
		r.variables[instanceID] = map[string]domain.Variable{}
	}
	old := r.variables[instanceID][name]
	next := domain.Variable{
		InstanceID:      instanceID,
		Name:            name,
		Type:            fmt.Sprintf("%T", value),
		Value:           value,
		Scope:           scope,
		UpdatedByNodeID: nodeID,
		UpdatedAt:       now,
	}
	r.variables[instanceID][name] = next
	r.variableHistory = append(r.variableHistory, domain.VariableHistory{
		ID:           r.ids.Next("varhist"),
		InstanceID:   instanceID,
		Name:         name,
		OldValue:     old.Value,
		NewValue:     value,
		SourceNodeID: nodeID,
		SourceTaskID: taskID,
		CreatedAt:    now,
	})
	r.auditEvent(instanceID, domain.AuditVariableUpdated, nodeID, taskID, map[string]any{"name": name, "scope": scope})
}

func (r *Runtime) auditEvent(instanceID string, eventType domain.AuditEventType, nodeID, taskID string, payload map[string]any) {
	r.audit = append(r.audit, domain.AuditEvent{
		ID:         r.ids.Next("audit"),
		InstanceID: instanceID,
		EventType:  eventType,
		NodeID:     nodeID,
		TaskID:     taskID,
		Payload:    payload,
		CreatedAt:  r.clock.Now(),
	})
}

func (r *Runtime) nextDefinitionVersion(key string) int {
	maxVersion := 0
	for _, def := range r.definitions {
		if def.Key == key && def.Version > maxVersion {
			maxVersion = def.Version
		}
	}
	return maxVersion + 1
}

func (r *Runtime) findDefinition(key string, version int) (domain.ProcessDefinition, error) {
	if version == 0 {
		id, ok := r.latestByKey[key]
		if !ok {
			return domain.ProcessDefinition{}, fmt.Errorf("deployed definition %s not found", key)
		}
		return r.definitions[id], nil
	}
	for _, def := range r.definitions {
		if def.Key == key && def.Version == version && def.Status == domain.DefinitionStatusDeployed {
			return def, nil
		}
	}
	return domain.ProcessDefinition{}, fmt.Errorf("deployed definition %s version %d not found", key, version)
}

func validateInputs(inputs []domain.InputParameter, vars map[string]any) error {
	for _, input := range inputs {
		key := inputVariableKey(input)
		if input.Required {
			if _, ok := vars[key]; !ok {
				return fmt.Errorf("required input variable %s is missing", key)
			}
		}
	}
	return nil
}

func validateDefinitionMetadata(def domain.ProcessDefinition) error {
	if strings.TrimSpace(def.Key) == "" {
		return fmt.Errorf("definition key is required")
	}
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("definition name is required")
	}
	return nil
}

func normalizeWorkflowModel(model domain.WorkflowModel) domain.WorkflowModel {
	if model.Nodes == nil {
		model.Nodes = map[string]domain.Node{}
	}
	if model.SequenceFlows == nil {
		model.SequenceFlows = []domain.SequenceFlow{}
	}
	return model
}

func variablesWithDefaults(inputs []domain.InputParameter, vars map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range vars {
		result[key] = value
	}
	for _, input := range inputs {
		key := inputVariableKey(input)
		if key == "" {
			continue
		}
		if _, exists := result[key]; exists || input.DefaultValue == nil {
			continue
		}
		result[key] = input.DefaultValue
	}
	return result
}

func inputVariableKey(input domain.InputParameter) string {
	key := strings.TrimSpace(input.Key)
	if key == "" {
		key = strings.TrimSpace(input.Name)
	}
	return key
}

func (r *Runtime) variableValues(instanceID string) map[string]any {
	result := map[string]any{}
	for name, variable := range r.variables[instanceID] {
		result[name] = variable.Value
	}
	return result
}

func cloneVariableMap(input map[string]domain.Variable) map[string]domain.Variable {
	result := map[string]domain.Variable{}
	for k, v := range input {
		result[k] = v
	}
	return result
}

func pageSlice[T any](items []T, req domain.PageRequest) domain.PageResult[T] {
	page := domain.NormalizePage(req)
	total := int64(len(items))
	start := domain.PageOffset(page)
	if start >= len(items) {
		return domain.PageResult[T]{List: []T{}, Total: total}
	}
	end := start + page.PageSize
	if end > len(items) {
		end = len(items)
	}
	return domain.PageResult[T]{List: items[start:end], Total: total}
}

func flowIDs(flows []domain.SequenceFlow) []string {
	ids := make([]string, 0, len(flows))
	for _, flow := range flows {
		ids = append(ids, flow.ID)
	}
	return ids
}
