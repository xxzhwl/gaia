package dispatcher

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"
	workflowgrpc "github.com/xxzhwl/gaia/components/workflow/transport/grpc"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type testAutomationWorker struct {
	received automation.DispatchRequest
}

func (w *testAutomationWorker) DispatchTask(_ context.Context, req automation.DispatchRequest) (automation.DispatchResult, error) {
	w.received = req
	return automation.DispatchResult{
		TaskID:            req.TaskID,
		ProcessInstanceID: req.ProcessInstanceID,
		NodeID:            req.NodeID,
		Completed:         true,
		Variables:         map[string]any{"done": true},
	}, nil
}

func (w *testAutomationWorker) Health(context.Context) (map[string]any, error) {
	return map[string]any{"status": "ok"}, nil
}

func TestDispatcherDispatchesGRPCAutomationTask(t *testing.T) {
	ctx := context.Background()
	listener := bufconn.Listen(1024 * 1024)
	server := googlegrpc.NewServer()
	worker := &testAutomationWorker{}
	workflowgrpc.RegisterAutomationWorkerService(server, worker)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	conn, err := googlegrpc.NewClient("passthrough:///bufnet",
		googlegrpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		googlegrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial worker: %v", err)
	}
	defer conn.Close()

	registry := automation.NewMemoryRegistry()
	if _, err := registry.Register(ctx, automation.Service{
		ID:       "grpc-worker",
		Name:     "gRPC Worker",
		BaseURL:  "bufnet",
		Protocol: automation.ProtocolGRPC,
		Tasks: []automation.Task{{
			Key: "send_contract",
		}},
	}); err != nil {
		t.Fatalf("register automation service: %v", err)
	}

	rt := engine.NewRuntime(fixedClock{now: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)}, &engine.SequenceIDGenerator{})
	def := testfixture.OrderApprovalDefinition("gaia://grpc-worker/send_contract")
	def.Model.Nodes["send_contract"] = domain.Node{
		ID:                  "send_contract",
		Type:                domain.NodeTypeServiceTask,
		Name:                "Send Contract",
		Endpoint:            "gaia://grpc-worker/send_contract",
		AutomationServiceID: "grpc-worker",
		AutomationTaskKey:   "send_contract",
		InputMappings:       []domain.InputMapping{{Parameter: "orderId", Expression: "${orderId}"}},
		OutputVariables:     []string{"done"},
	}
	deployed, err := rt.DeployDefinition(ctx, def)
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := rt.StartProcess(ctx, engine.StartProcessRequest{
		DefinitionKey: deployed.Key,
		Variables:     map[string]any{"orderId": "ORDER_GRPC_1"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	userTask := rt.Tasks(instance.ID)[0]
	if _, err := rt.CompleteTask(ctx, engine.CompleteTaskRequest{
		TaskID:    userTask.ID,
		Variables: map[string]any{"approvalResult": "APPROVED"},
	}); err != nil {
		t.Fatalf("complete user task: %v", err)
	}

	d := New(rt, WithAutomationRegistry(registry), WithGRPCInvoker(staticGRPCInvoker{conn: conn}))
	count, err := d.DispatchPending(ctx, 10)
	if err != nil {
		t.Fatalf("dispatch pending: %v", err)
	}
	if count != 1 || worker.received.AutomationTaskKey != "send_contract" || worker.received.Variables["orderId"] != "ORDER_GRPC_1" {
		t.Fatalf("worker did not receive expected request: count=%d req=%#v", count, worker.received)
	}
	tasks := rt.Tasks(instance.ID)
	if tasks[1].Status != domain.TaskStatusCompleted {
		t.Fatalf("expected external task completed, got %#v", tasks[1])
	}
	latest, ok := rt.GetInstance(instance.ID)
	if !ok || latest.Status != domain.InstanceStatusCompleted {
		t.Fatalf("expected workflow completed by grpc worker result, got %#v", latest)
	}
}

type staticGRPCInvoker struct {
	conn *googlegrpc.ClientConn
}

func (i staticGRPCInvoker) DispatchTask(ctx context.Context, _ string, req automation.DispatchRequest) (automation.DispatchResult, error) {
	var out automation.DispatchResult
	err := i.conn.Invoke(ctx,
		"/"+workflowgrpc.AutomationWorkerServiceName+"/"+workflowgrpc.AutomationWorkerDispatchMethod,
		req,
		&out,
		googlegrpc.CallContentSubtype("json"),
	)
	return out, err
}
