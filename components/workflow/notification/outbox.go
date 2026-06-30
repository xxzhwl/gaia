// Package notification 提供工作流通知 outbox 的可插拔投递能力。
package notification

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
)

// RuntimePort 定义通知投递器需要调用的运行时端口。
type RuntimePort interface {
	ClaimNotificationOutboxBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	MarkOutboxSent(ctx context.Context, eventID string) error
	MarkOutboxFailed(ctx context.Context, req engine.MarkOutboxFailedRequest) error
	MarkOutboxDead(ctx context.Context, eventID string) error
}

// Message 表示通知投递器交给上游通知系统的消息。
type Message struct {
	EventID          string
	NotificationType string
	TaskID           string
	InstanceID       string
	ActivityID       string
	NodeID           string
	Title            string
	TaskType         string
	Status           string
	Assignee         string
	Owner            string
	Action           string
	CompletedBy      string
	Payload          map[string]any
}

// Sender 定义真正发送通知的上游适配器。
type Sender interface {
	Send(ctx context.Context, message Message) error
}

// SenderFunc 允许用函数快速实现 Sender。
type SenderFunc func(ctx context.Context, message Message) error

// Send 调用底层函数发送通知。
func (fn SenderFunc) Send(ctx context.Context, message Message) error {
	return fn(ctx, message)
}

// Dispatcher 从通知 outbox 中领取事件并交给 Sender 投递。
type Dispatcher struct {
	runtime      RuntimePort
	sender       Sender
	retryDelay   time.Duration
	maxAttempts  int
	pollInterval time.Duration
	batchSize    int
	running      atomic.Bool
}

// Option 用于配置通知投递器。
type Option func(*Dispatcher)

// WithRetryDelay 设置投递失败后的重试间隔。
func WithRetryDelay(delay time.Duration) Option {
	return func(d *Dispatcher) {
		if delay > 0 {
			d.retryDelay = delay
		}
	}
}

// WithMaxAttempts 设置通知 outbox 事件最大投递次数。
func WithMaxAttempts(maxAttempts int) Option {
	return func(d *Dispatcher) {
		if maxAttempts > 0 {
			d.maxAttempts = maxAttempts
		}
	}
}

// WithPollInterval 设置通知投递器轮询间隔。
func WithPollInterval(interval time.Duration) Option {
	return func(d *Dispatcher) {
		if interval > 0 {
			d.pollInterval = interval
		}
	}
}

// WithBatchSize 设置每次领取通知 outbox 的数量。
func WithBatchSize(batchSize int) Option {
	return func(d *Dispatcher) {
		if batchSize > 0 {
			d.batchSize = batchSize
		}
	}
}

// New 创建通知 outbox 投递器。
func New(runtime RuntimePort, sender Sender, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		runtime:      runtime,
		sender:       sender,
		retryDelay:   10 * time.Second,
		maxAttempts:  3,
		pollInterval: 2 * time.Second,
		batchSize:    20,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// DispatchPending 立即领取并投递一批通知 outbox 事件。
func (d *Dispatcher) DispatchPending(ctx context.Context, limit int) (int, error) {
	if d == nil || d.runtime == nil {
		return 0, fmt.Errorf("notification dispatcher runtime is nil")
	}
	if d.sender == nil {
		return 0, fmt.Errorf("notification dispatcher sender is nil")
	}
	events, err := d.runtime.ClaimNotificationOutboxBatch(ctx, limit)
	if err != nil {
		return 0, err
	}
	var firstErr error
	success := 0
	for _, event := range events {
		message := buildMessage(event)
		if err := d.sender.Send(ctx, message); err != nil {
			_ = d.markFailedOrDead(ctx, event)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := d.runtime.MarkOutboxSent(ctx, event.ID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}
	return success, firstErr
}

// Run 按轮询间隔持续投递通知 outbox，直到上下文结束或投递失败。
func (d *Dispatcher) Run(ctx context.Context) error {
	if !d.running.CompareAndSwap(false, true) {
		return fmt.Errorf("notification dispatcher is already running")
	}
	defer d.running.Store(false)

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		if _, err := d.DispatchPending(ctx, d.batchSize); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) markFailedOrDead(ctx context.Context, event domain.OutboxEvent) error {
	if d.maxAttempts > 0 && event.RetryCount+1 >= d.maxAttempts {
		return d.runtime.MarkOutboxDead(ctx, event.ID)
	}
	nextRetryAt := time.Now().UTC().Add(d.retryDelay)
	return d.runtime.MarkOutboxFailed(ctx, engine.MarkOutboxFailedRequest{
		EventID:     event.ID,
		NextRetryAt: &nextRetryAt,
	})
}

func buildMessage(event domain.OutboxEvent) Message {
	return Message{
		EventID:          event.ID,
		NotificationType: stringFromPayload(event.Payload, "notificationType"),
		TaskID:           stringFromPayload(event.Payload, "taskId"),
		InstanceID:       stringFromPayload(event.Payload, "instanceId"),
		ActivityID:       stringFromPayload(event.Payload, "activityId"),
		NodeID:           stringFromPayload(event.Payload, "nodeId"),
		Title:            stringFromPayload(event.Payload, "title"),
		TaskType:         stringFromPayload(event.Payload, "type"),
		Status:           stringFromPayload(event.Payload, "status"),
		Assignee:         stringFromPayload(event.Payload, "assignee"),
		Owner:            stringFromPayload(event.Payload, "owner"),
		Action:           stringFromPayload(event.Payload, "action"),
		CompletedBy:      stringFromPayload(event.Payload, "completedBy"),
		Payload:          event.Payload,
	}
}

func stringFromPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
