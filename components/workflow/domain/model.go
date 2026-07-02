package domain

import (
	"encoding/json"
	"strings"
	"time"
)

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
	// EditVersion 记录草稿编辑轮次，用于乐观锁并发控制。每次成功 UpdateDefinition 递增。
	// 客户端在 PUT /definitions/{id} 时携带当前已知版本，服务端校验一致后更新。
	EditVersion int
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
	SLAPolicy           SLAPolicy
}

// UnmarshalJSON keeps older persisted workflow definitions readable. Early
// ServiceTask definitions stored InputVariables instead of InputMappings.
func (n *Node) UnmarshalJSON(data []byte) error {
	type nodeAlias Node
	aux := struct {
		nodeAlias
		InputVariables []string
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*n = Node(aux.nodeAlias)
	if len(n.InputMappings) == 0 && len(aux.InputVariables) > 0 {
		n.InputMappings = make([]InputMapping, 0, len(aux.InputVariables))
		for _, name := range aux.InputVariables {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			n.InputMappings = append(n.InputMappings, InputMapping{
				Parameter:  name,
				Expression: "${" + name + "}",
			})
		}
	}
	return nil
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

// SLAPolicy 描述任务超时后的自动处理策略。
type SLAPolicy struct {
	Action    SLAAction
	Operator  string
	Reason    string
	Variables map[string]any
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

// AsyncTaskBinding 记录 workflow 外部任务与 asynctask 任务之间的一次绑定。
type AsyncTaskBinding struct {
	ID                  uint64
	AsyncTaskID         int64
	WorkflowTaskID      string
	ProcessInstanceID   string
	DefinitionID        string
	DefinitionKey       string
	DefinitionName      string
	DefinitionVersion   int
	InstanceName        string
	NodeID              string
	NodeName            string
	AutomationTaskKey   string
	Theme               string
	ServiceName         string
	MethodName          string
	CallbackToken       string
	CompleteCallbackURL string
	FailCallbackURL     string
	Status              AsyncTaskBindingStatus
	CallbackStatus      AsyncTaskCallbackStatus
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
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

// TypedOutboxEvent 是 OutboxEvent 的 Go 侧强类型视图。
//
// 持久化和传输层仍使用 JSON/Struct，因此这里作为构造和消费时的类型安全辅助。
type TypedOutboxEvent[T any] struct {
	ID          string
	EventType   string
	AggregateID string
	Payload     T
	Status      OutboxStatus
	RetryCount  int
	NextRetryAt *time.Time
	CreatedAt   time.Time
}

// ExternalTaskDispatchPayload 是 EXTERNAL_TASK_DISPATCH outbox 的确定型 payload。
type ExternalTaskDispatchPayload struct {
	TaskID              string         `json:"taskId"`
	ProcessInstanceID   string         `json:"processInstanceId"`
	DefinitionID        string         `json:"definitionId,omitempty"`
	DefinitionKey       string         `json:"definitionKey,omitempty"`
	DefinitionName      string         `json:"definitionName,omitempty"`
	DefinitionVersion   int            `json:"definitionVersion,omitempty"`
	InstanceName        string         `json:"instanceName,omitempty"`
	NodeID              string         `json:"nodeId"`
	NodeName            string         `json:"nodeName,omitempty"`
	AutomationTaskKey   string         `json:"automationTaskKey"`
	DispatchURL         string         `json:"dispatchUrl"`
	Variables           map[string]any `json:"variables"`
	CallbackToken       string         `json:"callbackToken"`
	CallbackURL         string         `json:"callbackUrl,omitempty"`
	FailCallbackURL     string         `json:"failCallbackUrl,omitempty"`
	DispatchMode        string         `json:"dispatchMode,omitempty"`
	RetryMaxAttempts    int            `json:"retryMaxAttempts,omitempty"`
	RetryBackoffSeconds int            `json:"retryBackoffSeconds,omitempty"`
}

// NewOutboxEvent 将强类型 payload 编码为当前 OutboxEvent 使用的 map 形态。
func NewOutboxEvent[T any](id, eventType, aggregateID string, payload T, status OutboxStatus, createdAt time.Time) (OutboxEvent, error) {
	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return OutboxEvent{}, err
	}
	return OutboxEvent{
		ID:          id,
		EventType:   eventType,
		AggregateID: aggregateID,
		Payload:     payloadMap,
		Status:      status,
		CreatedAt:   createdAt,
	}, nil
}

// DecodePayload 将 OutboxEvent 的 map payload 解码到确定型结构体。
func (e OutboxEvent) DecodePayload(target any) error {
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func payloadToMap(payload any) (map[string]any, error) {
	if payload == nil {
		return map[string]any{}, nil
	}
	if value, ok := payload.(map[string]any); ok {
		return value, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
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
