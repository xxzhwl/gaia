package domain

import "time"

// DefinitionStatus 表示流程定义的生命周期状态。
type DefinitionStatus string

const (
	// DefinitionStatusDraft 表示流程定义仍处于草稿状态。
	DefinitionStatusDraft DefinitionStatus = "DRAFT"
	// DefinitionStatusDeployed 表示流程定义已部署，可用于发起实例。
	DefinitionStatusDeployed DefinitionStatus = "DEPLOYED"
	// DefinitionStatusDisabled 表示流程定义已停用。
	DefinitionStatusDisabled DefinitionStatus = "DISABLED"
)

// NodeType 表示流程图节点类型。
type NodeType string

const (
	// NodeTypeStartEvent 表示流程开始节点。
	NodeTypeStartEvent NodeType = "StartEvent"
	// NodeTypeEndEvent 表示流程结束节点。
	NodeTypeEndEvent NodeType = "EndEvent"
	// NodeTypeUserTask 表示人工待办节点。
	NodeTypeUserTask NodeType = "UserTask"
	// NodeTypeServiceTask 表示自动化外部任务节点。
	NodeTypeServiceTask NodeType = "ServiceTask"
	// NodeTypeExclusiveGateway 表示排他网关。
	NodeTypeExclusiveGateway NodeType = "ExclusiveGateway"
	// NodeTypeParallelGateway 表示并行网关。
	NodeTypeParallelGateway NodeType = "ParallelGateway"
	// NodeTypeInclusiveGateway 表示包容网关。
	NodeTypeInclusiveGateway NodeType = "InclusiveGateway"
)

// InstanceStatus 表示流程实例状态。
type InstanceStatus string

const (
	// InstanceStatusRunning 表示实例正在运行。
	InstanceStatusRunning InstanceStatus = "RUNNING"
	// InstanceStatusCompleted 表示实例已正常结束。
	InstanceStatusCompleted InstanceStatus = "COMPLETED"
	// InstanceStatusFailed 表示实例运行失败。
	InstanceStatusFailed InstanceStatus = "FAILED"
	// InstanceStatusTerminated 表示实例被人工终止。
	InstanceStatusTerminated InstanceStatus = "TERMINATED"
	// InstanceStatusSuspended 表示实例被挂起。
	InstanceStatusSuspended InstanceStatus = "SUSPENDED"
)

// ExecutionStatus 表示执行分支状态。
type ExecutionStatus string

const (
	// ExecutionStatusActive 表示执行分支可继续推进。
	ExecutionStatusActive ExecutionStatus = "ACTIVE"
	// ExecutionStatusWaiting 表示执行分支正在等待任务或网关汇聚。
	ExecutionStatusWaiting ExecutionStatus = "WAITING"
	// ExecutionStatusCompleted 表示执行分支已完成。
	ExecutionStatusCompleted ExecutionStatus = "COMPLETED"
	// ExecutionStatusCanceled 表示执行分支已被取消。
	ExecutionStatusCanceled ExecutionStatus = "CANCELED"
)

// ActivityStatus 表示节点活动实例状态。
type ActivityStatus string

const (
	// ActivityStatusReady 表示活动实例已创建但尚未开始。
	ActivityStatusReady ActivityStatus = "READY"
	// ActivityStatusRunning 表示活动实例正在执行。
	ActivityStatusRunning ActivityStatus = "RUNNING"
	// ActivityStatusWaiting 表示活动实例正在等待外部完成。
	ActivityStatusWaiting ActivityStatus = "WAITING"
	// ActivityStatusCompleted 表示活动实例已完成。
	ActivityStatusCompleted ActivityStatus = "COMPLETED"
	// ActivityStatusFailed 表示活动实例执行失败。
	ActivityStatusFailed ActivityStatus = "FAILED"
	// ActivityStatusCanceled 表示活动实例已取消。
	ActivityStatusCanceled ActivityStatus = "CANCELED"
)

// TaskType 表示任务类型。
type TaskType string

const (
	// TaskTypeUser 表示人工待办任务。
	TaskTypeUser TaskType = "USER_TASK"
	// TaskTypeExternal 表示自动化外部任务。
	TaskTypeExternal TaskType = "EXTERNAL_TASK"
)

// TaskStatus 表示任务状态。
type TaskStatus string

const (
	// TaskStatusCreated 表示任务已创建。
	TaskStatusCreated TaskStatus = "CREATED"
	// TaskStatusDispatched 表示自动化任务已投递给 worker。
	TaskStatusDispatched TaskStatus = "DISPATCHED"
	// TaskStatusWaiting 表示任务正在等待处理。
	TaskStatusWaiting TaskStatus = "WAITING"
	// TaskStatusClaimed 表示人工待办已被认领。
	TaskStatusClaimed TaskStatus = "CLAIMED"
	// TaskStatusCompleted 表示任务已完成。
	TaskStatusCompleted TaskStatus = "COMPLETED"
	// TaskStatusRejected 表示任务以驳回动作完成。
	TaskStatusRejected TaskStatus = "REJECTED"
	// TaskStatusFailed 表示任务处理失败。
	TaskStatusFailed TaskStatus = "FAILED"
	// TaskStatusCanceled 表示任务被流程结束或终止动作取消。
	TaskStatusCanceled TaskStatus = "CANCELED"
)

// VariableScope 表示流程变量作用域。
type VariableScope string

const (
	// VariableScopeSystem 表示引擎维护的系统变量。
	VariableScopeSystem VariableScope = "SYSTEM"
	// VariableScopeBusiness 表示业务传入或节点产出的变量。
	VariableScopeBusiness VariableScope = "BUSINESS"
)

// OutboxStatus 表示异步事件投递状态。
type OutboxStatus string

const (
	// OutboxStatusNew 表示事件尚未被调度器领取。
	OutboxStatusNew OutboxStatus = "NEW"
	// OutboxStatusProcessing 表示事件已被调度器领取。
	OutboxStatusProcessing OutboxStatus = "PROCESSING"
	// OutboxStatusSent 表示事件投递成功。
	OutboxStatusSent OutboxStatus = "SENT"
	// OutboxStatusFailed 表示事件投递失败，等待重试。
	OutboxStatusFailed OutboxStatus = "FAILED"
	// OutboxStatusDead 表示事件超过重试次数，不再自动投递。
	OutboxStatusDead OutboxStatus = "DEAD"
)

// OutboxEventType 表示工作流组件写入 outbox 的事件类型。
type OutboxEventType string

const (
	// OutboxEventExternalTaskDispatch 表示需要投递自动化外部任务。
	OutboxEventExternalTaskDispatch OutboxEventType = "EXTERNAL_TASK_DISPATCH"
	// OutboxEventTaskCreated 表示任务已创建。
	OutboxEventTaskCreated OutboxEventType = "TASK_CREATED"
	// OutboxEventTaskCompleted 表示任务已完成。
	OutboxEventTaskCompleted OutboxEventType = "TASK_COMPLETED"
	// OutboxEventTaskTimeout 表示任务已到达超时时间。
	OutboxEventTaskTimeout OutboxEventType = "TASK_TIMEOUT"
	// OutboxEventNotificationRequested 表示上游可根据该事件投递通知。
	OutboxEventNotificationRequested OutboxEventType = "NOTIFICATION_REQUESTED"
)

// AuditEventType 表示审计事件类型。
type AuditEventType string

const (
	// AuditInstanceStarted 表示流程实例已启动。
	AuditInstanceStarted AuditEventType = "INSTANCE_STARTED"
	// AuditNodeEntered 表示执行分支进入某个节点。
	AuditNodeEntered AuditEventType = "NODE_ENTERED"
	// AuditTaskCreated 表示任务已创建。
	AuditTaskCreated AuditEventType = "TASK_CREATED"
	// AuditTaskDispatched 表示自动化任务已投递。
	AuditTaskDispatched AuditEventType = "TASK_DISPATCHED"
	// AuditTaskClaimed 表示人工待办已认领。
	AuditTaskClaimed AuditEventType = "TASK_CLAIMED"
	// AuditTaskUnclaimed 表示人工待办已取消认领。
	AuditTaskUnclaimed AuditEventType = "TASK_UNCLAIMED"
	// AuditTaskTransferred 表示人工待办已转办。
	AuditTaskTransferred AuditEventType = "TASK_TRANSFERRED"
	// AuditTaskDelegated 表示人工待办已委托。
	AuditTaskDelegated AuditEventType = "TASK_DELEGATED"
	// AuditTaskCompleted 表示任务已完成。
	AuditTaskCompleted AuditEventType = "TASK_COMPLETED"
	// AuditTaskRejected 表示任务以驳回动作完成。
	AuditTaskRejected AuditEventType = "TASK_REJECTED"
	// AuditTaskTimedOut 表示任务已到达超时时间。
	AuditTaskTimedOut AuditEventType = "TASK_TIMED_OUT"
	// AuditVariableUpdated 表示流程变量已更新。
	AuditVariableUpdated AuditEventType = "VARIABLE_UPDATED"
	// AuditGatewayEvaluated 表示网关条件已计算。
	AuditGatewayEvaluated AuditEventType = "GATEWAY_EVALUATED"
	// AuditInstanceCompleted 表示流程实例已完成。
	AuditInstanceCompleted AuditEventType = "INSTANCE_COMPLETED"
	// AuditInstanceFailed 表示流程实例已失败。
	AuditInstanceFailed AuditEventType = "INSTANCE_FAILED"
)

// Clock 抽象当前时间，便于运行时测试和替换时间来源。
type Clock interface {
	Now() time.Time
}

// SystemClock 使用系统 UTC 时间。
type SystemClock struct{}

// Now 返回当前 UTC 时间。
func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}
