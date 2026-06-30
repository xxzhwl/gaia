package engine

import (
	"context"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func TestRuntimeCompletesRejectedPathAtEndEvent(t *testing.T) {
	rt := newTestRuntime()
	def := deployTestDefinition(t, rt)

	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_1001",
		Starter:       "user_1",
		Variables: map[string]any{
			"orderId": "ORDER_1001",
			"amount":  1200,
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	tasks := rt.Tasks(instance.ID)
	if len(tasks) != 1 {
		t.Fatalf("expected one user task, got %d", len(tasks))
	}
	if tasks[0].Type != domain.TaskTypeUser || tasks[0].Status != domain.TaskStatusWaiting {
		t.Fatalf("unexpected task: %#v", tasks[0])
	}

	instance, err = rt.CompleteTask(context.Background(), CompleteTaskRequest{
		TaskID: tasks[0].ID,
		Variables: map[string]any{
			"approvalResult": "REJECTED",
		},
	})
	if err != nil {
		t.Fatalf("complete user task: %v", err)
	}
	if instance.Status != domain.InstanceStatusCompleted {
		t.Fatalf("expected completed instance, got %s", instance.Status)
	}
	if instance.EndNodeID != "end_rejected" {
		t.Fatalf("expected rejected end node, got %s", instance.EndNodeID)
	}

	vars := rt.Variables(instance.ID)
	if vars["approvalResult"].Value != "REJECTED" {
		t.Fatalf("approvalResult not written: %#v", vars["approvalResult"])
	}
}

func TestRuntimeCreatesExternalTaskAndOutboxThenCompletes(t *testing.T) {
	rt := newTestRuntime()
	def := deployTestDefinition(t, rt)

	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_1002",
		Starter:       "user_1",
		Variables: map[string]any{
			"orderId": "ORDER_1002",
			"amount":  800,
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	userTask := rt.Tasks(instance.ID)[0]
	instance, err = rt.CompleteTask(context.Background(), CompleteTaskRequest{
		TaskID: userTask.ID,
		Variables: map[string]any{
			"approvalResult": "APPROVED",
		},
	})
	if err != nil {
		t.Fatalf("complete user task: %v", err)
	}
	if instance.Status != domain.InstanceStatusRunning {
		t.Fatalf("expected running instance, got %s", instance.Status)
	}

	tasks := rt.Tasks(instance.ID)
	if len(tasks) != 2 {
		t.Fatalf("expected user task and external task, got %d", len(tasks))
	}
	externalTask := tasks[1]
	if externalTask.Type != domain.TaskTypeExternal || externalTask.DispatchURL != "https://contract-service/tasks" {
		t.Fatalf("unexpected external task: %#v", externalTask)
	}

	outbox := rt.OutboxEvents()
	dispatchEvent := findOutboxByType(t, outbox, domain.OutboxEventExternalTaskDispatch)
	if dispatchEvent.AggregateID != externalTask.ID {
		t.Fatalf("outbox aggregate mismatch: %#v", dispatchEvent)
	}
	payloadVars := dispatchEvent.Payload["variables"].(map[string]any)
	if payloadVars["orderId"] != "ORDER_1002" {
		t.Fatalf("expected orderId in dispatch payload, got %#v", payloadVars)
	}

	instance, err = rt.CompleteTask(context.Background(), CompleteTaskRequest{
		TaskID: externalTask.ID,
		Variables: map[string]any{
			"contractId": "CT_9988",
		},
	})
	if err != nil {
		t.Fatalf("complete external task: %v", err)
	}
	if instance.Status != domain.InstanceStatusCompleted || instance.EndNodeID != "end_approved" {
		t.Fatalf("unexpected completed instance: %#v", instance)
	}

	instance, err = rt.CompleteTask(context.Background(), CompleteTaskRequest{
		TaskID: externalTask.ID,
		Variables: map[string]any{
			"contractId": "CT_DUPLICATE",
		},
	})
	if err != nil {
		t.Fatalf("second complete should be idempotent: %v", err)
	}
	vars := rt.Variables(instance.ID)
	if vars["contractId"].Value != "CT_9988" {
		t.Fatalf("idempotent complete changed variable: %#v", vars["contractId"])
	}
}

func TestRuntimeUserTaskMetadataAndOperations(t *testing.T) {
	rt := newTestRuntime()
	def := deployTestDefinition(t, rt)
	approve := def.Model.Nodes["manager_approve"]
	approve.Name = "经理审批"
	approve.TimeoutSeconds = 3600
	def.Model.Nodes["manager_approve"] = approve
	if _, err := rt.DeployDefinition(context.Background(), def); err != nil {
		t.Fatalf("redeploy definition: %v", err)
	}

	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: def.Key,
		BusinessKey:   "ORDER_META_1",
		Variables: map[string]any{
			"orderId":   "ORDER_META_1",
			"approvers": []any{"user_a", "user_b"},
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	task := rt.Tasks(instance.ID)[0]
	if task.Title != "经理审批" {
		t.Fatalf("task metadata not built: %#v", task)
	}
	if task.TimeoutAt == nil || task.VariableSnapshot["orderId"] != "ORDER_META_1" {
		t.Fatalf("task snapshot or timeout missing: %#v", task)
	}

	task, err = rt.ClaimTask(context.Background(), TaskOperationRequest{TaskID: task.ID, Operator: "user_a"})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if task.Status != domain.TaskStatusClaimed || task.Assignee != "user_a" {
		t.Fatalf("unexpected claimed task: %#v", task)
	}
	task, err = rt.TransferTask(context.Background(), TaskOperationRequest{TaskID: task.ID, Operator: "user_a", TargetAssignee: "user_b", Comment: "handoff"})
	if err != nil {
		t.Fatalf("transfer task: %v", err)
	}
	if task.Assignee != "user_b" || task.Owner != "user_a" || task.Comment != "handoff" {
		t.Fatalf("unexpected transferred task: %#v", task)
	}
}

func TestRuntimeScanTimeoutTasksCreatesNotificationOutbox(t *testing.T) {
	rt := newTestRuntime()
	def, err := rt.DeployDefinition(context.Background(), domain.ProcessDefinition{
		Key:  "timeout_task",
		Name: "Timeout Task",
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
	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: def.Key,
		Starter:       "user_1",
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	result, err := rt.ScanTimeoutTasks(context.Background(), ScanTimeoutTasksRequest{
		Now:   time.Date(2026, 6, 28, 12, 1, 1, 0, time.UTC),
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("scan timeout tasks: %v", err)
	}
	if result.Scanned != 1 || result.TimedOut != 1 || len(result.Events) != 2 {
		t.Fatalf("unexpected scan result: %#v", result)
	}
	timeoutEvent := findOutboxByType(t, rt.OutboxEvents(), domain.OutboxEventTaskTimeout)
	if timeoutEvent.AggregateID != rt.Tasks(instance.ID)[0].ID {
		t.Fatalf("timeout event aggregate mismatch: %#v", timeoutEvent)
	}
	notificationEvent := findNotificationOutbox(t, rt.OutboxEvents(), "task_timeout")
	if notificationEvent.AggregateID != timeoutEvent.AggregateID {
		t.Fatalf("notification event aggregate mismatch: %#v", notificationEvent)
	}

	result, err = rt.ScanTimeoutTasks(context.Background(), ScanTimeoutTasksRequest{
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

func TestRuntimeDeployDefinitionCreatesNewVersion(t *testing.T) {
	rt := newTestRuntime()
	def := deployTestDefinition(t, rt)
	approve := def.Model.Nodes["manager_approve"]
	approve.Name = "二次审批"
	def.Model.Nodes["manager_approve"] = approve

	deployed, err := rt.DeployDefinition(context.Background(), def)
	if err != nil {
		t.Fatalf("redeploy definition: %v", err)
	}
	if deployed.ID == def.ID {
		t.Fatalf("expected redeploy to create a new definition id")
	}
	if deployed.Version != def.Version+1 {
		t.Fatalf("expected version %d, got %d", def.Version+1, deployed.Version)
	}

	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
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

func TestRuntimeEndEventCancelsOpenTasksOnOtherBranches(t *testing.T) {
	rt := newTestRuntime()
	def, err := rt.DeployDefinition(context.Background(), deadEndBranchDefinition())
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: def.Key,
		Starter:       "user_1",
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	if instance.Status != domain.InstanceStatusCompleted || instance.EndNodeID != "end" {
		t.Fatalf("expected instance completed by end branch: %#v", instance)
	}
	tasks := rt.Tasks(instance.ID)
	if len(tasks) != 1 {
		t.Fatalf("expected dead-end branch task to be created then canceled, got %#v", tasks)
	}
	if tasks[0].Status != domain.TaskStatusCanceled {
		t.Fatalf("expected open task canceled after instance end, got %#v", tasks[0])
	}
}

func TestParallelGatewayForksAndJoins(t *testing.T) {
	rt := newTestRuntime()
	if _, err := rt.DeployDefinition(context.Background(), gatewayTestDefinition("parallel_gateway", domain.NodeTypeParallelGateway, []domain.SequenceFlow{
		{ID: "flow_split_a", SourceRef: "split", TargetRef: "task_a"},
		{ID: "flow_split_b", SourceRef: "split", TargetRef: "task_b"},
	})); err != nil {
		t.Fatalf("deploy parallel definition: %v", err)
	}

	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: "parallel_gateway",
		Variables:     map[string]any{"route": "both"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks := rt.Tasks(instance.ID)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 parallel tasks, got %d", len(tasks))
	}

	instance, err = rt.CompleteTask(context.Background(), CompleteTaskRequest{TaskID: tasks[0].ID})
	if err != nil {
		t.Fatalf("complete first branch: %v", err)
	}
	if instance.Status != domain.InstanceStatusRunning {
		t.Fatalf("instance should wait for second branch, got %s", instance.Status)
	}

	instance, err = rt.CompleteTask(context.Background(), CompleteTaskRequest{TaskID: tasks[1].ID})
	if err != nil {
		t.Fatalf("complete second branch: %v", err)
	}
	if instance.Status != domain.InstanceStatusCompleted {
		t.Fatalf("instance should complete after both branches join, got %s", instance.Status)
	}
}

func TestInclusiveGatewayOnlyActivatesMatchedBranches(t *testing.T) {
	rt := newTestRuntime()
	if _, err := rt.DeployDefinition(context.Background(), gatewayTestDefinition("inclusive_gateway", domain.NodeTypeInclusiveGateway, []domain.SequenceFlow{
		{ID: "flow_split_a", SourceRef: "split", TargetRef: "task_a", Condition: `${route == "a"}`},
		{ID: "flow_split_b", SourceRef: "split", TargetRef: "task_b", Condition: `${route == "b"}`},
		{ID: "flow_split_default", SourceRef: "split", TargetRef: "task_b", Default: true},
	})); err != nil {
		t.Fatalf("deploy inclusive definition: %v", err)
	}

	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: "inclusive_gateway",
		Variables:     map[string]any{"route": "a"},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks := rt.Tasks(instance.ID)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 matched inclusive task, got %d", len(tasks))
	}
	if tasks[0].NodeID != "task_a" {
		t.Fatalf("expected task_a branch, got %s", tasks[0].NodeID)
	}

	instance, err = rt.CompleteTask(context.Background(), CompleteTaskRequest{TaskID: tasks[0].ID})
	if err != nil {
		t.Fatalf("complete matched branch: %v", err)
	}
	if instance.Status != domain.InstanceStatusCompleted {
		t.Fatalf("instance should complete after matched branch joins, got %s", instance.Status)
	}
}

func newTestRuntime() *Runtime {
	return NewRuntime(fixedClock{now: time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)}, &SequenceIDGenerator{})
}

func gatewayTestDefinition(key string, gatewayType domain.NodeType, splitFlows []domain.SequenceFlow) domain.ProcessDefinition {
	flows := []domain.SequenceFlow{
		{ID: "flow_start_split", SourceRef: "start", TargetRef: "split"},
	}
	flows = append(flows, splitFlows...)
	flows = append(flows,
		domain.SequenceFlow{ID: "flow_a_join", SourceRef: "task_a", TargetRef: "join"},
		domain.SequenceFlow{ID: "flow_b_join", SourceRef: "task_b", TargetRef: "join"},
		domain.SequenceFlow{ID: "flow_join_end", SourceRef: "join", TargetRef: "end"},
	)
	return domain.ProcessDefinition{
		Key:  key,
		Name: key,
		Model: domain.WorkflowModel{
			Nodes: map[string]domain.Node{
				"start": {ID: "start", Type: domain.NodeTypeStartEvent, Name: "Start"},
				"split": {ID: "split", Type: gatewayType, Name: "Split"},
				"task_a": {
					ID:                 "task_a",
					Type:               domain.NodeTypeUserTask,
					Name:               "Task A",
					AssigneeExpression: "${starter}",
				},
				"task_b": {
					ID:                 "task_b",
					Type:               domain.NodeTypeUserTask,
					Name:               "Task B",
					AssigneeExpression: "${starter}",
				},
				"join": {ID: "join", Type: gatewayType, Name: "Join"},
				"end":  {ID: "end", Type: domain.NodeTypeEndEvent, Name: "End"},
			},
			SequenceFlows: flows,
		},
	}
}

func deadEndBranchDefinition() domain.ProcessDefinition {
	return domain.ProcessDefinition{
		Key:  "dead_end_branch",
		Name: "Dead End Branch",
		Model: domain.WorkflowModel{
			Nodes: map[string]domain.Node{
				"start": {ID: "start", Type: domain.NodeTypeStartEvent, Name: "Start"},
				"split": {ID: "split", Type: domain.NodeTypeParallelGateway, Name: "Split"},
				"todo": {
					ID:                 "todo",
					Type:               domain.NodeTypeUserTask,
					Name:               "旁路待办",
					AssigneeExpression: "${starter}",
				},
				"end": {ID: "end", Type: domain.NodeTypeEndEvent, Name: "End"},
			},
			SequenceFlows: []domain.SequenceFlow{
				{ID: "flow_start_split", SourceRef: "start", TargetRef: "split"},
				{ID: "flow_split_todo", SourceRef: "split", TargetRef: "todo"},
				{ID: "flow_split_end", SourceRef: "split", TargetRef: "end"},
			},
		},
	}
}

func TestRuntimeMapsServiceTaskInputsWithExpressions(t *testing.T) {
	rt := newTestRuntime()
	def, err := rt.DeployDefinition(context.Background(), domain.ProcessDefinition{
		Key:  "mapped_service_input",
		Name: "Mapped Service Input",
		Model: domain.WorkflowModel{
			Nodes: map[string]domain.Node{
				"start": {
					ID:   "start",
					Type: domain.NodeTypeStartEvent,
					Name: "Start",
				},
				"call_service": {
					ID:                  "call_service",
					Type:                domain.NodeTypeServiceTask,
					Name:                "Call Service",
					Endpoint:            "gaia://order-worker/normalize_order",
					AutomationServiceID: "order-worker",
					AutomationTaskKey:   "normalize_order",
					InputMappings: []domain.InputMapping{
						{Parameter: "orderId", Expression: "${businessOrderId}"},
						{Parameter: "amount", Expression: "${totalAmount}"},
						{Parameter: "payload", Expression: `{"id":"${businessOrderId}","items":"${items}","literal":"orderId"}`},
					},
				},
				"end": {
					ID:   "end",
					Type: domain.NodeTypeEndEvent,
					Name: "End",
				},
			},
			SequenceFlows: []domain.SequenceFlow{
				{ID: "flow_start_call", SourceRef: "start", TargetRef: "call_service"},
				{ID: "flow_call_end", SourceRef: "call_service", TargetRef: "end"},
			},
		},
	})
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	instance, err := rt.StartProcess(context.Background(), StartProcessRequest{
		DefinitionKey: def.Key,
		Variables: map[string]any{
			"businessOrderId": "ORDER_MAPPED_1",
			"totalAmount":     128.5,
			"items":           []any{"sku_1", "sku_2"},
		},
	})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	outbox := rt.OutboxEvents()
	dispatchEvent := findOutboxByType(t, outbox, domain.OutboxEventExternalTaskDispatch)
	payloadVars := dispatchEvent.Payload["variables"].(map[string]any)
	if payloadVars["orderId"] != "ORDER_MAPPED_1" || payloadVars["amount"] != 128.5 {
		t.Fatalf("scalar mappings not resolved: %#v", payloadVars)
	}
	payload := payloadVars["payload"].(map[string]any)
	if payload["id"] != "ORDER_MAPPED_1" || payload["literal"] != "orderId" {
		t.Fatalf("json object mappings not resolved: %#v", payload)
	}
	items := payload["items"].([]any)
	if len(items) != 2 || items[0] != "sku_1" {
		t.Fatalf("json array mapping not resolved: %#v", payload)
	}
	tasks := rt.Tasks(instance.ID)
	if len(tasks) != 1 || tasks[0].Type != domain.TaskTypeExternal {
		t.Fatalf("expected one external task, got %#v", tasks)
	}
}

func deployTestDefinition(t *testing.T, rt *Runtime) domain.ProcessDefinition {
	t.Helper()
	def, err := rt.DeployDefinition(context.Background(), domain.ProcessDefinition{
		Key:  "order_approval",
		Name: "Order Approval",
		Inputs: []domain.InputParameter{
			{Name: "orderId", Type: "string", Required: true},
		},
		Model: domain.WorkflowModel{
			Nodes: map[string]domain.Node{
				"start": {
					ID:   "start",
					Type: domain.NodeTypeStartEvent,
					Name: "Start",
				},
				"manager_approve": {
					ID:                 "manager_approve",
					Type:               domain.NodeTypeUserTask,
					Name:               "Manager Approve",
					AssigneeExpression: "${starter.managerId}",
					OutputVariables:    []string{"approvalResult"},
				},
				"approval_gateway": {
					ID:   "approval_gateway",
					Type: domain.NodeTypeExclusiveGateway,
					Name: "Approval Gateway",
				},
				"send_contract": {
					ID:              "send_contract",
					Type:            domain.NodeTypeServiceTask,
					Name:            "Send Contract",
					Endpoint:        "https://contract-service/tasks",
					InputMappings:   []domain.InputMapping{{Parameter: "orderId", Expression: "${orderId}"}},
					OutputVariables: []string{"contractId"},
					TimeoutSeconds:  300,
				},
				"end_approved": {
					ID:   "end_approved",
					Type: domain.NodeTypeEndEvent,
					Name: "Approved End",
				},
				"end_rejected": {
					ID:   "end_rejected",
					Type: domain.NodeTypeEndEvent,
					Name: "Rejected End",
				},
			},
			SequenceFlows: []domain.SequenceFlow{
				{ID: "flow_start_approve", SourceRef: "start", TargetRef: "manager_approve"},
				{ID: "flow_approve_gateway", SourceRef: "manager_approve", TargetRef: "approval_gateway"},
				{ID: "flow_approved", SourceRef: "approval_gateway", TargetRef: "send_contract", Condition: `${approvalResult == "APPROVED"}`},
				{ID: "flow_rejected", SourceRef: "approval_gateway", TargetRef: "end_rejected", Default: true},
				{ID: "flow_contract_end", SourceRef: "send_contract", TargetRef: "end_approved"},
			},
		},
	})
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	return def
}

func findOutboxByType(t *testing.T, events []domain.OutboxEvent, eventType domain.OutboxEventType) domain.OutboxEvent {
	t.Helper()
	for _, event := range events {
		if event.EventType == string(eventType) {
			return event
		}
	}
	t.Fatalf("outbox event %s not found in %#v", eventType, events)
	return domain.OutboxEvent{}
}

func findNotificationOutbox(t *testing.T, events []domain.OutboxEvent, notificationType string) domain.OutboxEvent {
	t.Helper()
	for _, event := range events {
		if event.EventType == string(domain.OutboxEventNotificationRequested) && event.Payload["notificationType"] == notificationType {
			return event
		}
	}
	t.Fatalf("notification outbox %s not found in %#v", notificationType, events)
	return domain.OutboxEvent{}
}
