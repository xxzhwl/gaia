package account

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
	"gorm.io/gorm"
)

// 标准 outbox 事件主题。
const (
	EventUserRegistered    = "account.user.registered"
	EventUserLoggedIn      = "account.user.logged_in"
	EventUserLoggedOut     = "account.user.logged_out"
	EventPasswordChanged   = "account.user.password_changed"
	EventMFASetup          = "account.user.mfa_setup"
	EventMFADisabled       = "account.user.mfa_disabled"
	EventOAuthBound        = "account.user.oauth_bound"
	EventOAuthUnbound      = "account.user.oauth_unbound"
	EventUserDeleted       = "account.user.deleted"
	EventUserLocked        = "account.user.locked"
	EventRoleAssigned      = "account.role.assigned"
	EventPermissionChanged = "account.permission.changed"
)

const (
	outboxPending  = "pending"
	outboxSent     = "sent"
	outboxIgnored  = "ignored"
	outboxFailed   = "failed"
	outboxMaxRetry = 5
)

// OutboxEvent 持有业务事务提交后需要发布的事件数据。
type OutboxEvent struct {
	ID        string    `gorm:"size:36;primaryKey"`
	Topic     string    `gorm:"size:80;not null;index:idx_acct_outbox_status_topic,priority:2;index:idx_acct_outbox_topic_created"`
	Key       string    `gorm:"size:128;not null;default:''"` // optional routing key
	Payload   string    `gorm:"type:json;not null"`
	Status    string    `gorm:"size:20;not null;default:pending;index:idx_acct_outbox_status_topic,priority:1"`
	Retry     int       `gorm:"not null;default:0"`
	LastError string    `gorm:"size:255"`
	CreatedAt time.Time `gorm:"autoCreateTime;index:idx_acct_outbox_topic_created,priority:1"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (OutboxEvent) TableName() string { return "acct_outbox_events" }

// EventSubscriber 接收已发布的 outbox 事件。
// 实现必须是幂等的——发布者可能多次投递同一事件。
type EventSubscriber interface {
	Handle(ctx context.Context, topic string, payload []byte) error
}

// EventSubscriberFunc 将函数适配为 EventSubscriber 接口。
type EventSubscriberFunc func(ctx context.Context, topic string, payload []byte) error

func (f EventSubscriberFunc) Handle(ctx context.Context, topic string, payload []byte) error {
	return f(ctx, topic, payload)
}

// emitOutbox writes an event to the outbox table within the current transaction.
func emitOutbox(tx *gorm.DB, topic, key string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	return tx.Create(&OutboxEvent{
		ID:        newID(),
		Topic:     topic,
		Key:       key,
		Payload:   string(data),
		Status:    outboxPending,
		CreatedAt: time.Now(),
	}).Error
}

// StartPublisher 启动后台协程，轮询 outbox 表并将待处理事件投递给注册的订阅者。
func (m *Manager) StartPublisher(ctx context.Context) {
	if len(m.cfg.EventSubscribers) == 0 {
		gaia.InfoF("[account] no event subscribers registered, outbox publisher disabled")
		return
	}
	go m.publishLoop(ctx)
}

func (m *Manager) publishLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	gaia.InfoF("[account] outbox publisher started with %d subscribers", len(m.cfg.EventSubscribers))

	for {
		select {
		case <-ctx.Done():
			gaia.InfoF("[account] outbox publisher stopped")
			return
		case <-ticker.C:
			m.publishBatch(ctx)
		}
	}
}

func (m *Manager) publishBatch(ctx context.Context) {
	var events []OutboxEvent
	if err := m.db.WithContext(ctx).Where("status = ? AND retry < ?", outboxPending, outboxMaxRetry).
		Order("created_at ASC").
		Limit(50).
		Find(&events).Error; err != nil {
		gaia.WarnF("[account] outbox poll error: %v", err)
		return
	}

	for _, event := range events {
		m.deliverOne(ctx, &event)
	}
}

func (m *Manager) deliverOne(ctx context.Context, event *OutboxEvent) {
	subscriber, ok := m.cfg.EventSubscribers[event.Topic]
	if !ok {
		// No subscriber for this topic — mark as sent to avoid infinite polling
		_ = m.db.WithContext(ctx).Model(event).Update("status", outboxIgnored).Error
		return
	}

	err := subscriber.Handle(ctx, event.Topic, []byte(event.Payload))
	if err != nil {
		retry := event.Retry + 1
		updates := map[string]any{
			"retry":      retry,
			"last_error": err.Error(),
		}
		if retry >= outboxMaxRetry {
			updates["status"] = outboxFailed
		}
		_ = m.db.WithContext(ctx).Model(event).Updates(updates).Error
		gaia.WarnF("[account] outbox delivery failed: topic=%s id=%s retry=%d err=%v",
			event.Topic, event.ID, retry, err)
		return
	}

	_ = m.db.WithContext(ctx).Model(event).Update("status", outboxSent).Error
}
