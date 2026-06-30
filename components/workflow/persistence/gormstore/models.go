package gormstore

import (
	"time"

	"gorm.io/gorm"
)

// ProcessDefinitionModel 映射 workflow_definition 表。
type ProcessDefinitionModel struct {
	RowID      uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID         string    `gorm:"uniqueIndex;size:64;not null"`
	Key        string    `gorm:"column:definition_key;uniqueIndex:idx_definition_key_version,priority:1;size:128;not null"`
	Name       string    `gorm:"size:255;not null"`
	Version    int       `gorm:"uniqueIndex:idx_definition_key_version,priority:2;not null"`
	Status     string    `gorm:"index;size:32;not null"`
	ModelJSON  []byte    `gorm:"type:json;not null"`
	InputsJSON []byte    `gorm:"type:json"`
	CreatedBy  string    `gorm:"size:128"`
	CreatedAt  time.Time `gorm:"not null"`
	DeployedAt *time.Time
}

// TableName 返回流程定义表名。
func (ProcessDefinitionModel) TableName() string {
	return "workflow_definition"
}

// ProcessInstanceModel 映射 workflow_process_instance 表。
type ProcessInstanceModel struct {
	RowID             uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID                string    `gorm:"uniqueIndex;size:64;not null"`
	DefinitionID      string    `gorm:"index;size:64;not null"`
	DefinitionKey     string    `gorm:"index;size:128;not null"`
	DefinitionVersion int       `gorm:"not null"`
	BusinessKey       string    `gorm:"index;size:128"`
	TenantID          string    `gorm:"index;size:128"`
	Status            string    `gorm:"index;size:32;not null"`
	StartTime         time.Time `gorm:"not null"`
	EndTime           *time.Time
	Starter           string `gorm:"size:128"`
	EndNodeID         string `gorm:"size:128"`
	FailReason        string `gorm:"type:text"`
	Version           int    `gorm:"not null;default:1"`
}

// TableName 返回流程实例表名。
func (ProcessInstanceModel) TableName() string {
	return "workflow_process_instance"
}

// ExecutionModel 映射 workflow_execution 表。
type ExecutionModel struct {
	RowID      uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID         string    `gorm:"uniqueIndex;size:64;not null"`
	InstanceID string    `gorm:"index;size:64;not null"`
	ParentID   string    `gorm:"size:64"`
	NodeID     string    `gorm:"index;size:128;not null"`
	Status     string    `gorm:"index;size:32;not null"`
	JoinKey    string    `gorm:"size:128"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

// TableName 返回执行分支表名。
func (ExecutionModel) TableName() string {
	return "workflow_execution"
}

// ActivityInstanceModel 映射 workflow_activity_instance 表。
type ActivityInstanceModel struct {
	RowID        uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID           string    `gorm:"uniqueIndex;size:64;not null"`
	InstanceID   string    `gorm:"index;size:64;not null"`
	ExecutionID  string    `gorm:"index;size:64;not null"`
	NodeID       string    `gorm:"index;size:128;not null"`
	NodeType     string    `gorm:"size:64;not null"`
	NodeName     string    `gorm:"size:255"`
	Status       string    `gorm:"index;size:32;not null"`
	StartTime    time.Time `gorm:"not null"`
	EndTime      *time.Time
	RetryCount   int    `gorm:"not null;default:0"`
	ErrorMessage string `gorm:"type:text"`
}

// TableName 返回活动实例表名。
func (ActivityInstanceModel) TableName() string {
	return "workflow_activity_instance"
}

// TaskModel 映射 workflow_task 表。
type TaskModel struct {
	RowID                uint64     `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID                   string     `gorm:"uniqueIndex;size:64;not null"`
	InstanceID           string     `gorm:"index;size:64;not null"`
	ActivityInstanceID   string     `gorm:"index;size:64;not null"`
	NodeID               string     `gorm:"index;size:128;not null"`
	Title                string     `gorm:"size:255"`
	Type                 string     `gorm:"index;size:32;not null"`
	Status               string     `gorm:"index;size:32;not null"`
	Assignee             string     `gorm:"index;size:128"`
	Owner                string     `gorm:"size:128"`
	VariableSnapshotJSON []byte     `gorm:"type:json"`
	Action               string     `gorm:"size:64"`
	Comment              string     `gorm:"type:text"`
	CompletedBy          string     `gorm:"size:128"`
	DelegatedFrom        string     `gorm:"size:128"`
	DispatchURL          string     `gorm:"type:text"`
	CallbackToken        string     `gorm:"size:255"`
	TimeoutAt            *time.Time `gorm:"index"`
	ClaimedAt            *time.Time
	RetryCount           int       `gorm:"not null;default:0"`
	CreatedAt            time.Time `gorm:"not null"`
	CompletedAt          *time.Time
}

// TableName 返回任务表名。
func (TaskModel) TableName() string {
	return "workflow_task"
}

// VariableCurrentModel 映射 workflow_variable_current 表。
type VariableCurrentModel struct {
	RowID           uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	InstanceID      string    `gorm:"uniqueIndex:idx_variable_instance_name,priority:1;size:64;not null"`
	Name            string    `gorm:"uniqueIndex:idx_variable_instance_name,priority:2;size:128;not null"`
	Type            string    `gorm:"size:64;not null"`
	ValueJSON       []byte    `gorm:"type:json"`
	Scope           string    `gorm:"index;size:32;not null"`
	UpdatedByNodeID string    `gorm:"size:128"`
	UpdatedAt       time.Time `gorm:"not null"`
}

// TableName 返回当前变量表名。
func (VariableCurrentModel) TableName() string {
	return "workflow_variable_current"
}

// VariableHistoryModel 映射 workflow_variable_history 表。
type VariableHistoryModel struct {
	RowID        uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID           string    `gorm:"uniqueIndex;size:64;not null"`
	InstanceID   string    `gorm:"index;size:64;not null"`
	Name         string    `gorm:"index;size:128;not null"`
	OldValueJSON []byte    `gorm:"type:json"`
	NewValueJSON []byte    `gorm:"type:json"`
	SourceNodeID string    `gorm:"size:128"`
	SourceTaskID string    `gorm:"size:64"`
	CreatedAt    time.Time `gorm:"not null"`
}

// TableName 返回变量历史表名。
func (VariableHistoryModel) TableName() string {
	return "workflow_variable_history"
}

// OutboxEventModel 映射 workflow_outbox_event 表。
type OutboxEventModel struct {
	RowID       uint64     `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID          string     `gorm:"uniqueIndex;size:64;not null"`
	EventType   string     `gorm:"index;size:64;not null"`
	AggregateID string     `gorm:"index;size:64;not null"`
	PayloadJSON []byte     `gorm:"type:json;not null"`
	Status      string     `gorm:"index;size:32;not null"`
	RetryCount  int        `gorm:"not null;default:0"`
	NextRetryAt *time.Time `gorm:"index"`
	CreatedAt   time.Time  `gorm:"not null"`
}

// TableName 返回 outbox 事件表名。
func (OutboxEventModel) TableName() string {
	return "workflow_outbox_event"
}

// AuditEventModel 映射 workflow_audit_event 表。
type AuditEventModel struct {
	RowID       uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID          string    `gorm:"uniqueIndex;size:64;not null"`
	InstanceID  string    `gorm:"index;size:64;not null"`
	EventType   string    `gorm:"index;size:64;not null"`
	NodeID      string    `gorm:"size:128"`
	TaskID      string    `gorm:"size:64"`
	PayloadJSON []byte    `gorm:"type:json"`
	CreatedAt   time.Time `gorm:"not null"`
}

// TableName 返回审计事件表名。
func (AuditEventModel) TableName() string {
	return "workflow_audit_event"
}

// AutomationServiceModel 映射 workflow_automation_service 表。
type AutomationServiceModel struct {
	RowID        uint64    `gorm:"column:row_id;primaryKey;autoIncrement"`
	ID           string    `gorm:"uniqueIndex;size:128;not null"`
	Name         string    `gorm:"size:255"`
	BaseURL      string    `gorm:"type:text;not null"`
	HealthURL    string    `gorm:"type:text"`
	Protocol     string    `gorm:"index;size:32;not null"`
	Version      string    `gorm:"size:64"`
	TagsJSON     []byte    `gorm:"type:json"`
	RegisteredAt time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"index;not null"`
	TTLSeconds   int       `gorm:"not null;default:0"`
}

// TableName 返回自动化服务表名。
func (AutomationServiceModel) TableName() string {
	return "workflow_automation_service"
}

// AutomationTaskModel 映射 workflow_automation_task 表。
type AutomationTaskModel struct {
	RowID            uint64 `gorm:"column:row_id;primaryKey;autoIncrement"`
	ServiceID        string `gorm:"uniqueIndex:idx_automation_task_service_key,priority:1;size:128;not null"`
	Key              string `gorm:"uniqueIndex:idx_automation_task_service_key,priority:2;size:128;not null"`
	Name             string `gorm:"size:255"`
	Description      string `gorm:"type:text"`
	Method           string `gorm:"size:16"`
	Endpoint         string `gorm:"type:text"`
	InputSchemaJSON  []byte `gorm:"type:json"`
	OutputSchemaJSON []byte `gorm:"type:json"`
}

// TableName 返回自动化任务表名。
func (AutomationTaskModel) TableName() string {
	return "workflow_automation_task"
}

// AutoMigrate 自动迁移工作流组件所需的数据库表。
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&ProcessDefinitionModel{},
		&ProcessInstanceModel{},
		&ExecutionModel{},
		&ActivityInstanceModel{},
		&TaskModel{},
		&VariableCurrentModel{},
		&VariableHistoryModel{},
		&OutboxEventModel{},
		&AuditEventModel{},
		&AutomationServiceModel{},
		&AutomationTaskModel{},
	)
}
