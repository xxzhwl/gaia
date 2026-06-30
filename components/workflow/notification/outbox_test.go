package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
)

type fakeRuntime struct {
	events []domain.OutboxEvent
	sent   []string
	failed []engine.MarkOutboxFailedRequest
	dead   []string
}

func (r *fakeRuntime) ClaimNotificationOutboxBatch(_ context.Context, limit int) ([]domain.OutboxEvent, error) {
	if limit <= 0 || limit > len(r.events) {
		limit = len(r.events)
	}
	return r.events[:limit], nil
}

func (r *fakeRuntime) MarkOutboxSent(_ context.Context, eventID string) error {
	r.sent = append(r.sent, eventID)
	return nil
}

func (r *fakeRuntime) MarkOutboxFailed(_ context.Context, req engine.MarkOutboxFailedRequest) error {
	r.failed = append(r.failed, req)
	return nil
}

func (r *fakeRuntime) MarkOutboxDead(_ context.Context, eventID string) error {
	r.dead = append(r.dead, eventID)
	return nil
}

func TestDispatcherSendsNotificationOutbox(t *testing.T) {
	runtime := &fakeRuntime{events: []domain.OutboxEvent{notificationEvent("outbox_1", 0)}}
	var got Message
	dispatcher := New(runtime, SenderFunc(func(_ context.Context, message Message) error {
		got = message
		return nil
	}))

	count, err := dispatcher.DispatchPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("dispatch pending: %v", err)
	}
	if count != 1 || len(runtime.sent) != 1 || runtime.sent[0] != "outbox_1" {
		t.Fatalf("unexpected dispatch result: count=%d sent=%#v", count, runtime.sent)
	}
	if got.NotificationType != "task_created" || got.TaskID != "task_1" || got.Title != "审批" {
		t.Fatalf("message fields not mapped: %#v", got)
	}
}

func TestDispatcherMarksFailedOrDead(t *testing.T) {
	sendErr := errors.New("send failed")
	runtime := &fakeRuntime{events: []domain.OutboxEvent{notificationEvent("outbox_failed", 0)}}
	dispatcher := New(runtime, SenderFunc(func(context.Context, Message) error {
		return sendErr
	}), WithRetryDelay(time.Millisecond))

	count, err := dispatcher.DispatchPending(context.Background(), 10)
	if !errors.Is(err, sendErr) || count != 0 || len(runtime.failed) != 1 || runtime.failed[0].NextRetryAt == nil {
		t.Fatalf("expected failed notification outbox, count=%d err=%v failed=%#v", count, err, runtime.failed)
	}

	runtime = &fakeRuntime{events: []domain.OutboxEvent{notificationEvent("outbox_dead", 2)}}
	dispatcher = New(runtime, SenderFunc(func(context.Context, Message) error {
		return sendErr
	}), WithMaxAttempts(3))
	count, err = dispatcher.DispatchPending(context.Background(), 10)
	if !errors.Is(err, sendErr) || count != 0 || len(runtime.dead) != 1 || runtime.dead[0] != "outbox_dead" {
		t.Fatalf("expected dead notification outbox, count=%d err=%v dead=%#v", count, err, runtime.dead)
	}
}

func notificationEvent(id string, retryCount int) domain.OutboxEvent {
	return domain.OutboxEvent{
		ID:          id,
		EventType:   string(domain.OutboxEventNotificationRequested),
		AggregateID: "task_1",
		Status:      domain.OutboxStatusProcessing,
		RetryCount:  retryCount,
		Payload: map[string]any{
			"notificationType": "task_created",
			"taskId":           "task_1",
			"instanceId":       "pi_1",
			"title":            "审批",
			"status":           string(domain.TaskStatusWaiting),
		},
		CreatedAt: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC),
	}
}
