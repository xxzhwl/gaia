package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/xxzhwl/gaia"
	coretask "github.com/xxzhwl/gaia/components/asynctask"
	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
)

const (
	testTheme   = "workflow-worker-test"
	testService = "order_worker"
)

type orderInput struct {
	OrderID string `json:"orderId"`
}

type orderWorker struct{}

// NormalizeOrder 归一化订单入参（测试用）。
func (orderWorker) NormalizeOrder(_ context.Context, in orderInput) (map[string]any, error) {
	return map[string]any{"ok": true, "orderId": in.OrderID}, nil
}

// fakeScheduler 替换真实 asynctask 调度器，避免测试依赖 MySQL。
type fakeScheduler struct {
	received coretask.TaskBaseInfo
	enqueued []int64
	nextID   int64
}

func (s *fakeScheduler) ReceiveTask(task coretask.TaskBaseInfo) (coretask.TaskModel, error) {
	s.received = task
	if s.nextID == 0 {
		s.nextID = 42
	}
	return coretask.TaskModel{Id: s.nextID, TaskBaseInfo: task}, nil
}

func (s *fakeScheduler) TaskQuickQueue(taskID int64) {
	s.enqueued = append(s.enqueued, taskID)
}

// newTestWorker 用极简 API 构造：只需注册 proxy → New → RegisterService(Themes:[...])。
func newTestWorker(t *testing.T) (*Worker, *fakeScheduler, *MemoryBindingStore) {
	t.Helper()
	// 唯一必需的业务侧动作：把 proxy 挂到 gaia。
	gaia.RegisterProxy(testTheme, testService, orderWorker{})

	store := NewMemoryBindingStore()
	sch := &fakeScheduler{}
	w := New(WithBindingStore(store))
	if _, err := w.RegisterService(automation.Service{
		ID:       "worker-under-test",
		Themes:   []string{testTheme},
		Protocol: automation.ProtocolHTTP,
	}); err != nil {
		t.Fatalf("register service: %v", err)
	}
	w.resolveScheduler = func(string) schedulerPort { return sch }
	return w, sch, store
}

func TestDispatchTaskSavesBindingBeforeEnqueue(t *testing.T) {
	w, sch, store := newTestWorker(t)

	result, err := w.DispatchTask(context.Background(), automation.DispatchRequest{
		TaskID:            "task-1",
		ProcessInstanceID: "inst-1",
		DefinitionID:      "def-1",
		DefinitionKey:     "order_flow",
		DefinitionName:    "Order Flow",
		DefinitionVersion: 3,
		InstanceName:      "ORDER-1",
		NodeID:            "node-1",
		NodeName:          "NormalizeOrder",
		AutomationTaskKey: "normalize_order", // 自动扫描后 taskKey = snake_case(NormalizeOrder)
		Variables:         map[string]any{"orderId": "O-1"},
		CallbackToken:     "token-1",
		CallbackURL:       "http://workflow/tasks/task-1/complete",
		FailCallbackURL:   "http://workflow/tasks/task-1/fail",
	})
	if err != nil {
		t.Fatalf("dispatch task: %v", err)
	}
	if result.Completed {
		t.Fatalf("dispatch should be async")
	}
	if sch.received.ServiceName != testService || sch.received.MethodName != "NormalizeOrder" {
		t.Fatalf("unexpected asynctask target: %#v", sch.received)
	}
	var arg map[string]any
	if err := json.Unmarshal([]byte(sch.received.Arg), &arg); err != nil {
		t.Fatalf("decode task arg: %v", err)
	}
	if arg["orderId"] != "O-1" {
		t.Fatalf("unexpected task arg: %#v", arg)
	}
	if len(sch.enqueued) != 1 || sch.enqueued[0] != 42 {
		t.Fatalf("task should be enqueued once with id 42, got %#v", sch.enqueued)
	}
	binding, err := store.GetByAsyncTaskID(context.Background(), 42)
	if err != nil {
		t.Fatalf("get binding: %v", err)
	}
	if binding.WorkflowTaskID != "task-1" || binding.ServiceName != testService || binding.MethodName != "NormalizeOrder" {
		t.Fatalf("unexpected binding: %#v", binding)
	}
	if binding.Theme != testTheme {
		t.Fatalf("unexpected binding theme: %#v", binding)
	}
	if binding.Status != domain.AsyncTaskBindingStatusSubmitted || binding.CallbackStatus != domain.AsyncTaskCallbackStatusPending {
		t.Fatalf("unexpected binding status: %#v", binding)
	}
}

func TestPreHandlerInjectsWorkflowContext(t *testing.T) {
	w, _, store := newTestWorker(t)
	ctx := context.Background()
	if err := store.Save(ctx, domain.AsyncTaskBinding{
		AsyncTaskID:       7,
		WorkflowTaskID:    "task-7",
		ProcessInstanceID: "inst-7",
		NodeID:            "node-7",
		AutomationTaskKey: "normalize_order",
		Theme:             testTheme,
		ServiceName:       testService,
		MethodName:        "NormalizeOrder",
	}); err != nil {
		t.Fatalf("save binding: %v", err)
	}

	gaia.BuildContextTrace()
	defer gaia.RemoveContextTrace()
	if err := gaia.NewContextTrace().SetKvData(coretask.ExecutorTaskIdCtxKey, int64(7)); err != nil {
		t.Fatalf("set task id: %v", err)
	}

	if err := w.preHandler()(); err != nil {
		t.Fatalf("pre handler: %v", err)
	}

	taskCtx, ok := automation.ContextFrom(context.Background())
	if !ok {
		t.Fatalf("workflow context not injected")
	}
	if taskCtx.WorkflowTaskID != "task-7" || taskCtx.ProcessInstanceID != "inst-7" || taskCtx.NodeID != "node-7" {
		t.Fatalf("unexpected injected context: %#v", taskCtx)
	}
	binding, _ := store.GetByAsyncTaskID(ctx, 7)
	if binding.Status != domain.AsyncTaskBindingStatusRunning {
		t.Fatalf("binding should be RUNNING, got %s", binding.Status)
	}
}

type recordCallback struct {
	completeCalls int
	failCalls     int
	failErr       error
	lastVars      map[string]any
}

func (c *recordCallback) Complete(_ context.Context, _ domain.AsyncTaskBinding, vars map[string]any) error {
	c.completeCalls++
	c.lastVars = vars
	return nil
}

func (c *recordCallback) Fail(_ context.Context, _ domain.AsyncTaskBinding, _ string) error {
	c.failCalls++
	return c.failErr
}

func setupPostHandler(t *testing.T, asyncTaskID int64, status, errMsg string) (*Worker, *MemoryBindingStore, *recordCallback) {
	t.Helper()
	gaia.RegisterProxy(testTheme, testService, orderWorker{})
	store := NewMemoryBindingStore()
	callback := &recordCallback{}
	w := New(WithBindingStore(store), WithCallbackClient(callback))
	if _, err := w.RegisterService(automation.Service{
		ID:       "worker-under-test",
		Themes:   []string{testTheme},
		Protocol: automation.ProtocolHTTP,
	}); err != nil {
		t.Fatalf("register service: %v", err)
	}
	w.resultVars = func(int64) map[string]any { return map[string]any{"ok": true} }

	ctx := context.Background()
	if err := store.Save(ctx, domain.AsyncTaskBinding{
		AsyncTaskID:    asyncTaskID,
		WorkflowTaskID: fmt.Sprintf("task-%d", asyncTaskID),
		Theme:          testTheme,
		ServiceName:    testService,
		MethodName:     "NormalizeOrder",
		Status:         domain.AsyncTaskBindingStatusRunning,
		CallbackStatus: domain.AsyncTaskCallbackStatusPending,
	}); err != nil {
		t.Fatalf("save binding: %v", err)
	}

	gaia.BuildContextTrace()
	t.Cleanup(gaia.RemoveContextTrace)
	tc := gaia.NewContextTrace()
	_ = tc.SetKvData(coretask.ExecutorTaskIdCtxKey, asyncTaskID)
	_ = tc.SetKvData(coretask.ExecutorTaskStatusCtxKey, status)
	if errMsg != "" {
		_ = tc.SetKvData(coretask.ExecutorTaskErrMsgCtxKey, errMsg)
	}
	return w, store, callback
}

func TestPostHandlerSuccessCallsComplete(t *testing.T) {
	w, store, callback := setupPostHandler(t, 11, coretask.TaskStatusSuccess.String(), "")
	if err := w.postHandler()(); err != nil {
		t.Fatalf("post handler: %v", err)
	}
	if callback.completeCalls != 1 || callback.failCalls != 0 {
		t.Fatalf("expected one complete callback, got %#v", callback)
	}
	if callback.lastVars["ok"] != true {
		t.Fatalf("unexpected callback vars: %#v", callback.lastVars)
	}
	binding, _ := store.GetByAsyncTaskID(context.Background(), 11)
	if binding.Status != domain.AsyncTaskBindingStatusSuccess || binding.CallbackStatus != domain.AsyncTaskCallbackStatusSuccess {
		t.Fatalf("unexpected binding after success: %#v", binding)
	}
}

func TestPostHandlerRetrySkipsCallback(t *testing.T) {
	w, store, callback := setupPostHandler(t, 12, coretask.TaskStatusRetry.String(), "temporary")
	if err := w.postHandler()(); err != nil {
		t.Fatalf("post handler: %v", err)
	}
	if callback.completeCalls != 0 || callback.failCalls != 0 {
		t.Fatalf("retry should not trigger callback: %#v", callback)
	}
	binding, _ := store.GetByAsyncTaskID(context.Background(), 12)
	if binding.Status != domain.AsyncTaskBindingStatusRetry || binding.CallbackStatus != domain.AsyncTaskCallbackStatusPending {
		t.Fatalf("retry should keep callback pending: %#v", binding)
	}
}

func TestPostHandlerFailedCallbackErrorRecorded(t *testing.T) {
	w, store, callback := setupPostHandler(t, 13, coretask.TaskStatusFailed.String(), "final error")
	callback.failErr = fmt.Errorf("callback down")
	if err := w.postHandler()(); err != nil {
		t.Fatalf("post handler: %v", err)
	}
	if callback.failCalls != 1 {
		t.Fatalf("expected one fail callback, got %#v", callback)
	}
	binding, _ := store.GetByAsyncTaskID(context.Background(), 13)
	if binding.Status != domain.AsyncTaskBindingStatusFailed {
		t.Fatalf("binding should be FAILED, got %s", binding.Status)
	}
	if binding.CallbackStatus != domain.AsyncTaskCallbackStatusFailed || binding.LastError != "callback down" {
		t.Fatalf("callback failure should be recorded: %#v", binding)
	}
}
