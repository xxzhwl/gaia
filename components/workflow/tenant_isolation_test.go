package workflow

import (
	"context"
	"testing"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"
)

// startInstanceForTenant 在指定租户下部署定义并启动一个实例。
func startInstanceForTenant(t *testing.T, e *Engine, tenant, businessKey string) domain.ProcessInstance {
	t.Helper()
	ctx := context.Background()
	def, err := e.DeployDefinition(ctx, testfixture.OrderApprovalDefinition("https://worker.example.com/tasks"))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := e.StartProcess(ctx, workflowengine.StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   businessKey,
		TenantID:      tenant,
		Starter:       "user_1",
		Variables:     map[string]any{"orderId": businessKey},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	return instance
}

func TestListInstancesIsolatedByTenantFromContext(t *testing.T) {
	e := NewMemoryEngine()
	startInstanceForTenant(t, e, "tenant_a", "ORDER_A")
	startInstanceForTenant(t, e, "tenant_b", "ORDER_B")

	ctxA := domain.WithTenant(context.Background(), "tenant_a")
	result, err := e.ListInstances(ctxA, domain.InstanceListFilter{})
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if result.Total != 1 || len(result.List) != 1 {
		t.Fatalf("expected only tenant_a instance, got total=%d list=%d", result.Total, len(result.List))
	}
	if result.List[0].TenantID != "tenant_a" {
		t.Fatalf("expected tenant_a instance, got %s", result.List[0].TenantID)
	}
}

func TestListInstancesWithoutTenantContextReturnsAll(t *testing.T) {
	e := NewMemoryEngine()
	startInstanceForTenant(t, e, "tenant_a", "ORDER_A")
	startInstanceForTenant(t, e, "tenant_b", "ORDER_B")

	result, err := e.ListInstances(context.Background(), domain.InstanceListFilter{})
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("expected all instances without tenant context, got %d", result.Total)
	}
}

func TestGetInstanceRejectsCrossTenantAccess(t *testing.T) {
	e := NewMemoryEngine()
	instanceA := startInstanceForTenant(t, e, "tenant_a", "ORDER_A")

	// 同租户可读。
	ctxA := domain.WithTenant(context.Background(), "tenant_a")
	if _, err := e.GetInstance(ctxA, instanceA.ID); err != nil {
		t.Fatalf("same-tenant access should succeed, got %v", err)
	}

	// 跨租户按 ID 直取应被拒绝（表现为未找到）。
	ctxB := domain.WithTenant(context.Background(), "tenant_b")
	if _, err := e.GetInstance(ctxB, instanceA.ID); err == nil {
		t.Fatal("cross-tenant access should be rejected")
	}
}

func TestListTasksIsolatedByTenantFromContext(t *testing.T) {
	e := NewMemoryEngine()
	startInstanceForTenant(t, e, "tenant_a", "ORDER_A")
	startInstanceForTenant(t, e, "tenant_b", "ORDER_B")

	ctxA := domain.WithTenant(context.Background(), "tenant_a")
	result, err := e.ListTasks(ctxA, domain.TaskListFilter{})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if result.Total != 1 || len(result.List) != 1 {
		t.Fatalf("expected only tenant_a task, got total=%d list=%d", result.Total, len(result.List))
	}
	if result.List[0].InstanceID == "" {
		t.Fatal("expected task instance id")
	}
	instance, err := e.GetInstance(ctxA, result.List[0].InstanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if instance.TenantID != "tenant_a" {
		t.Fatalf("expected tenant_a task, got instance tenant %s", instance.TenantID)
	}
}

func TestInstanceScopedReadsRejectCrossTenantAccess(t *testing.T) {
	e := NewMemoryEngine()
	instanceA := startInstanceForTenant(t, e, "tenant_a", "ORDER_A")
	ctxB := domain.WithTenant(context.Background(), "tenant_b")

	if _, err := e.Tasks(ctxB, instanceA.ID); err == nil {
		t.Fatal("cross-tenant tasks access should be rejected")
	}
	if _, err := e.Variables(ctxB, instanceA.ID); err == nil {
		t.Fatal("cross-tenant variables access should be rejected")
	}
	if _, err := e.Timeline(ctxB, instanceA.ID); err == nil {
		t.Fatal("cross-tenant timeline access should be rejected")
	}
}
