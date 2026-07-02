package gormstore

import (
	"context"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"gorm.io/gorm"
)

// AsyncTaskBindingStore 持久化 workflow 与 asynctask 的绑定关系。
type AsyncTaskBindingStore struct {
	db *gorm.DB
}

// NewAsyncTaskBindingStore 创建 workflow_async_task_bindings 表仓储。
func NewAsyncTaskBindingStore(db *gorm.DB) *AsyncTaskBindingStore {
	return &AsyncTaskBindingStore{db: db}
}

// Save 写入一条绑定记录。
func (s *AsyncTaskBindingStore) Save(ctx context.Context, binding domain.AsyncTaskBinding) error {
	now := time.Now().UTC()
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	if binding.UpdatedAt.IsZero() {
		binding.UpdatedAt = now
	}
	if binding.Status == "" {
		binding.Status = domain.AsyncTaskBindingStatusSubmitted
	}
	if binding.CallbackStatus == "" {
		binding.CallbackStatus = domain.AsyncTaskCallbackStatusPending
	}
	model := asyncTaskBindingToModel(binding)
	return s.db.WithContext(ctx).Create(&model).Error
}

// GetByAsyncTaskID 按 asynctask 任务 ID 查询绑定。
func (s *AsyncTaskBindingStore) GetByAsyncTaskID(ctx context.Context, asyncTaskID int64) (domain.AsyncTaskBinding, error) {
	var model AsyncTaskBindingModel
	if err := s.db.WithContext(ctx).Take(&model, "async_task_id = ?", asyncTaskID).Error; err != nil {
		return domain.AsyncTaskBinding{}, err
	}
	return asyncTaskBindingFromModel(model), nil
}

// MarkTaskStatus 更新 asynctask 侧执行状态。
func (s *AsyncTaskBindingStore) MarkTaskStatus(ctx context.Context, asyncTaskID int64, status domain.AsyncTaskBindingStatus, lastError string) error {
	return s.db.WithContext(ctx).Model(&AsyncTaskBindingModel{}).
		Where("async_task_id = ?", asyncTaskID).
		Updates(map[string]any{
			"status":     string(status),
			"last_error": lastError,
			"updated_at": time.Now().UTC(),
		}).Error
}

// MarkCallback 更新 workflow callback 投递状态。
func (s *AsyncTaskBindingStore) MarkCallback(ctx context.Context, asyncTaskID int64, status domain.AsyncTaskCallbackStatus, lastError string) error {
	return s.db.WithContext(ctx).Model(&AsyncTaskBindingModel{}).
		Where("async_task_id = ?", asyncTaskID).
		Updates(map[string]any{
			"callback_status": string(status),
			"last_error":      lastError,
			"updated_at":      time.Now().UTC(),
		}).Error
}
