package persistence

import (
	"context"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

// UnitOfWork 抽象事务边界和只读仓储视图。
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
	View(ctx context.Context) Repositories
}

// Repositories 聚合工作流持久化所需的全部仓储。
type Repositories interface {
	Definitions() DefinitionRepository
	Instances() InstanceRepository
	Tasks() TaskRepository
	Variables() VariableRepository
	Outbox() OutboxRepository
	Audit() AuditRepository
}

// DefinitionRepository 定义流程定义仓储接口。
type DefinitionRepository interface {
	Save(ctx context.Context, def domain.ProcessDefinition) error
	Update(ctx context.Context, def domain.ProcessDefinition) error
	Get(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	FindLatestDeployed(ctx context.Context, key string) (domain.ProcessDefinition, error)
	FindDeployedVersion(ctx context.Context, key string, version int) (domain.ProcessDefinition, error)
	NextVersion(ctx context.Context, key string) (int, error)
	List(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error)
}

// InstanceRepository 定义流程实例、执行分支和活动实例仓储接口。
type InstanceRepository interface {
	Save(ctx context.Context, instance domain.ProcessInstance) error
	Get(ctx context.Context, instanceID string) (domain.ProcessInstance, error)
	Update(ctx context.Context, instance domain.ProcessInstance) error
	GetExecution(ctx context.Context, executionID string) (domain.Execution, error)
	SaveExecution(ctx context.Context, execution domain.Execution) error
	GetActivity(ctx context.Context, activityID string) (domain.ActivityInstance, error)
	SaveActivity(ctx context.Context, activity domain.ActivityInstance) error
	ExecutionsByInstance(ctx context.Context, instanceID string) ([]domain.Execution, error)
	List(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error)
}

// TaskRepository 定义任务仓储接口。
type TaskRepository interface {
	Save(ctx context.Context, task domain.Task) error
	Get(ctx context.Context, taskID string) (domain.Task, error)
	Update(ctx context.Context, task domain.Task) error
	CompleteIfOpen(ctx context.Context, task domain.Task, completedAt time.Time) (bool, error)
	ListOpenDue(ctx context.Context, dueAt time.Time, limit int) ([]domain.Task, error)
	ListByInstance(ctx context.Context, instanceID string) ([]domain.Task, error)
	List(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error)
}

// VariableRepository 定义流程变量仓储接口。
type VariableRepository interface {
	UpsertCurrent(ctx context.Context, variable domain.Variable) error
	AppendHistory(ctx context.Context, history domain.VariableHistory) error
	CurrentByInstance(ctx context.Context, instanceID string) (map[string]domain.Variable, error)
}

// OutboxRepository 定义 outbox 事件仓储接口。
type OutboxRepository interface {
	Save(ctx context.Context, event domain.OutboxEvent) error
	Exists(ctx context.Context, eventType, aggregateID string) (bool, error)
	ClaimBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	ClaimBatchByType(ctx context.Context, eventType string, limit int) ([]domain.OutboxEvent, error)
	MarkSent(ctx context.Context, eventID string) error
	MarkFailed(ctx context.Context, eventID string, nextRetryAt *time.Time) error
	MarkDead(ctx context.Context, eventID string) error
	List(ctx context.Context) ([]domain.OutboxEvent, error)
}

// AuditRepository 定义审计事件仓储接口。
type AuditRepository interface {
	Append(ctx context.Context, event domain.AuditEvent) error
	ListByInstance(ctx context.Context, instanceID string) ([]domain.AuditEvent, error)
}
