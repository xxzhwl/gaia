package domain

import (
	"testing"
	"time"
)

func TestTypedOutboxEventPayloadRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	event, err := NewOutboxEvent("outbox_1", string(OutboxEventExternalTaskDispatch), "task_1", ExternalTaskDispatchPayload{
		TaskID:            "task_1",
		ProcessInstanceID: "inst_1",
		NodeID:            "node_1",
		AutomationTaskKey: "normalize",
		DispatchURL:       "https://worker/tasks",
		Variables:         map[string]any{"orderId": "O-1"},
		CallbackToken:     "token",
		DispatchMode:      "ASYNC",
	}, OutboxStatusNew, createdAt)
	if err != nil {
		t.Fatalf("NewOutboxEvent() error = %v", err)
	}
	if event.Payload["taskId"] != "task_1" || event.Payload["dispatchMode"] != "ASYNC" {
		t.Fatalf("payload not encoded as expected: %#v", event.Payload)
	}

	var decoded ExternalTaskDispatchPayload
	if err := event.DecodePayload(&decoded); err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	if decoded.TaskID != "task_1" || decoded.Variables["orderId"] != "O-1" {
		t.Fatalf("payload did not round-trip: %#v", decoded)
	}
}
