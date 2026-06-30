package dispatcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"
)

func TestDispatcherSendsExternalTaskAndMarksOutboxSent(t *testing.T) {
	var payload map[string]any
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer bizServer.Close()

	rt := startApprovedFlowWaitingExternalTask(t, bizServer.URL)
	d := New(rt, WithCallbackBaseURL("https://workflow.example.com"))

	count, err := d.DispatchPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("dispatch pending: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one dispatched event, got %d", count)
	}
	if payload["callbackUrl"] == "" {
		t.Fatalf("callbackUrl missing from dispatch payload: %#v", payload)
	}
	if payload["dispatchUrl"] != nil {
		t.Fatalf("dispatchUrl should not be sent to business service: %#v", payload)
	}

	outbox := rt.OutboxEvents()
	dispatchEvent := findDispatchOutbox(t, outbox)
	if dispatchEvent.Status != domain.OutboxStatusSent {
		t.Fatalf("expected sent outbox, got %#v", dispatchEvent)
	}
	tasks := rt.Tasks(dispatchEvent.Payload["processInstanceId"].(string))
	if tasks[1].Status != domain.TaskStatusDispatched {
		t.Fatalf("expected external task dispatched, got %#v", tasks[1])
	}
}

func TestDispatcherMarksOutboxFailedForNon2xxResponse(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bizServer.Close()

	rt := startApprovedFlowWaitingExternalTask(t, bizServer.URL)
	d := New(rt, WithRetryDelay(time.Millisecond))

	count, err := d.DispatchPending(context.Background(), 10)
	if err == nil {
		t.Fatal("expected dispatch error")
	}
	if count != 0 {
		t.Fatalf("expected zero successful dispatches, got %d", count)
	}
	outbox := rt.OutboxEvents()
	dispatchEvent := findDispatchOutbox(t, outbox)
	if dispatchEvent.Status != domain.OutboxStatusFailed || dispatchEvent.RetryCount != 1 || dispatchEvent.NextRetryAt == nil {
		t.Fatalf("expected failed outbox with retry metadata, got %#v", dispatchEvent)
	}
}

func TestDispatcherResolvesAutomationTaskReference(t *testing.T) {
	var payload map[string]any
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer bizServer.Close()

	registry := automation.NewMemoryRegistry()
	if _, err := registry.Register(context.Background(), automation.Service{
		ID:      "order-worker",
		Name:    "Order Worker",
		BaseURL: bizServer.URL,
		Tasks: []automation.Task{{
			Key:      "send_contract",
			Name:     "Send Contract",
			Method:   http.MethodPost,
			Endpoint: bizServer.URL,
		}},
	}); err != nil {
		t.Fatalf("register automation service: %v", err)
	}

	rt := startApprovedFlowWaitingExternalTask(t, "gaia://order-worker/send_contract")
	d := New(rt, WithAutomationRegistry(registry))

	count, err := d.DispatchPending(context.Background(), 10)
	if err != nil {
		t.Fatalf("dispatch pending: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one dispatched event, got %d", count)
	}
	if payload["taskId"] == "" {
		t.Fatalf("task payload missing: %#v", payload)
	}
}

func TestDispatcherMarksOutboxDeadAfterMaxAttempts(t *testing.T) {
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bizServer.Close()

	rt := startApprovedFlowWaitingExternalTask(t, bizServer.URL)
	d := New(rt, WithMaxAttempts(1), WithRetryDelay(time.Millisecond))

	count, err := d.DispatchPending(context.Background(), 10)
	if err == nil {
		t.Fatal("expected dispatch error")
	}
	if count != 0 {
		t.Fatalf("expected zero successful dispatches, got %d", count)
	}
	outbox := rt.OutboxEvents()
	dispatchEvent := findDispatchOutbox(t, outbox)
	if dispatchEvent.Status != domain.OutboxStatusDead || dispatchEvent.RetryCount != 1 {
		t.Fatalf("expected dead outbox, got %#v", dispatchEvent)
	}
}

func TestDispatcherRunStopsWhenContextIsCanceled(t *testing.T) {
	payloadCh := make(chan map[string]any, 1)
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		payloadCh <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer bizServer.Close()

	rt := startApprovedFlowWaitingExternalTask(t, bizServer.URL)
	d := New(rt, WithPollInterval(time.Millisecond), WithBatchSize(1))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	select {
	case <-payloadCh:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("dispatcher did not send payload before deadline")
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not stop after cancel")
	}
}

func startApprovedFlowWaitingExternalTask(t *testing.T, serviceEndpoint string) *engine.Runtime {
	t.Helper()
	rt := engine.NewRuntime(nil, nil)
	def, err := rt.DeployDefinition(context.Background(), testfixture.OrderApprovalDefinition(serviceEndpoint))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := rt.StartProcess(context.Background(), engine.StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_2001",
		Starter:       "user_1",
		Variables: map[string]any{
			"orderId": "ORDER_2001",
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	userTask := rt.Tasks(instance.ID)[0]
	if _, err := rt.CompleteTask(context.Background(), engine.CompleteTaskRequest{
		TaskID: userTask.ID,
		Variables: map[string]any{
			"approvalResult": "APPROVED",
		},
	}); err != nil {
		t.Fatalf("complete user task: %v", err)
	}
	return rt
}

func findDispatchOutbox(t *testing.T, events []domain.OutboxEvent) domain.OutboxEvent {
	t.Helper()
	for _, event := range events {
		if event.EventType == string(domain.OutboxEventExternalTaskDispatch) {
			return event
		}
	}
	t.Fatalf("dispatch outbox not found in %#v", events)
	return domain.OutboxEvent{}
}
