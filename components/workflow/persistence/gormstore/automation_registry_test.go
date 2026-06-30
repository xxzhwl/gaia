package gormstore

import (
	"context"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/automation"
)

func TestAutomationRegistryPersistsAndResolvesServices(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	registry := NewAutomationRegistry(db)

	service, err := registry.Register(ctx, automation.Service{
		ID:         "order-worker",
		Name:       "Order Worker",
		BaseURL:    "127.0.0.1:19090",
		Protocol:   automation.ProtocolGRPC,
		Version:    "v1",
		Tags:       []string{"workflow", "order"},
		TTLSeconds: 60,
		Tasks: []automation.Task{{
			Key:         "settle_order",
			Name:        "订单结算",
			Description: "完成订单结算",
			InputSchema: []automation.Parameter{{
				Key:      "orderId",
				Name:     "订单号",
				Type:     "string",
				Required: true,
			}},
		}},
	})
	if err != nil {
		t.Fatalf("register service: %v", err)
	}
	if service.Protocol != automation.ProtocolGRPC {
		t.Fatalf("protocol not normalized: %#v", service)
	}

	loaded, task, err := registry.ResolveTask(ctx, "gaia://order-worker/settle_order")
	if err != nil {
		t.Fatalf("resolve task: %v", err)
	}
	if loaded.ID != "order-worker" || loaded.Protocol != automation.ProtocolGRPC || task.Key != "settle_order" || len(task.InputSchema) != 1 {
		t.Fatalf("service or task did not round trip: service=%#v task=%#v", loaded, task)
	}
	endpoint, err := registry.ResolveEndpoint(ctx, "gaia://order-worker/settle_order")
	if err != nil {
		t.Fatalf("resolve endpoint: %v", err)
	}
	if endpoint != "127.0.0.1:19090/settle_order" {
		t.Fatalf("unexpected endpoint: %s", endpoint)
	}

	registry.now = func() time.Time { return service.UpdatedAt.Add(61 * time.Second) }
	services, err := registry.ListServices(ctx)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expired service should be hidden: %#v", services)
	}
}
