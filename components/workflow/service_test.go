package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"
)

func TestNewEngineByCfgMemoryRuntime(t *testing.T) {
	eng, err := NewEngineByCfg(Config{Mode: RuntimeModeMemory})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	assertEngineCanRunProcess(t, eng)
}

func TestNewEngineByCfgSQLiteRuntime(t *testing.T) {
	eng, err := NewEngineByCfg(Config{
		Mode:        RuntimeModeSQLite,
		SQLitePath:  filepath.Join(t.TempDir(), "workflow.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("new sqlite engine: %v", err)
	}
	assertEngineCanRunProcess(t, eng)
}

func TestNewEngineByCfgRejectsMissingDSNForMySQL(t *testing.T) {
	if _, err := NewEngineByCfg(Config{Mode: RuntimeModeMySQL}); err == nil {
		t.Fatal("expected missing dsn error")
	}
}

func assertEngineCanRunProcess(t *testing.T, eng *Engine) {
	t.Helper()
	ctx := context.Background()
	def, err := eng.DeployDefinition(ctx, testfixture.OrderApprovalDefinition("https://contract-service/tasks"))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := eng.StartProcess(ctx, workflowengine.StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_CLIENT_1",
		Starter:       "user_1",
		Variables: map[string]any{
			"orderId": "ORDER_CLIENT_1",
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks, err := eng.Tasks(ctx, instance.ID)
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Type != domain.TaskTypeUser {
		t.Fatalf("expected one user task, got %#v", tasks)
	}
}
