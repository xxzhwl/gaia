package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/dispatcher"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/notification"
	"github.com/xxzhwl/gaia/components/workflow/persistence/gormstore"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	// RuntimeModeMemory 表示使用内存运行时，适合测试和本地临时运行。
	RuntimeModeMemory = "memory"
	// RuntimeModeSQLite 表示使用 SQLite 持久化运行时。
	RuntimeModeSQLite = "sqlite"
	// RuntimeModeMySQL 表示使用 MySQL 持久化运行时。
	RuntimeModeMySQL = "mysql"
	// RuntimeModePostgres 表示使用 PostgreSQL 持久化运行时。
	RuntimeModePostgres = "postgres"
)

// Runtime 定义工作流引擎依赖的运行时能力。
type Runtime interface {
	CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	StartProcess(ctx context.Context, req workflowengine.StartProcessRequest) (domain.ProcessInstance, error)
	CompleteTask(ctx context.Context, req workflowengine.CompleteTaskRequest) (domain.ProcessInstance, error)
	ClaimTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error)
	UnclaimTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error)
	TransferTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error)
	DelegateTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error)
	RejectTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.ProcessInstance, error)
	ScanTimeoutTasks(ctx context.Context, req workflowengine.ScanTimeoutTasksRequest) (workflowengine.ScanTimeoutTasksResult, error)
	GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error)
	ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error)
	ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error)
	ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error)
	Tasks(ctx context.Context, instanceID string) ([]domain.Task, error)
	Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error)
	AuditTrail(ctx context.Context, instanceID string) ([]domain.AuditEvent, error)
	OutboxEvents(ctx context.Context) ([]domain.OutboxEvent, error)
}

// DispatcherRuntime 定义调度器需要调用的运行时端口。
type DispatcherRuntime interface {
	dispatcher.RuntimePort
}

// NotificationRuntime 定义通知投递器需要调用的运行时端口。
type NotificationRuntime interface {
	notification.RuntimePort
}

// Engine 是工作流组件的一站式门面。
type Engine struct {
	runtime                Runtime
	dispatcher             *dispatcher.Dispatcher
	notificationDispatcher *notification.Dispatcher
	registry               automation.Registry
}

// Config 表示工作流组件初始化配置。
type Config struct {
	Mode         string
	DSN          string
	SQLitePath   string
	AutoMigrate  bool
	Dispatcher   DispatcherConfig
	Notification NotificationConfig
	Registry     automation.Registry
}

// DispatcherConfig 表示外部任务调度器配置。
type DispatcherConfig struct {
	Enabled         bool
	CallbackBaseURL string
	RetryDelay      time.Duration
	MaxAttempts     int
	PollInterval    time.Duration
	BatchSize       int
}

// NotificationConfig 表示通知 outbox 投递器配置。
type NotificationConfig struct {
	RetryDelay   time.Duration
	MaxAttempts  int
	PollInterval time.Duration
	BatchSize    int
}

// NewEngineByCfg 根据显式配置创建工作流引擎。
func NewEngineByCfg(cfg Config) (*Engine, error) {
	runtime, err := BuildRuntime(cfg)
	if err != nil {
		return nil, err
	}
	registry := cfg.Registry
	if registry == nil {
		registry, err = BuildAutomationRegistry(cfg)
		if err != nil {
			return nil, err
		}
	}
	engine := &Engine{runtime: runtime, registry: registry}
	engine.ConfigureDispatcher(cfg.Dispatcher)
	return engine, nil
}

// BuildAutomationRegistry 根据运行时配置创建自动化服务注册表。
func BuildAutomationRegistry(config Config) (automation.Registry, error) {
	mode := strings.ToLower(strings.TrimSpace(config.Mode))
	if mode == "" || mode == RuntimeModeMemory {
		return automation.DefaultRegistry(), nil
	}
	switch mode {
	case RuntimeModeSQLite, RuntimeModeMySQL, RuntimeModePostgres:
		db, err := OpenDB(config)
		if err != nil {
			return nil, err
		}
		if config.AutoMigrate {
			if err := gormstore.AutoMigrate(db); err != nil {
				return nil, err
			}
		}
		return gormstore.NewAutomationRegistry(db), nil
	default:
		return nil, fmt.Errorf("unsupported workflow registry mode %q", config.Mode)
	}
}

// NewEngine 根据 Gaia 配置 schema 创建工作流引擎。
func NewEngine(schema string) (*Engine, error) {
	return NewEngineByCfg(configFromSchema(schema))
}

// DefaultEngine 根据约定配置创建默认工作流引擎。
func DefaultEngine() (*Engine, error) {
	if hasWorkflowSchema("Framework.Workflow") {
		return NewEngine("Framework.Workflow")
	}
	return NewEngine("Workflow")
}

// NewMemoryEngine 创建只使用内存存储的工作流引擎。
func NewMemoryEngine() *Engine {
	return &Engine{
		runtime:  &memoryRuntimeAdapter{runtime: workflowengine.NewRuntime(nil, nil)},
		registry: automation.DefaultRegistry(),
	}
}

// ConfigureDispatcher 按配置启用或更新外部任务调度器。
func (e *Engine) ConfigureDispatcher(config DispatcherConfig) {
	if !config.Enabled {
		return
	}
	runtime, ok := e.runtime.(DispatcherRuntime)
	if !ok {
		return
	}
	opts := []dispatcher.Option{
		dispatcher.WithCallbackBaseURL(config.CallbackBaseURL),
		dispatcher.WithAutomationRegistry(e.registry),
	}
	if config.RetryDelay > 0 {
		opts = append(opts, dispatcher.WithRetryDelay(config.RetryDelay))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, dispatcher.WithMaxAttempts(config.MaxAttempts))
	}
	if config.PollInterval > 0 {
		opts = append(opts, dispatcher.WithPollInterval(config.PollInterval))
	}
	if config.BatchSize > 0 {
		opts = append(opts, dispatcher.WithBatchSize(config.BatchSize))
	}
	e.dispatcher = dispatcher.NewWithRuntime(runtime, opts...)
}

// ConfigureNotificationDispatcher 按配置启用或更新通知 outbox 投递器。
func (e *Engine) ConfigureNotificationDispatcher(sender notification.Sender, config NotificationConfig) {
	runtime, ok := e.runtime.(NotificationRuntime)
	if !ok || sender == nil {
		return
	}
	opts := []notification.Option{}
	if config.RetryDelay > 0 {
		opts = append(opts, notification.WithRetryDelay(config.RetryDelay))
	}
	if config.MaxAttempts > 0 {
		opts = append(opts, notification.WithMaxAttempts(config.MaxAttempts))
	}
	if config.PollInterval > 0 {
		opts = append(opts, notification.WithPollInterval(config.PollInterval))
	}
	if config.BatchSize > 0 {
		opts = append(opts, notification.WithBatchSize(config.BatchSize))
	}
	e.notificationDispatcher = notification.New(runtime, sender, opts...)
}

// StartDispatcher 异步启动外部任务调度器。
func (e *Engine) StartDispatcher(ctx context.Context) {
	if e.dispatcher == nil {
		return
	}
	go func() {
		if err := e.dispatcher.Run(ctx); err != nil && ctx.Err() == nil {
			gaia.ErrorF("workflow dispatcher stopped with error: %v", err)
		}
	}()
}

// StartNotificationDispatcher 异步启动通知 outbox 投递器。
func (e *Engine) StartNotificationDispatcher(ctx context.Context) {
	if e.notificationDispatcher == nil {
		return
	}
	go func() {
		if err := e.notificationDispatcher.Run(ctx); err != nil && ctx.Err() == nil {
			gaia.ErrorF("workflow notification dispatcher stopped with error: %v", err)
		}
	}()
}

// RegisterAutomationService 注册提供 workflow 自动化任务的服务。
func (e *Engine) RegisterAutomationService(ctx context.Context, service automation.Service) (automation.Service, error) {
	return e.registry.Register(ctx, service)
}

// UnregisterAutomationService 注销自动化服务。
func (e *Engine) UnregisterAutomationService(ctx context.Context, serviceID string) error {
	return e.registry.Unregister(ctx, serviceID)
}

// ListAutomationServices 查询当前已注册的自动化服务。
func (e *Engine) ListAutomationServices(ctx context.Context) ([]automation.Service, error) {
	return e.registry.ListServices(ctx)
}

// ListAutomationTasks 查询当前可用的自动化任务。
func (e *Engine) ListAutomationTasks(ctx context.Context) ([]automation.Task, error) {
	return e.registry.ListTasks(ctx)
}

// GetAutomationTask 查询指定自动化服务中的任务定义。
func (e *Engine) GetAutomationTask(ctx context.Context, serviceID, taskKey string) (automation.Task, error) {
	return e.registry.GetTask(ctx, serviceID, taskKey)
}

// DeployDefinition 部署流程定义，并生成新的可运行版本。
func (e *Engine) DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	return e.runtime.DeployDefinition(ctx, def)
}

// CreateDefinition 创建草稿流程定义。
func (e *Engine) CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	return e.runtime.CreateDefinition(ctx, def)
}

// UpdateDefinition 更新草稿或定义记录的模型内容。
func (e *Engine) UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	return e.runtime.UpdateDefinition(ctx, definitionID, def)
}

// GetDefinition 获取指定流程定义。
func (e *Engine) GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error) {
	return e.runtime.GetDefinition(ctx, definitionID)
}

// StartProcess 启动流程实例。
func (e *Engine) StartProcess(ctx context.Context, req workflowengine.StartProcessRequest) (domain.ProcessInstance, error) {
	return e.runtime.StartProcess(ctx, req)
}

// CompleteTask 完成人工待办或外部任务，并写回输出变量。
func (e *Engine) CompleteTask(ctx context.Context, req workflowengine.CompleteTaskRequest) (domain.ProcessInstance, error) {
	return e.runtime.CompleteTask(ctx, req)
}

// ClaimTask 认领人工待办。
func (e *Engine) ClaimTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return e.runtime.ClaimTask(ctx, req)
}

// UnclaimTask 取消认领人工待办。
func (e *Engine) UnclaimTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return e.runtime.UnclaimTask(ctx, req)
}

// TransferTask 转办人工待办。
func (e *Engine) TransferTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return e.runtime.TransferTask(ctx, req)
}

// DelegateTask 委托人工待办。
func (e *Engine) DelegateTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return e.runtime.DelegateTask(ctx, req)
}

// RejectTask 驳回人工待办，并按流程变量和网关规则推进流程。
func (e *Engine) RejectTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.ProcessInstance, error) {
	return e.runtime.RejectTask(ctx, req)
}

// ScanTimeoutTasks 扫描超时任务并写入 SLA/通知 outbox。
func (e *Engine) ScanTimeoutTasks(ctx context.Context, req workflowengine.ScanTimeoutTasksRequest) (workflowengine.ScanTimeoutTasksResult, error) {
	return e.runtime.ScanTimeoutTasks(ctx, req)
}

// GetInstance 获取流程实例。
func (e *Engine) GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error) {
	return e.runtime.GetInstance(ctx, instanceID)
}

// ListDefinitions 分页查询流程定义。
func (e *Engine) ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error) {
	return e.runtime.ListDefinitions(ctx, filter)
}

// ListInstances 分页查询流程实例。
func (e *Engine) ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error) {
	return e.runtime.ListInstances(ctx, filter)
}

// ListTasks 分页查询任务。
func (e *Engine) ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error) {
	return e.runtime.ListTasks(ctx, filter)
}

// Tasks 查询指定流程实例下的全部任务。
func (e *Engine) Tasks(ctx context.Context, instanceID string) ([]domain.Task, error) {
	return e.runtime.Tasks(ctx, instanceID)
}

// Variables 查询指定流程实例的当前变量快照。
func (e *Engine) Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error) {
	return e.runtime.Variables(ctx, instanceID)
}

// Timeline 查询指定流程实例的审计时间线。
func (e *Engine) Timeline(ctx context.Context, instanceID string) ([]domain.AuditEvent, error) {
	return e.runtime.AuditTrail(ctx, instanceID)
}

// Outbox 查询当前 outbox 事件。
func (e *Engine) Outbox(ctx context.Context) ([]domain.OutboxEvent, error) {
	return e.runtime.OutboxEvents(ctx)
}

// BuildRuntime 根据配置创建内存或持久化运行时。
func BuildRuntime(config Config) (Runtime, error) {
	mode := strings.ToLower(strings.TrimSpace(config.Mode))
	if mode == "" {
		mode = RuntimeModeMemory
	}
	switch mode {
	case RuntimeModeMemory:
		gaia.InfoF("workflow runtime mode: memory")
		return &memoryRuntimeAdapter{runtime: workflowengine.NewRuntime(nil, nil)}, nil
	case RuntimeModeSQLite, RuntimeModeMySQL, RuntimeModePostgres:
		db, err := OpenDB(config)
		if err != nil {
			return nil, err
		}
		if config.AutoMigrate {
			if err := gormstore.AutoMigrate(db); err != nil {
				return nil, err
			}
		}
		gaia.InfoF("workflow runtime mode: %s", mode)
		return workflowengine.NewPersistentRuntime(gormstore.NewUnitOfWork(db), nil, nil), nil
	default:
		return nil, fmt.Errorf("unsupported workflow runtime mode %q", config.Mode)
	}
}

// OpenDB 根据运行时配置打开持久化数据库连接。
func OpenDB(config Config) (*gorm.DB, error) {
	mode := strings.ToLower(strings.TrimSpace(config.Mode))
	switch mode {
	case RuntimeModeSQLite:
		path := config.SQLitePath
		if path == "" {
			path = filepath.Join("data", "gaia-workflow.db")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		return gorm.Open(sqlite.Open(path), &gorm.Config{})
	case RuntimeModeMySQL:
		if strings.TrimSpace(config.DSN) == "" {
			return nil, fmt.Errorf("Workflow.Runtime.DSN or Framework.Mysql is required for mysql runtime")
		}
		return gorm.Open(mysql.Open(config.DSN), &gorm.Config{})
	case RuntimeModePostgres:
		if strings.TrimSpace(config.DSN) == "" {
			return nil, fmt.Errorf("Workflow.Runtime.DSN or Framework.Postgresql is required for postgres runtime")
		}
		return gorm.Open(postgres.Open(config.DSN), &gorm.Config{})
	default:
		return nil, fmt.Errorf("unsupported database runtime %q", config.Mode)
	}
}

func configFromSchema(schema string) Config {
	return Config{
		Mode:         runtimeModeFromSchema(schema),
		DSN:          dsnFromSchema(schema),
		SQLitePath:   confStringWithDefault(schema+".Runtime.SQLitePath", filepath.Join("data", "gaia-workflow.db")),
		AutoMigrate:  gaia.GetSafeConfBoolWithDefault(schema+".Runtime.AutoMigrate", true),
		Dispatcher:   dispatcherConfigFromSchema(schema),
		Notification: notificationConfigFromSchema(schema),
	}
}

func runtimeModeFromSchema(schema string) string {
	mode := confString(schema + ".Runtime.Mode")
	if mode != "" {
		return mode
	}
	switch {
	case confString("Framework.Mysql") != "":
		return RuntimeModeMySQL
	case confString("Framework.Postgresql") != "":
		return RuntimeModePostgres
	default:
		return RuntimeModeMemory
	}
}

func dsnFromSchema(schema string) string {
	dsn := confString(schema + ".Runtime.DSN")
	if dsn != "" {
		return dsn
	}
	switch strings.ToLower(strings.TrimSpace(runtimeModeFromSchema(schema))) {
	case RuntimeModeMySQL:
		return confString("Framework.Mysql")
	case RuntimeModePostgres:
		return confString("Framework.Postgresql")
	default:
		return ""
	}
}

func dispatcherConfigFromSchema(schema string) DispatcherConfig {
	return DispatcherConfig{
		Enabled:         gaia.GetSafeConfBool(schema + ".Dispatcher.Enabled"),
		CallbackBaseURL: confString(schema + ".Dispatcher.CallbackBaseURL"),
		RetryDelay:      gaia.GetSafeConfDurationWithDefault(schema+".Dispatcher.RetryDelay", 10*time.Second),
		MaxAttempts:     gaia.GetSafeConfIntWithDefault(schema+".Dispatcher.MaxAttempts", 3),
		PollInterval:    gaia.GetSafeConfDurationWithDefault(schema+".Dispatcher.PollInterval", 2*time.Second),
		BatchSize:       gaia.GetSafeConfIntWithDefault(schema+".Dispatcher.BatchSize", 20),
	}
}

func notificationConfigFromSchema(schema string) NotificationConfig {
	return NotificationConfig{
		RetryDelay:   gaia.GetSafeConfDurationWithDefault(schema+".Notification.RetryDelay", 10*time.Second),
		MaxAttempts:  gaia.GetSafeConfIntWithDefault(schema+".Notification.MaxAttempts", 3),
		PollInterval: gaia.GetSafeConfDurationWithDefault(schema+".Notification.PollInterval", 2*time.Second),
		BatchSize:    gaia.GetSafeConfIntWithDefault(schema+".Notification.BatchSize", 20),
	}
}

func hasWorkflowSchema(schema string) bool {
	return confString(schema+".Runtime.Mode") != "" ||
		confString(schema+".Runtime.DSN") != "" ||
		confString(schema+".Runtime.SQLitePath") != "" ||
		confString(schema+".Dispatcher.CallbackBaseURL") != "" ||
		confString(schema+".Notification.RetryDelay") != "" ||
		gaia.GetSafeConfBool(schema+".Runtime.AutoMigrate") ||
		gaia.GetSafeConfBool(schema+".Dispatcher.Enabled")
}

func confString(key string) string {
	return strings.TrimSpace(gaia.GetSafeConfString(key))
}

func confStringWithDefault(key, fallback string) string {
	value := confString(key)
	if value == "" {
		return fallback
	}
	return value
}

type memoryRuntimeAdapter struct {
	runtime *workflowengine.Runtime
}

func (m *memoryRuntimeAdapter) DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	return m.runtime.DeployDefinition(ctx, def)
}

func (m *memoryRuntimeAdapter) CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	return m.runtime.CreateDefinition(ctx, def)
}

func (m *memoryRuntimeAdapter) UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error) {
	return m.runtime.UpdateDefinition(ctx, definitionID, def)
}

func (m *memoryRuntimeAdapter) GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error) {
	def, ok := m.runtime.GetDefinition(ctx, definitionID)
	if !ok {
		return domain.ProcessDefinition{}, fmt.Errorf("process definition %s not found", definitionID)
	}
	return def, nil
}

func (m *memoryRuntimeAdapter) StartProcess(ctx context.Context, req workflowengine.StartProcessRequest) (domain.ProcessInstance, error) {
	return m.runtime.StartProcess(ctx, req)
}

func (m *memoryRuntimeAdapter) CompleteTask(ctx context.Context, req workflowengine.CompleteTaskRequest) (domain.ProcessInstance, error) {
	return m.runtime.CompleteTask(ctx, req)
}

func (m *memoryRuntimeAdapter) ClaimTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return m.runtime.ClaimTask(ctx, req)
}

func (m *memoryRuntimeAdapter) UnclaimTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return m.runtime.UnclaimTask(ctx, req)
}

func (m *memoryRuntimeAdapter) TransferTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return m.runtime.TransferTask(ctx, req)
}

func (m *memoryRuntimeAdapter) DelegateTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.Task, error) {
	return m.runtime.DelegateTask(ctx, req)
}

func (m *memoryRuntimeAdapter) RejectTask(ctx context.Context, req workflowengine.TaskOperationRequest) (domain.ProcessInstance, error) {
	return m.runtime.RejectTask(ctx, req)
}

func (m *memoryRuntimeAdapter) ScanTimeoutTasks(ctx context.Context, req workflowengine.ScanTimeoutTasksRequest) (workflowengine.ScanTimeoutTasksResult, error) {
	return m.runtime.ScanTimeoutTasks(ctx, req)
}

func (m *memoryRuntimeAdapter) GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error) {
	instance, ok := m.runtime.GetInstance(instanceID)
	if !ok {
		return domain.ProcessInstance{}, fmt.Errorf("process instance %s not found", instanceID)
	}
	return instance, nil
}

func (m *memoryRuntimeAdapter) ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error) {
	return m.runtime.ListDefinitions(filter), nil
}

func (m *memoryRuntimeAdapter) ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error) {
	return m.runtime.ListInstances(filter), nil
}

func (m *memoryRuntimeAdapter) ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error) {
	return m.runtime.ListTasks(filter), nil
}

func (m *memoryRuntimeAdapter) Tasks(ctx context.Context, instanceID string) ([]domain.Task, error) {
	return m.runtime.Tasks(instanceID), nil
}

func (m *memoryRuntimeAdapter) Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error) {
	return m.runtime.Variables(instanceID), nil
}

func (m *memoryRuntimeAdapter) AuditTrail(ctx context.Context, instanceID string) ([]domain.AuditEvent, error) {
	return m.runtime.AuditTrail(instanceID), nil
}

func (m *memoryRuntimeAdapter) OutboxEvents(ctx context.Context) ([]domain.OutboxEvent, error) {
	return m.runtime.OutboxEvents(), nil
}

func (m *memoryRuntimeAdapter) ClaimOutboxBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	return m.runtime.ClaimOutboxBatch(limit), nil
}

func (m *memoryRuntimeAdapter) ClaimNotificationOutboxBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	return m.runtime.ClaimNotificationOutboxBatch(limit), nil
}

func (m *memoryRuntimeAdapter) MarkOutboxSent(ctx context.Context, eventID string) error {
	return m.runtime.MarkOutboxSent(eventID)
}

func (m *memoryRuntimeAdapter) MarkOutboxFailed(ctx context.Context, req workflowengine.MarkOutboxFailedRequest) error {
	return m.runtime.MarkOutboxFailed(req)
}

func (m *memoryRuntimeAdapter) MarkOutboxDead(ctx context.Context, eventID string) error {
	return m.runtime.MarkOutboxDead(eventID)
}

func (m *memoryRuntimeAdapter) MarkTaskDispatched(ctx context.Context, taskID string) error {
	return m.runtime.MarkTaskDispatched(taskID)
}
