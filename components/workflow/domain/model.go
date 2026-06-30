package domain

import "time"

// ProcessDefinition 表示一个可部署或已部署的流程定义版本。
type ProcessDefinition struct {
	ID         string
	Key        string
	Name       string
	Version    int
	Status     DefinitionStatus
	Model      WorkflowModel
	Inputs     []InputParameter
	CreatedBy  string
	CreatedAt  time.Time
	DeployedAt *time.Time
}

// InputParameter 描述启动流程时可传入的业务变量。
type InputParameter struct {
	Key          string
	Name         string
	Type         string
	Required     bool
	DefaultValue any
}

// WorkflowModel 保存流程图结构，包括节点和顺序流。
type WorkflowModel struct {
	Nodes         map[string]Node
	SequenceFlows []SequenceFlow
}

// Node 表示流程图中的一个节点。
type Node struct {
	ID                  string
	Type                NodeType
	Name                string
	InputMappings       []InputMapping
	OutputVariables     []string
	Endpoint            string
	AutomationServiceID string
	AutomationTaskKey   string
	AssigneeExpression  string
	TimeoutSeconds      int
	RetryPolicy         RetryPolicy
}

// InputMapping 描述自动化任务入参和流程变量表达式之间的映射关系。
type InputMapping struct {
	Parameter  string
	Expression string
}

// RetryPolicy 描述自动化任务调度失败后的重试策略。
type RetryPolicy struct {
	MaxAttempts    int
	BackoffSeconds int
}

// SequenceFlow 表示节点之间的一条有向连线。
type SequenceFlow struct {
	ID        string
	Name      string
	SourceRef string
	TargetRef string
	Condition string
	Default   bool
}

// ProcessInstance 表示一次流程运行实例。
type ProcessInstance struct {
	ID                string
	DefinitionID      string
	DefinitionKey     string
	DefinitionVersion int
	BusinessKey       string
	TenantID          string
	Status            InstanceStatus
	StartTime         time.Time
	EndTime           *time.Time
	Starter           string
	EndNodeID         string
	FailReason        string
	Version           int
}

// Execution 表示流程实例中的一个执行分支。
type Execution struct {
	ID         string
	InstanceID string
	ParentID   string
	NodeID     string
	Status     ExecutionStatus
	JoinKey    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ActivityInstance 表示某个节点在流程实例中的一次执行记录。
type ActivityInstance struct {
	ID           string
	InstanceID   string
	ExecutionID  string
	NodeID       string
	NodeType     NodeType
	NodeName     string
	Status       ActivityStatus
	StartTime    time.Time
	EndTime      *time.Time
	RetryCount   int
	ErrorMessage string
}

// Task 表示流程等待外部完成的任务，包括人工待办和自动化外部任务。
type Task struct {
	ID                 string
	InstanceID         string
	ActivityInstanceID string
	NodeID             string
	Title              string
	Type               TaskType
	Status             TaskStatus
	Assignee           string
	Owner              string
	VariableSnapshot   map[string]any
	Action             string
	Comment            string
	CompletedBy        string
	DelegatedFrom      string
	DispatchURL        string
	CallbackToken      string
	TimeoutAt          *time.Time
	ClaimedAt          *time.Time
	RetryCount         int
	CreatedAt          time.Time
	CompletedAt        *time.Time
}

// Variable 表示流程实例当前可读取的变量值。
type Variable struct {
	InstanceID      string
	Name            string
	Type            string
	Value           any
	Scope           VariableScope
	UpdatedByNodeID string
	UpdatedAt       time.Time
}

// VariableHistory 记录流程变量的历史变更。
type VariableHistory struct {
	ID           string
	InstanceID   string
	Name         string
	OldValue     any
	NewValue     any
	SourceNodeID string
	SourceTaskID string
	CreatedAt    time.Time
}

// OutboxEvent 表示需要异步投递的工作流事件。
type OutboxEvent struct {
	ID          string
	EventType   string
	AggregateID string
	Payload     map[string]any
	Status      OutboxStatus
	RetryCount  int
	NextRetryAt *time.Time
	CreatedAt   time.Time
}

// AuditEvent 表示流程运行过程中的审计事件。
type AuditEvent struct {
	ID         string
	InstanceID string
	EventType  AuditEventType
	NodeID     string
	TaskID     string
	Payload    map[string]any
	CreatedAt  time.Time
}
