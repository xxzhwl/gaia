package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/persistence/gormstore"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPersistentRuntimeCompletesApprovedWorkflow(t *testing.T) {
	ctx := context.Background()
	rt := newPersistentRuntimeForTest(t)
	def, err := rt.DeployDefinition(ctx, testfixture.OrderApprovalDefinition("https://contract-service/tasks"))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}

	instance, err := rt.StartProcess(ctx, StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_PERSIST_1",
		TenantID:      "tenant_a",
		Starter:       "user_1",
		Variables: map[string]any{
			"orderId": "ORDER_PERSIST_1",
			"amount":  900,
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks, err := rt.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Type != domain.TaskTypeUser {
		t.Fatalf("expected one user task, got %#v", tasks)
	}

	instance, err = rt.CompleteTask(ctx, CompleteTaskRequest{
		TaskID: tasks[0].ID,
		Variables: map[string]any{
			"approvalResult": "APPROVED",
		},
	})
	if err != nil {
		t.Fatalf("complete user task: %v", err)
	}
	if instance.Status != domain.InstanceStatusRunning {
		t.Fatalf("expected running instance, got %#v", instance)
	}
	tasks, err = rt.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 2 || tasks[1].Type != domain.TaskTypeExternal {
		t.Fatalf("expected external task after approval, got %#v", tasks)
	}
	outbox, err := rt.OutboxEvents(ctx)
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	dispatchEvent := findPersistentOutboxByType(t, outbox, domain.OutboxEventExternalTaskDispatch)
	if dispatchEvent.Status != domain.OutboxStatusNew {
		t.Fatalf("expected new dispatch outbox event, got %#v", dispatchEvent)
	}

	instance, err = rt.CompleteTask(ctx, CompleteTaskRequest{
		TaskID: tasks[1].ID,
		Variables: map[string]any{
			"contractId": "CT_PERSIST_1",
		},
	})
	if err != nil {
		t.Fatalf("complete external task: %v", err)
	}
	if instance.Status != domain.InstanceStatusCompleted || instance.EndNodeID != "end_approved" {
		t.Fatalf("unexpected final instance: %#v", instance)
	}
	instance, err = rt.CompleteTask(ctx, CompleteTaskRequest{
		TaskID: tasks[1].ID,
		Variables: map[string]any{
			"contractId": "CT_DUP",
		},
	})
	if err != nil {
		t.Fatalf("idempotent complete: %v", err)
	}
	vars, err := rt.Variables(ctx, instance.ID)
	if err != nil {
		t.Fatalf("variables: %v", err)
	}
	if vars["contractId"].Value != "CT_PERSIST_1" {
		t.Fatalf("idempotent complete changed contractId: %#v", vars["contractId"])
	}
}

func TestPersistentRuntimeDeployDefinitionCreatesNewVersion(t *testing.T) {
	ctx := context.Background()
	rt := newPersistentRuntimeForTest(t)
	def, err := rt.DeployDefinition(ctx, testfixture.OrderApprovalDefinition("https://contract-service/tasks"))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	def.Model.Nodes["manager_approve"] = domain.Node{
		ID:                 "manager_approve",
		Type:               domain.NodeTypeUserTask,
		Name:               "二次审批",
		AssigneeExpression: "${starter.managerId}",
		OutputVariables:    []string{"approvalResult"},
	}

	deployed, err := rt.DeployDefinition(ctx, def)
	if err != nil {
		t.Fatalf("redeploy definition: %v", err)
	}
	if deployed.ID == def.ID {
		t.Fatalf("expected redeploy to create a new definition id")
	}
	if deployed.Version != def.Version+1 {
		t.Fatalf("expected version %d, got %d", def.Version+1, deployed.Version)
	}

	instance, err := rt.StartProcess(ctx, StartProcessRequest{
		DefinitionKey: def.Key,
		Variables:     map[string]any{"orderId": "ORDER_VERSION_1"},
	})
	if err != nil {
		t.Fatalf("start latest process: %v", err)
	}
	if instance.DefinitionID != deployed.ID || instance.DefinitionVersion != deployed.Version {
		t.Fatalf("latest process did not use redeployed definition: %#v", instance)
	}
}

func TestPersistentRuntimeConcurrentCompleteTaskIsIdempotent(t *testing.T) {
	ctx := context.Background()
	rt := newPersistentRuntimeForTest(t)
	def, err := rt.DeployDefinition(ctx, testfixture.OrderApprovalDefinition("https://contract-service/tasks"))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := rt.StartProcess(ctx, StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_CONCURRENT_1",
		Starter:       "user_1",
		Variables: map[string]any{
			"orderId": "ORDER_CONCURRENT_1",
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks, err := rt.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := rt.CompleteTask(context.Background(), CompleteTaskRequest{
				TaskID: tasks[0].ID,
				Variables: map[string]any{
					"approvalResult": "REJECTED",
				},
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent complete returned error: %v", err)
		}
	}

	instance, err = rt.GetInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if instance.Status != domain.InstanceStatusCompleted || instance.EndNodeID != "end_rejected" {
		t.Fatalf("unexpected final instance: %#v", instance)
	}
	tasks, err = rt.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != domain.TaskStatusCompleted {
		t.Fatalf("expected one completed task, got %#v", tasks)
	}
	events, err := rt.AuditTrail(ctx, instance.ID)
	if err != nil {
		t.Fatalf("audit trail: %v", err)
	}
	completedEvents := 0
	for _, event := range events {
		if event.EventType == domain.AuditTaskCompleted {
			completedEvents++
		}
	}
	if completedEvents != 1 {
		t.Fatalf("expected exactly one task completed audit event, got %d", completedEvents)
	}
}

func TestPersistentRuntimeScanTimeoutTasksCreatesNotificationOutbox(t *testing.T) {
	ctx := context.Background()
	rt := newPersistentRuntimeForTest(t)
	def, err := rt.DeployDefinition(ctx, domain.ProcessDefinition{
		Key:  "persistent_timeout_task",
		Name: "Persistent Timeout Task",
		Model: domain.WorkflowModel{
			Nodes: map[string]domain.Node{
				"start": {ID: "start", Type: domain.NodeTypeStartEvent, Name: "Start"},
				"approve": {
					ID:                 "approve",
					Type:               domain.NodeTypeUserTask,
					Name:               "审批",
					AssigneeExpression: "${starter}",
					TimeoutSeconds:     60,
				},
				"end": {ID: "end", Type: domain.NodeTypeEndEvent, Name: "End"},
			},
			SequenceFlows: []domain.SequenceFlow{
				{ID: "flow_start_approve", SourceRef: "start", TargetRef: "approve"},
				{ID: "flow_approve_end", SourceRef: "approve", TargetRef: "end"},
			},
		},
	})
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := rt.StartProcess(ctx, StartProcessRequest{
		DefinitionKey: def.Key,
		Starter:       "user_1",
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks, err := rt.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}

	result, err := rt.ScanTimeoutTasks(ctx, ScanTimeoutTasksRequest{
		Now:   time.Date(2026, 6, 28, 12, 1, 1, 0, time.UTC),
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("scan timeout tasks: %v", err)
	}
	if result.Scanned != 1 || result.TimedOut != 1 || len(result.Events) != 2 {
		t.Fatalf("unexpected scan result: %#v", result)
	}
	outbox, err := rt.OutboxEvents(ctx)
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	timeoutEvent := findPersistentOutboxByType(t, outbox, domain.OutboxEventTaskTimeout)
	if timeoutEvent.AggregateID != tasks[0].ID {
		t.Fatalf("timeout event aggregate mismatch: %#v", timeoutEvent)
	}
	notificationEvent := findPersistentNotificationOutbox(t, outbox, "task_timeout")
	if notificationEvent.AggregateID != tasks[0].ID {
		t.Fatalf("notification event aggregate mismatch: %#v", notificationEvent)
	}

	result, err = rt.ScanTimeoutTasks(ctx, ScanTimeoutTasksRequest{
		Now:   time.Date(2026, 6, 28, 12, 2, 0, 0, time.UTC),
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("rescan timeout tasks: %v", err)
	}
	if result.Scanned != 1 || result.TimedOut != 0 || len(result.Events) != 0 {
		t.Fatalf("timeout scan should be idempotent, got %#v", result)
	}
}

func findPersistentOutboxByType(t *testing.T, events []domain.OutboxEvent, eventType domain.OutboxEventType) domain.OutboxEvent {
	t.Helper()
	for _, event := range events {
		if event.EventType == string(eventType) {
			return event
		}
	}
	t.Fatalf("outbox event %s not found in %#v", eventType, events)
	return domain.OutboxEvent{}
}

func findPersistentNotificationOutbox(t *testing.T, events []domain.OutboxEvent, notificationType string) domain.OutboxEvent {
	t.Helper()
	for _, event := range events {
		if event.EventType == string(domain.OutboxEventNotificationRequested) && event.Payload["notificationType"] == notificationType {
			return event
		}
	}
	t.Fatalf("notification outbox %s not found in %#v", notificationType, events)
	return domain.OutboxEvent{}
}

func newPersistentRuntimeForTest(t *testing.T) *PersistentRuntime {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "workflow.db")
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL", dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gormstore.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return NewPersistentRuntime(
		gormstore.NewUnitOfWork(db),
		fixedClock{now: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)},
		&SequenceIDGenerator{},
	)
}
