package automation

import (
	"context"
	"testing"
)

type sampleInput struct {
	OrderID string  `json:"orderId" workflow:"name=订单号,required"`
	Amount  float64 `json:"amount" workflow:"name=订单金额,required"`
	Note    string  `json:"note,omitempty" workflow:"name=备注"`
}

type sampleOutput struct {
	Accepted bool   `json:"accepted" workflow:"name=是否通过"`
	Message  string `json:"message" workflow:"name=消息"`
}

func TestMethodCatalogReflectsSchemaAndExecutes(t *testing.T) {
	catalog := NewMethodCatalog()
	catalog.MustRegister("sample_task", func(_ context.Context, input sampleInput) (sampleOutput, error) {
		return sampleOutput{
			Accepted: input.Amount >= 100,
			Message:  input.OrderID + ":" + input.Note,
		}, nil
	}, WithTaskName("示例任务"))

	tasks := catalog.Tasks("svc_1", "http://127.0.0.1/tasks")
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.ServiceID != "svc_1" || task.Endpoint != "http://127.0.0.1/tasks" {
		t.Fatalf("unexpected task service metadata: %#v", task)
	}
	if len(task.InputSchema) != 3 || task.InputSchema[0].Key != "orderId" || task.InputSchema[0].Name != "订单号" {
		t.Fatalf("input schema not reflected: %#v", task.InputSchema)
	}
	if !task.InputSchema[0].Required || task.InputSchema[2].Required {
		t.Fatalf("required flags not reflected: %#v", task.InputSchema)
	}
	if len(task.OutputSchema) != 2 || task.OutputSchema[0].Key != "accepted" || task.OutputSchema[0].Type != "boolean" {
		t.Fatalf("output schema not reflected: %#v", task.OutputSchema)
	}

	output, err := catalog.Execute(context.Background(), "sample_task", map[string]any{
		"orderId": "O-1",
		"amount":  "128.50",
		"note":    "ok",
	})
	if err != nil {
		t.Fatalf("execute task: %v", err)
	}
	if output["accepted"] != true || output["message"] != "O-1:ok" {
		t.Fatalf("unexpected output: %#v", output)
	}
}
