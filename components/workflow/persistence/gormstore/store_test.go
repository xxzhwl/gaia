package gormstore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/persistence"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRepositoriesRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	uow := NewUnitOfWork(db)
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

	err := uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		def := testfixture.OrderApprovalDefinition("https://contract-service/tasks")
		def.ID = "def_1"
		def.Version = 1
		def.Status = domain.DefinitionStatusDeployed
		def.CreatedAt = now
		def.DeployedAt = &now
		if err := repos.Definitions().Save(ctx, def); err != nil {
			return err
		}

		instance := domain.ProcessInstance{
			ID:                "pi_1",
			DefinitionID:      def.ID,
			DefinitionKey:     def.Key,
			DefinitionVersion: def.Version,
			BusinessKey:       "ORDER_1",
			TenantID:          "tenant_a",
			Status:            domain.InstanceStatusRunning,
			StartTime:         now,
			Starter:           "user_1",
			Version:           1,
		}
		if err := repos.Instances().Save(ctx, instance); err != nil {
			return err
		}
		if err := repos.Instances().SaveExecution(ctx, domain.Execution{
			ID:         "exe_1",
			InstanceID: instance.ID,
			NodeID:     "manager_approve",
			Status:     domain.ExecutionStatusWaiting,
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			return err
		}
		if err := repos.Instances().SaveActivity(ctx, domain.ActivityInstance{
			ID:          "act_1",
			InstanceID:  instance.ID,
			ExecutionID: "exe_1",
			NodeID:      "manager_approve",
			NodeType:    domain.NodeTypeUserTask,
			NodeName:    "Manager Approve",
			Status:      domain.ActivityStatusWaiting,
			StartTime:   now,
		}); err != nil {
			return err
		}
		if err := repos.Tasks().Save(ctx, domain.Task{
			ID:                 "task_1",
			InstanceID:         instance.ID,
			ActivityInstanceID: "act_1",
			NodeID:             "manager_approve",
			Title:              "审批 ORDER_1",
			Type:               domain.TaskTypeUser,
			Status:             domain.TaskStatusWaiting,
			Assignee:           "user_2",
			VariableSnapshot:   map[string]any{"orderId": "ORDER_1"},
			CreatedAt:          now,
		}); err != nil {
			return err
		}
		if err := repos.Variables().UpsertCurrent(ctx, domain.Variable{
			InstanceID: instance.ID,
			Name:       "orderId",
			Type:       "string",
			Value:      "ORDER_1",
			Scope:      domain.VariableScopeBusiness,
			UpdatedAt:  now,
		}); err != nil {
			return err
		}
		if err := repos.Variables().AppendHistory(ctx, domain.VariableHistory{
			ID:         "varhist_1",
			InstanceID: instance.ID,
			Name:       "orderId",
			NewValue:   "ORDER_1",
			CreatedAt:  now,
		}); err != nil {
			return err
		}
		if err := repos.Outbox().Save(ctx, domain.OutboxEvent{
			ID:          "outbox_1",
			EventType:   string(domain.OutboxEventExternalTaskDispatch),
			AggregateID: "task_2",
			Payload: map[string]any{
				"taskId":      "task_2",
				"dispatchUrl": "https://contract-service/tasks",
			},
			Status:    domain.OutboxStatusNew,
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := repos.Outbox().Save(ctx, domain.OutboxEvent{
			ID:          "outbox_notification_1",
			EventType:   string(domain.OutboxEventNotificationRequested),
			AggregateID: "task_1",
			Payload: map[string]any{
				"notificationType": "task_created",
				"taskId":           "task_1",
			},
			Status:    domain.OutboxStatusNew,
			CreatedAt: now.Add(time.Second),
		}); err != nil {
			return err
		}
		return repos.Audit().Append(ctx, domain.AuditEvent{
			ID:         "audit_1",
			InstanceID: instance.ID,
			EventType:  domain.AuditInstanceStarted,
			Payload:    map[string]any{"businessKey": instance.BusinessKey},
			CreatedAt:  now,
		})
	})
	if err != nil {
		t.Fatalf("write repositories: %v", err)
	}

	repos := uow.View(ctx)
	nextVersion, err := repos.Definitions().NextVersion(ctx, "order_approval")
	if err != nil {
		t.Fatalf("next version: %v", err)
	}
	if nextVersion != 2 {
		t.Fatalf("expected next version 2, got %d", nextVersion)
	}
	def, err := repos.Definitions().FindLatestDeployed(ctx, "order_approval")
	if err != nil {
		t.Fatalf("find definition: %v", err)
	}
	if def.Model.Nodes["send_contract"].Endpoint != "https://contract-service/tasks" {
		t.Fatalf("definition model did not round trip: %#v", def.Model.Nodes["send_contract"])
	}
	instance, err := repos.Instances().Get(ctx, "pi_1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if instance.BusinessKey != "ORDER_1" {
		t.Fatalf("unexpected instance: %#v", instance)
	}
	tasks, err := repos.Tasks().ListByInstance(ctx, "pi_1")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task did not round trip: %#v", tasks)
	}
	if tasks[0].Title != "审批 ORDER_1" {
		t.Fatalf("task metadata did not round trip: %#v", tasks[0])
	}
	if tasks[0].VariableSnapshot["orderId"] != "ORDER_1" {
		t.Fatalf("task snapshot did not round trip: %#v", tasks[0])
	}
	vars, err := repos.Variables().CurrentByInstance(ctx, "pi_1")
	if err != nil {
		t.Fatalf("current variables: %v", err)
	}
	if vars["orderId"].Value != "ORDER_1" {
		t.Fatalf("variable did not round trip: %#v", vars["orderId"])
	}
	claimed, err := repos.Outbox().ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("claim outbox: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Status != domain.OutboxStatusProcessing {
		t.Fatalf("unexpected claimed outbox: %#v", claimed)
	}
	claimedAgain, err := repos.Outbox().ClaimBatch(ctx, 10)
	if err != nil {
		t.Fatalf("claim outbox again: %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("expected no events after processing claim, got %#v", claimedAgain)
	}
	claimedNotification, err := repos.Outbox().ClaimBatchByType(ctx, string(domain.OutboxEventNotificationRequested), 10)
	if err != nil {
		t.Fatalf("claim notification outbox: %v", err)
	}
	if len(claimedNotification) != 1 || claimedNotification[0].ID != "outbox_notification_1" || claimedNotification[0].Status != domain.OutboxStatusProcessing {
		t.Fatalf("unexpected claimed notification outbox: %#v", claimedNotification)
	}
	nextRetryAt := now.Add(time.Minute)
	if err := repos.Outbox().MarkFailed(ctx, "outbox_1", &nextRetryAt); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	outbox, err := repos.Outbox().List(ctx)
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	if outbox[0].Status != domain.OutboxStatusFailed || outbox[0].RetryCount != 1 || outbox[0].NextRetryAt == nil {
		t.Fatalf("outbox failure metadata missing: %#v", outbox[0])
	}
	if err := repos.Outbox().MarkDead(ctx, "outbox_1"); err != nil {
		t.Fatalf("mark dead: %v", err)
	}
	outbox, err = repos.Outbox().List(ctx)
	if err != nil {
		t.Fatalf("list outbox after dead: %v", err)
	}
	if outbox[0].Status != domain.OutboxStatusDead || outbox[0].RetryCount != 2 {
		t.Fatalf("outbox dead metadata missing: %#v", outbox[0])
	}
	audit, err := repos.Audit().ListByInstance(ctx, "pi_1")
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(audit) != 1 || audit[0].Payload["businessKey"] != "ORDER_1" {
		t.Fatalf("audit did not round trip: %#v", audit)
	}
}

func TestUnitOfWorkRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	uow := NewUnitOfWork(db)
	expected := errors.New("boom")

	err := uow.WithinTx(ctx, func(ctx context.Context, repos persistence.Repositories) error {
		if err := repos.Instances().Save(ctx, domain.ProcessInstance{
			ID:                "pi_rollback",
			DefinitionID:      "def_1",
			DefinitionKey:     "order_approval",
			DefinitionVersion: 1,
			Status:            domain.InstanceStatusRunning,
			StartTime:         time.Now().UTC(),
			Version:           1,
		}); err != nil {
			return err
		}
		return expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected rollback error, got %v", err)
	}
	if _, err := uow.View(ctx).Instances().Get(ctx, "pi_rollback"); !IsNotFound(err) {
		t.Fatalf("expected rolled back instance to be missing, got %v", err)
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "workflow.db")
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?_busy_timeout=5000&_journal_mode=WAL", dbPath)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db
}
