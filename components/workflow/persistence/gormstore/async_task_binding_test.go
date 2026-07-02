package gormstore

import (
	"context"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

func TestAsyncTaskBindingStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	store := NewAsyncTaskBindingStore(db)
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)

	err := store.Save(ctx, domain.AsyncTaskBinding{
		AsyncTaskID:         1001,
		WorkflowTaskID:      "task-1",
		ProcessInstanceID:   "inst-1",
		DefinitionID:        "def-1",
		DefinitionKey:       "order_flow",
		DefinitionName:      "Order Flow",
		DefinitionVersion:   2,
		InstanceName:        "ORDER-1",
		NodeID:              "node-1",
		NodeName:            "Normalize",
		AutomationTaskKey:   "normalize_order",
		Theme:               "order-system",
		ServiceName:         "order_worker",
		MethodName:          "Normalize",
		CallbackToken:       "token-1",
		CompleteCallbackURL: "http://workflow/tasks/task-1/complete",
		FailCallbackURL:     "http://workflow/tasks/task-1/fail",
		Status:              domain.AsyncTaskBindingStatusSubmitted,
		CallbackStatus:      domain.AsyncTaskCallbackStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		t.Fatalf("save binding: %v", err)
	}

	loaded, err := store.GetByAsyncTaskID(ctx, 1001)
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if loaded.WorkflowTaskID != "task-1" || loaded.DefinitionVersion != 2 || loaded.ServiceName != "order_worker" {
		t.Fatalf("unexpected binding: %#v", loaded)
	}

	if err := store.MarkTaskStatus(ctx, 1001, domain.AsyncTaskBindingStatusSuccess, ""); err != nil {
		t.Fatalf("mark task status: %v", err)
	}
	if err := store.MarkCallback(ctx, 1001, domain.AsyncTaskCallbackStatusFailed, "callback timeout"); err != nil {
		t.Fatalf("mark callback: %v", err)
	}
	loaded, err = store.GetByAsyncTaskID(ctx, 1001)
	if err != nil {
		t.Fatalf("get updated binding: %v", err)
	}
	if loaded.Status != domain.AsyncTaskBindingStatusSuccess ||
		loaded.CallbackStatus != domain.AsyncTaskCallbackStatusFailed ||
		loaded.LastError != "callback timeout" {
		t.Fatalf("unexpected updated binding: %#v", loaded)
	}
}
