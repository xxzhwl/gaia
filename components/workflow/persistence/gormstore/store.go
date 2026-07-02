package gormstore

import (
	"context"
	"errors"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/persistence"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNotFound 表示仓储记录不存在。
var ErrNotFound = gorm.ErrRecordNotFound

// UnitOfWork 基于 GORM 实现工作流仓储事务边界。
type UnitOfWork struct {
	db *gorm.DB
}

// NewUnitOfWork 创建 GORM 工作单元。
func NewUnitOfWork(db *gorm.DB) *UnitOfWork {
	return &UnitOfWork{db: db}
}

// WithinTx 在事务中执行仓储操作。
func (u *UnitOfWork) WithinTx(ctx context.Context, fn func(ctx context.Context, repos persistence.Repositories) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, newRepositories(tx))
	})
}

// View 返回非事务仓储视图。
func (u *UnitOfWork) View(ctx context.Context) persistence.Repositories {
	return newRepositories(u.db.WithContext(ctx))
}

type repositories struct {
	db *gorm.DB
}

func newRepositories(db *gorm.DB) *repositories {
	return &repositories{db: db}
}

func (r *repositories) Definitions() persistence.DefinitionRepository {
	return &definitionRepo{db: r.db}
}

func (r *repositories) Instances() persistence.InstanceRepository {
	return &instanceRepo{db: r.db}
}

func (r *repositories) Tasks() persistence.TaskRepository {
	return &taskRepo{db: r.db}
}

func (r *repositories) Variables() persistence.VariableRepository {
	return &variableRepo{db: r.db}
}

func (r *repositories) Outbox() persistence.OutboxRepository {
	return &outboxRepo{db: r.db}
}

func (r *repositories) Audit() persistence.AuditRepository {
	return &auditRepo{db: r.db}
}

type definitionRepo struct {
	db *gorm.DB
}

func (r *definitionRepo) Save(ctx context.Context, def domain.ProcessDefinition) error {
	model, err := processDefinitionToModel(def)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

func (r *definitionRepo) Update(ctx context.Context, def domain.ProcessDefinition) error {
	model, err := processDefinitionToModel(def)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&ProcessDefinitionModel{}).
		Where("id = ? AND edit_version = ?", def.ID, def.EditVersion-1).
		Updates(map[string]any{
			"definition_key": model.Key,
			"name":           model.Name,
			"version":        model.Version,
			"status":         model.Status,
			"model_json":     model.ModelJSON,
			"inputs_json":    model.InputsJSON,
			"created_by":     model.CreatedBy,
			"created_at":     model.CreatedAt,
			"deployed_at":    model.DeployedAt,
			"edit_version":   model.EditVersion,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("process definition edit conflict")
	}
	return nil
}

func (r *definitionRepo) SetStatus(ctx context.Context, definitionID string, status domain.DefinitionStatus) error {
	return r.db.WithContext(ctx).
		Model(&ProcessDefinitionModel{}).
		Where("id = ?", definitionID).
		Update("status", string(status)).Error
}

func (r *definitionRepo) Get(ctx context.Context, definitionID string) (domain.ProcessDefinition, error) {
	var model ProcessDefinitionModel
	err := r.db.WithContext(ctx).Take(&model, "id = ?", definitionID).Error
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromModel(model)
}

func (r *definitionRepo) FindLatestDeployed(ctx context.Context, key string) (domain.ProcessDefinition, error) {
	var model ProcessDefinitionModel
	err := r.db.WithContext(ctx).
		Where("definition_key = ? AND status = ?", key, string(domain.DefinitionStatusDeployed)).
		Order("version DESC").
		First(&model).Error
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromModel(model)
}

func (r *definitionRepo) FindDeployedVersion(ctx context.Context, key string, version int) (domain.ProcessDefinition, error) {
	var model ProcessDefinitionModel
	err := r.db.WithContext(ctx).
		Where("definition_key = ? AND version = ? AND status = ?", key, version, string(domain.DefinitionStatusDeployed)).
		First(&model).Error
	if err != nil {
		return domain.ProcessDefinition{}, err
	}
	return processDefinitionFromModel(model)
}

func (r *definitionRepo) NextVersion(ctx context.Context, key string) (int, error) {
	var maxVersion int
	err := r.db.WithContext(ctx).
		Model(&ProcessDefinitionModel{}).
		Where("definition_key = ?", key).
		Select("COALESCE(MAX(version), 0)").
		Scan(&maxVersion).Error
	if err != nil {
		return 0, err
	}
	return maxVersion + 1, nil
}

func (r *definitionRepo) List(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error) {
	page := domain.NormalizePage(filter.PageRequest)
	query := r.db.WithContext(ctx).Model(&ProcessDefinitionModel{})
	if filter.Key != "" {
		query = query.Where("definition_key LIKE ?", "%"+filter.Key+"%")
	}
	if filter.Name != "" {
		query = query.Where("name LIKE ?", "%"+filter.Name+"%")
	}
	if filter.Status != "" {
		query = query.Where("status = ?", string(filter.Status))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return domain.PageResult[domain.ProcessDefinition]{}, err
	}

	var models []ProcessDefinitionModel
	err := query.
		Order("created_at DESC").
		Limit(page.PageSize).
		Offset(domain.PageOffset(page)).
		Find(&models).Error
	if err != nil {
		return domain.PageResult[domain.ProcessDefinition]{}, err
	}

	definitions := make([]domain.ProcessDefinition, 0, len(models))
	for _, model := range models {
		definition, err := processDefinitionFromModel(model)
		if err != nil {
			return domain.PageResult[domain.ProcessDefinition]{}, err
		}
		definitions = append(definitions, definition)
	}
	return domain.PageResult[domain.ProcessDefinition]{List: definitions, Total: total}, nil
}

type instanceRepo struct {
	db *gorm.DB
}

func (r *instanceRepo) Save(ctx context.Context, instance domain.ProcessInstance) error {
	return r.db.WithContext(ctx).Create(processInstanceToModel(instance)).Error
}

func (r *instanceRepo) Get(ctx context.Context, instanceID string) (domain.ProcessInstance, error) {
	var model ProcessInstanceModel
	if err := r.db.WithContext(ctx).Take(&model, "id = ?", instanceID).Error; err != nil {
		return domain.ProcessInstance{}, err
	}
	return processInstanceFromModel(model), nil
}

func (r *instanceRepo) Update(ctx context.Context, instance domain.ProcessInstance) error {
	model := processInstanceToModel(instance)
	return r.db.WithContext(ctx).Model(&ProcessInstanceModel{}).
		Where("id = ?", instance.ID).
		Updates(map[string]any{
			"definition_id":      model.DefinitionID,
			"definition_key":     model.DefinitionKey,
			"definition_version": model.DefinitionVersion,
			"business_key":       model.BusinessKey,
			"tenant_id":          model.TenantID,
			"status":             model.Status,
			"start_time":         model.StartTime,
			"end_time":           model.EndTime,
			"starter":            model.Starter,
			"end_node_id":        model.EndNodeID,
			"fail_reason":        model.FailReason,
			"version":            model.Version,
		}).Error
}

func (r *instanceRepo) GetExecution(ctx context.Context, executionID string) (domain.Execution, error) {
	var model ExecutionModel
	if err := r.db.WithContext(ctx).Take(&model, "id = ?", executionID).Error; err != nil {
		return domain.Execution{}, err
	}
	return executionFromModel(model), nil
}

func (r *instanceRepo) SaveExecution(ctx context.Context, execution domain.Execution) error {
	model := executionToModel(execution)
	var existing ExecutionModel
	find := r.db.WithContext(ctx).Select("id").Limit(1).Find(&existing, "id = ?", execution.ID)
	if find.Error != nil {
		return find.Error
	}
	if find.RowsAffected == 0 {
		return r.db.WithContext(ctx).Create(model).Error
	}
	result := r.db.WithContext(ctx).Model(&ExecutionModel{}).
		Where("id = ?", execution.ID).
		Updates(map[string]any{
			"instance_id": model.InstanceID,
			"parent_id":   model.ParentID,
			"node_id":     model.NodeID,
			"status":      model.Status,
			"join_key":    model.JoinKey,
			"created_at":  model.CreatedAt,
			"updated_at":  model.UpdatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *instanceRepo) GetActivity(ctx context.Context, activityID string) (domain.ActivityInstance, error) {
	var model ActivityInstanceModel
	if err := r.db.WithContext(ctx).Take(&model, "id = ?", activityID).Error; err != nil {
		return domain.ActivityInstance{}, err
	}
	return activityFromModel(model), nil
}

func (r *instanceRepo) SaveActivity(ctx context.Context, activity domain.ActivityInstance) error {
	model := activityToModel(activity)
	var existing ActivityInstanceModel
	find := r.db.WithContext(ctx).Select("id").Limit(1).Find(&existing, "id = ?", activity.ID)
	if find.Error != nil {
		return find.Error
	}
	if find.RowsAffected == 0 {
		return r.db.WithContext(ctx).Create(model).Error
	}
	result := r.db.WithContext(ctx).Model(&ActivityInstanceModel{}).
		Where("id = ?", activity.ID).
		Updates(map[string]any{
			"instance_id":   model.InstanceID,
			"execution_id":  model.ExecutionID,
			"node_id":       model.NodeID,
			"node_type":     model.NodeType,
			"node_name":     model.NodeName,
			"status":        model.Status,
			"start_time":    model.StartTime,
			"end_time":      model.EndTime,
			"retry_count":   model.RetryCount,
			"error_message": model.ErrorMessage,
		})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (r *instanceRepo) ExecutionsByInstance(ctx context.Context, instanceID string) ([]domain.Execution, error) {
	return r.executionsByInstance(ctx, instanceID, false)
}

func (r *instanceRepo) ExecutionsByInstanceForUpdate(ctx context.Context, instanceID string) ([]domain.Execution, error) {
	return r.executionsByInstance(ctx, instanceID, true)
}

func (r *instanceRepo) executionsByInstance(ctx context.Context, instanceID string, forUpdate bool) ([]domain.Execution, error) {
	var models []ExecutionModel
	query := r.db.WithContext(ctx)
	if forUpdate {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.Where("instance_id = ?", instanceID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]domain.Execution, 0, len(models))
	for _, model := range models {
		result = append(result, executionFromModel(model))
	}
	return result, nil
}

func (r *instanceRepo) List(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error) {
	page := domain.NormalizePage(filter.PageRequest)
	query := r.db.WithContext(ctx).Model(&ProcessInstanceModel{})
	if filter.DefinitionKey != "" {
		query = query.Where("definition_key LIKE ?", "%"+filter.DefinitionKey+"%")
	}
	if filter.Status != "" {
		query = query.Where("status = ?", string(filter.Status))
	}
	if filter.TenantID != "" {
		query = query.Where("tenant_id = ?", filter.TenantID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return domain.PageResult[domain.ProcessInstance]{}, err
	}

	var models []ProcessInstanceModel
	err := query.
		Order("start_time DESC").
		Limit(page.PageSize).
		Offset(domain.PageOffset(page)).
		Find(&models).Error
	if err != nil {
		return domain.PageResult[domain.ProcessInstance]{}, err
	}

	instances := make([]domain.ProcessInstance, 0, len(models))
	for _, model := range models {
		instances = append(instances, processInstanceFromModel(model))
	}
	return domain.PageResult[domain.ProcessInstance]{List: instances, Total: total}, nil
}

type taskRepo struct {
	db *gorm.DB
}

func (r *taskRepo) Save(ctx context.Context, task domain.Task) error {
	model, err := taskToModel(task)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

func (r *taskRepo) Get(ctx context.Context, taskID string) (domain.Task, error) {
	var model TaskModel
	if err := r.db.WithContext(ctx).Take(&model, "id = ?", taskID).Error; err != nil {
		return domain.Task{}, err
	}
	return taskFromModel(model)
}

func (r *taskRepo) Update(ctx context.Context, task domain.Task) error {
	model, err := taskToModel(task)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&TaskModel{}).
		Where("id = ?", task.ID).
		Updates(map[string]any{
			"instance_id":            model.InstanceID,
			"activity_instance_id":   model.ActivityInstanceID,
			"node_id":                model.NodeID,
			"title":                  model.Title,
			"type":                   model.Type,
			"status":                 model.Status,
			"assignee":               model.Assignee,
			"owner":                  model.Owner,
			"variable_snapshot_json": model.VariableSnapshotJSON,
			"action":                 model.Action,
			"comment":                model.Comment,
			"completed_by":           model.CompletedBy,
			"delegated_from":         model.DelegatedFrom,
			"dispatch_url":           model.DispatchURL,
			"callback_token":         model.CallbackToken,
			"timeout_at":             model.TimeoutAt,
			"claimed_at":             model.ClaimedAt,
			"retry_count":            model.RetryCount,
			"created_at":             model.CreatedAt,
			"completed_at":           model.CompletedAt,
		}).Error
}

func (r *taskRepo) CompleteIfOpen(ctx context.Context, task domain.Task, completedAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&TaskModel{}).
		Where("id = ? AND status IN ?", task.ID, []string{
			string(domain.TaskStatusCreated),
			string(domain.TaskStatusWaiting),
			string(domain.TaskStatusDispatched),
			string(domain.TaskStatusClaimed),
		}).
		Updates(map[string]any{
			"status":       string(domain.TaskStatusCompleted),
			"action":       task.Action,
			"comment":      task.Comment,
			"completed_by": task.CompletedBy,
			"completed_at": completedAt,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *taskRepo) ListOpenDue(ctx context.Context, dueAt time.Time, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 100
	}
	var models []TaskModel
	err := r.db.WithContext(ctx).
		Where("timeout_at IS NOT NULL AND timeout_at <= ? AND status IN ?", dueAt, []string{
			string(domain.TaskStatusCreated),
			string(domain.TaskStatusWaiting),
			string(domain.TaskStatusDispatched),
			string(domain.TaskStatusClaimed),
		}).
		Order("timeout_at ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	result := make([]domain.Task, 0, len(models))
	for _, model := range models {
		task, err := taskFromModel(model)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, nil
}

func (r *taskRepo) ListByInstance(ctx context.Context, instanceID string) ([]domain.Task, error) {
	var models []TaskModel
	if err := r.db.WithContext(ctx).Where("instance_id = ?", instanceID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]domain.Task, 0, len(models))
	for _, model := range models {
		task, err := taskFromModel(model)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, nil
}

func (r *taskRepo) List(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error) {
	page := domain.NormalizePage(filter.PageRequest)
	query := r.db.WithContext(ctx).Model(&TaskModel{})
	if filter.TenantID != "" {
		query = query.Joins("JOIN workflow_process_instance ON workflow_process_instance.id = workflow_task.instance_id").
			Where("workflow_process_instance.tenant_id = ?", filter.TenantID)
	}
	if filter.InstanceID != "" {
		query = query.Where("workflow_task.instance_id = ?", filter.InstanceID)
	}
	if filter.Status != "" {
		query = query.Where("workflow_task.status = ?", string(filter.Status))
	}
	if filter.Type != "" {
		query = query.Where("workflow_task.type = ?", string(filter.Type))
	}
	if filter.Assignee != "" {
		query = query.Where("workflow_task.assignee = ?", filter.Assignee)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return domain.PageResult[domain.Task]{}, err
	}

	var models []TaskModel
	err := query.
		Order("workflow_task.created_at DESC").
		Limit(page.PageSize).
		Offset(domain.PageOffset(page)).
		Find(&models).Error
	if err != nil {
		return domain.PageResult[domain.Task]{}, err
	}

	tasks := make([]domain.Task, 0, len(models))
	for _, model := range models {
		task, err := taskFromModel(model)
		if err != nil {
			return domain.PageResult[domain.Task]{}, err
		}
		tasks = append(tasks, task)
	}
	return domain.PageResult[domain.Task]{List: tasks, Total: total}, nil
}

type variableRepo struct {
	db *gorm.DB
}

func (r *variableRepo) UpsertCurrent(ctx context.Context, variable domain.Variable) error {
	model, err := variableCurrentToModel(variable)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "instance_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"type", "value_json", "scope", "updated_by_node_id", "updated_at",
		}),
	}).Create(&model).Error
}

func (r *variableRepo) AppendHistory(ctx context.Context, history domain.VariableHistory) error {
	model, err := variableHistoryToModel(history)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

func (r *variableRepo) CurrentByInstance(ctx context.Context, instanceID string) (map[string]domain.Variable, error) {
	var models []VariableCurrentModel
	if err := r.db.WithContext(ctx).Where("instance_id = ?", instanceID).Find(&models).Error; err != nil {
		return nil, err
	}
	return variablesFromCurrentModels(models)
}

func (r *variableRepo) CurrentByInstanceAndScope(ctx context.Context, instanceID string, scope domain.VariableScope) (map[string]domain.Variable, error) {
	var models []VariableCurrentModel
	if err := r.db.WithContext(ctx).
		Where("instance_id = ? AND scope = ?", instanceID, string(scope)).
		Find(&models).Error; err != nil {
		return nil, err
	}
	return variablesFromCurrentModels(models)
}

func (r *variableRepo) CurrentByNames(ctx context.Context, instanceID string, names []string) (map[string]domain.Variable, error) {
	if len(names) == 0 {
		return map[string]domain.Variable{}, nil
	}
	var models []VariableCurrentModel
	if err := r.db.WithContext(ctx).
		Where("instance_id = ? AND name IN ?", instanceID, names).
		Find(&models).Error; err != nil {
		return nil, err
	}
	return variablesFromCurrentModels(models)
}

func (r *variableRepo) CurrentByNamesAndScope(ctx context.Context, instanceID string, names []string, scope domain.VariableScope) (map[string]domain.Variable, error) {
	if len(names) == 0 {
		return map[string]domain.Variable{}, nil
	}
	var models []VariableCurrentModel
	if err := r.db.WithContext(ctx).
		Where("instance_id = ? AND name IN ? AND scope = ?", instanceID, names, string(scope)).
		Find(&models).Error; err != nil {
		return nil, err
	}
	return variablesFromCurrentModels(models)
}

func (r *variableRepo) DeleteCurrentByNames(ctx context.Context, instanceID string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("instance_id = ? AND name IN ?", instanceID, names).
		Delete(&VariableCurrentModel{}).Error
}

func (r *variableRepo) DeleteCurrentByNamesAndScope(ctx context.Context, instanceID string, names []string, scope domain.VariableScope) error {
	if len(names) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("instance_id = ? AND name IN ? AND scope = ?", instanceID, names, string(scope)).
		Delete(&VariableCurrentModel{}).Error
}

func variablesFromCurrentModels(models []VariableCurrentModel) (map[string]domain.Variable, error) {
	result := make(map[string]domain.Variable, len(models))
	for _, model := range models {
		variable, err := variableCurrentFromModel(model)
		if err != nil {
			return nil, err
		}
		result[variable.Name] = variable
	}
	return result, nil
}

type outboxRepo struct {
	db *gorm.DB
}

func (r *outboxRepo) Save(ctx context.Context, event domain.OutboxEvent) error {
	model, err := outboxToModel(event)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

func (r *outboxRepo) Get(ctx context.Context, eventID string) (domain.OutboxEvent, error) {
	var model OutboxEventModel
	if err := r.db.WithContext(ctx).Take(&model, "id = ?", eventID).Error; err != nil {
		return domain.OutboxEvent{}, err
	}
	return outboxFromModel(model)
}

func (r *outboxRepo) Exists(ctx context.Context, eventType, aggregateID string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&OutboxEventModel{}).
		Where("event_type = ? AND aggregate_id = ?", eventType, aggregateID).
		Count(&count).Error
	return count > 0, err
}

func (r *outboxRepo) ClaimBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	return r.ClaimBatchByType(ctx, string(domain.OutboxEventExternalTaskDispatch), limit)
}

func (r *outboxRepo) ClaimBatchByType(ctx context.Context, eventType string, limit int) ([]domain.OutboxEvent, error) {
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC()
	var models []OutboxEventModel
	err := r.db.WithContext(ctx).
		Where("event_type = ? AND status IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
			eventType,
			[]string{string(domain.OutboxStatusNew), string(domain.OutboxStatusFailed)}, now).
		Order("created_at ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, err
	}
	events := make([]domain.OutboxEvent, 0, len(models))
	for _, model := range models {
		update := r.db.WithContext(ctx).
			Model(&OutboxEventModel{}).
			Where("id = ? AND event_type = ? AND status IN ? AND (next_retry_at IS NULL OR next_retry_at <= ?)", model.ID,
				eventType, []string{
					string(domain.OutboxStatusNew),
					string(domain.OutboxStatusFailed),
				}, now).
			Update("status", string(domain.OutboxStatusProcessing))
		if update.Error != nil {
			return nil, update.Error
		}
		if update.RowsAffected != 1 {
			continue
		}
		model.Status = string(domain.OutboxStatusProcessing)
		event, err := outboxFromModel(model)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (r *outboxRepo) MarkSent(ctx context.Context, eventID string) error {
	return r.db.WithContext(ctx).Model(&OutboxEventModel{}).
		Where("id = ?", eventID).
		Updates(map[string]any{"status": string(domain.OutboxStatusSent)}).Error
}

func (r *outboxRepo) MarkFailed(ctx context.Context, eventID string, nextRetryAt *time.Time) error {
	return r.db.WithContext(ctx).Model(&OutboxEventModel{}).
		Where("id = ?", eventID).
		Updates(map[string]any{
			"status":        string(domain.OutboxStatusFailed),
			"retry_count":   gorm.Expr("retry_count + 1"),
			"next_retry_at": nextRetryAt,
		}).Error
}

func (r *outboxRepo) MarkDead(ctx context.Context, eventID string) error {
	return r.db.WithContext(ctx).Model(&OutboxEventModel{}).
		Where("id = ?", eventID).
		Updates(map[string]any{
			"status":      string(domain.OutboxStatusDead),
			"retry_count": gorm.Expr("retry_count + 1"),
		}).Error
}

func (r *outboxRepo) List(ctx context.Context, filter domain.OutboxListFilter) (domain.PageResult[domain.OutboxEvent], error) {
	page := domain.NormalizePage(filter.PageRequest)
	query := r.db.WithContext(ctx).Model(&OutboxEventModel{})
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", string(filter.Status))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return domain.PageResult[domain.OutboxEvent]{}, err
	}
	var models []OutboxEventModel
	if err := query.Order("created_at ASC").
		Offset(domain.PageOffset(page)).Limit(page.PageSize).
		Find(&models).Error; err != nil {
		return domain.PageResult[domain.OutboxEvent]{}, err
	}
	events := make([]domain.OutboxEvent, 0, len(models))
	for _, model := range models {
		event, err := outboxFromModel(model)
		if err != nil {
			return domain.PageResult[domain.OutboxEvent]{}, err
		}
		events = append(events, event)
	}
	return domain.PageResult[domain.OutboxEvent]{List: events, Total: total}, nil
}

func (r *outboxRepo) PurgeProcessed(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	subquery := r.db.WithContext(ctx).
		Model(&OutboxEventModel{}).
		Select("row_id").
		Where("status IN ?", []string{string(domain.OutboxStatusSent), string(domain.OutboxStatusDead)}).
		Order("created_at ASC").
		Limit(limit)
	result := r.db.WithContext(ctx).
		Where("row_id IN (?)", subquery).
		Delete(&OutboxEventModel{})
	return result.RowsAffected, result.Error
}

type auditRepo struct {
	db *gorm.DB
}

func (r *auditRepo) Append(ctx context.Context, event domain.AuditEvent) error {
	model, err := auditToModel(event)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&model).Error
}

func (r *auditRepo) ListByInstance(ctx context.Context, instanceID string) ([]domain.AuditEvent, error) {
	var models []AuditEventModel
	if err := r.db.WithContext(ctx).Where("instance_id = ?", instanceID).Order("created_at ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]domain.AuditEvent, 0, len(models))
	for _, model := range models {
		event, err := auditFromModel(model)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}

// IsNotFound 判断错误是否为记录不存在。
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
