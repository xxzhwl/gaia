package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/persistence/gormstore"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func TestDispatcherWorksWithPersistentRuntime(t *testing.T) {
	ctx := context.Background()
	var payload map[string]any
	bizServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode dispatch payload: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer bizServer.Close()

	rt := newPersistentRuntimeForTest(t)
	def, err := rt.DeployDefinition(ctx, testfixture.OrderApprovalDefinition(bizServer.URL))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := rt.StartProcess(ctx, engine.StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_PERSIST_DISPATCH",
		Starter:       "user_1",
		Variables: map[string]any{
			"orderId": "ORDER_PERSIST_DISPATCH",
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks, err := rt.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if _, err := rt.CompleteTask(ctx, engine.CompleteTaskRequest{
		TaskID: tasks[0].ID,
		Variables: map[string]any{
			"approvalResult": "APPROVED",
		},
	}); err != nil {
		t.Fatalf("complete approval: %v", err)
	}

	d := NewWithRuntime(rt, WithCallbackBaseURL("https://workflow.example.com"), WithEndpointValidator(&DefaultEndpointGuard{AllowPrivate: true}))
	sent, err := d.DispatchPending(ctx, 10)
	if err != nil {
		t.Fatalf("dispatch pending: %v", err)
	}
	if sent != 1 {
		t.Fatalf("expected one sent dispatch, got %d", sent)
	}
	if payload["callbackUrl"] == "" {
		t.Fatalf("missing callback url in payload: %#v", payload)
	}
	outbox, err := rt.OutboxEvents(ctx, domain.OutboxListFilter{})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	dispatchEvent := findDispatchOutbox(t, outbox.List)
	if dispatchEvent.Status != domain.OutboxStatusSent {
		t.Fatalf("expected sent outbox, got %#v", dispatchEvent)
	}
	tasks, err = rt.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if tasks[1].Status != domain.TaskStatusDispatched {
		t.Fatalf("expected dispatched task, got %#v", tasks[1])
	}
}

func newPersistentRuntimeForTest(t *testing.T) *engine.PersistentRuntime {
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
	return engine.NewPersistentRuntime(
		gormstore.NewUnitOfWork(db),
		fixedClock{now: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)},
		&engine.SequenceIDGenerator{},
	)
}
