package engine

import (
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

func taskOpen(status domain.TaskStatus) bool {
	return status == domain.TaskStatusCreated ||
		status == domain.TaskStatusWaiting ||
		status == domain.TaskStatusDispatched ||
		status == domain.TaskStatusClaimed
}

func taskEventPayload(task domain.Task) map[string]any {
	payload := map[string]any{
		"taskId":      task.ID,
		"instanceId":  task.InstanceID,
		"activityId":  task.ActivityInstanceID,
		"nodeId":      task.NodeID,
		"title":       task.Title,
		"type":        string(task.Type),
		"status":      string(task.Status),
		"assignee":    task.Assignee,
		"owner":       task.Owner,
		"action":      task.Action,
		"completedBy": task.CompletedBy,
		"createdAt":   task.CreatedAt,
	}
	if task.TimeoutAt != nil {
		payload["timeoutAt"] = *task.TimeoutAt
	}
	if task.CompletedAt != nil {
		payload["completedAt"] = *task.CompletedAt
	}
	return payload
}

func notificationPayload(notificationType string, task domain.Task) map[string]any {
	payload := taskEventPayload(task)
	payload["notificationType"] = notificationType
	payload["event"] = notificationType
	return payload
}

func (r *Runtime) createTaskLifecycleOutbox(eventType string, task domain.Task) domain.OutboxEvent {
	event := domain.OutboxEvent{
		ID:          r.ids.Next("outbox"),
		EventType:   eventType,
		AggregateID: task.ID,
		Payload:     taskEventPayload(task),
		Status:      domain.OutboxStatusNew,
		CreatedAt:   r.clock.Now(),
	}
	r.outbox[event.ID] = event
	return event
}

func (r *Runtime) createNotificationOutbox(notificationType string, task domain.Task) domain.OutboxEvent {
	event := domain.OutboxEvent{
		ID:          r.ids.Next("outbox"),
		EventType:   string(domain.OutboxEventNotificationRequested),
		AggregateID: task.ID,
		Payload:     notificationPayload(notificationType, task),
		Status:      domain.OutboxStatusNew,
		CreatedAt:   r.clock.Now(),
	}
	r.outbox[event.ID] = event
	return event
}

func (r *Runtime) outboxExists(eventType, aggregateID string) bool {
	for _, event := range r.outbox {
		if event.EventType == eventType && event.AggregateID == aggregateID {
			return true
		}
	}
	return false
}

func newTaskLifecycleOutbox(ids IDGenerator, now time.Time, eventType string, task domain.Task) domain.OutboxEvent {
	return domain.OutboxEvent{
		ID:          ids.Next("outbox"),
		EventType:   eventType,
		AggregateID: task.ID,
		Payload:     taskEventPayload(task),
		Status:      domain.OutboxStatusNew,
		CreatedAt:   now,
	}
}

func newNotificationOutbox(ids IDGenerator, now time.Time, notificationType string, task domain.Task) domain.OutboxEvent {
	return domain.OutboxEvent{
		ID:          ids.Next("outbox"),
		EventType:   string(domain.OutboxEventNotificationRequested),
		AggregateID: task.ID,
		Payload:     notificationPayload(notificationType, task),
		Status:      domain.OutboxStatusNew,
		CreatedAt:   now,
	}
}
