package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

// BindingStore 保存 workflow 外部任务与 asynctask 任务之间的关联关系。
//
// 生产环境可直接传入 persistence/gormstore.AsyncTaskBindingStore（其方法签名与本接口
// 完全一致，可作为实现直接使用）；测试/纯内存场景使用 MemoryBindingStore。
type BindingStore interface {
	// Save 写入一条绑定记录（下发阶段调用，必须先于任务入队）。
	Save(ctx context.Context, binding domain.AsyncTaskBinding) error
	// GetByAsyncTaskID 按 asynctask 任务 ID 查询绑定。
	GetByAsyncTaskID(ctx context.Context, asyncTaskID int64) (domain.AsyncTaskBinding, error)
	// MarkTaskStatus 更新 asynctask 侧执行状态。
	MarkTaskStatus(ctx context.Context, asyncTaskID int64, status domain.AsyncTaskBindingStatus, lastError string) error
	// MarkCallback 更新 workflow 回调投递状态。
	MarkCallback(ctx context.Context, asyncTaskID int64, status domain.AsyncTaskCallbackStatus, lastError string) error
}

// MemoryBindingStore 是进程内 binding store，适合测试和纯内存模式。
type MemoryBindingStore struct {
	mu          sync.RWMutex
	byAsyncTask map[int64]domain.AsyncTaskBinding
}

// NewMemoryBindingStore 创建内存 binding store。
func NewMemoryBindingStore() *MemoryBindingStore {
	return &MemoryBindingStore{byAsyncTask: map[int64]domain.AsyncTaskBinding{}}
}

// Save 写入绑定。
func (s *MemoryBindingStore) Save(_ context.Context, binding domain.AsyncTaskBinding) error {
	if binding.AsyncTaskID == 0 {
		return fmt.Errorf("async task id is required")
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byAsyncTask[binding.AsyncTaskID] = binding
	return nil
}

// GetByAsyncTaskID 查询绑定。
func (s *MemoryBindingStore) GetByAsyncTaskID(_ context.Context, asyncTaskID int64) (domain.AsyncTaskBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.byAsyncTask[asyncTaskID]
	if !ok {
		return domain.AsyncTaskBinding{}, fmt.Errorf("workflow async task binding %d not found", asyncTaskID)
	}
	return binding, nil
}

// MarkTaskStatus 更新执行状态。
func (s *MemoryBindingStore) MarkTaskStatus(_ context.Context, asyncTaskID int64, status domain.AsyncTaskBindingStatus, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.byAsyncTask[asyncTaskID]
	if !ok {
		return fmt.Errorf("workflow async task binding %d not found", asyncTaskID)
	}
	binding.Status = status
	binding.LastError = lastError
	binding.UpdatedAt = time.Now().UTC()
	s.byAsyncTask[asyncTaskID] = binding
	return nil
}

// MarkCallback 更新 callback 状态。
func (s *MemoryBindingStore) MarkCallback(_ context.Context, asyncTaskID int64, status domain.AsyncTaskCallbackStatus, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.byAsyncTask[asyncTaskID]
	if !ok {
		return fmt.Errorf("workflow async task binding %d not found", asyncTaskID)
	}
	binding.CallbackStatus = status
	binding.LastError = lastError
	binding.UpdatedAt = time.Now().UTC()
	s.byAsyncTask[asyncTaskID] = binding
	return nil
}

var _ BindingStore = (*MemoryBindingStore)(nil)
