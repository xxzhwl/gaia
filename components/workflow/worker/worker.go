package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	coretask "github.com/xxzhwl/gaia/components/asynctask"
	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
)

// Worker 是 workflow 执行侧的门面：接收 engine 下发的自动化任务，交由 asynctask 执行。
//
// 使用方式（极简 3 步）：
//  1. 业务包 init() 里执行 gaia.RegisterProxy(theme, serviceName, proxy) 挂上业务对象；
//  2. w := worker.New(worker.WithCallbackClient(...))
//  3. svc, _ := w.RegisterService(automation.Service{ID, BaseURL, Protocol, Themes: []string{theme}})
//     w.Start(ctx)
//     然后把 svc 提交给 engine 完成注册。
//
// Worker 会：反射列出 theme 下每个 proxy 的全部导出方法 → 生成 automation.Task 清单
// （taskKey = snake_case(方法名)、Description = 方法源码注释）→ 拉起 asynctask 调度器。
//
// 它实现了 transport/grpc.AutomationWorker（DispatchTask/Health），既可注册为 gRPC 服务，
// 也可被 HTTP 传输层复用同一套逻辑。
type Worker struct {
	catalog      *automation.MethodCatalog
	bindingStore BindingStore
	callback     CallbackClient
	maxRetryTime int
	bootstrap    bool

	schedulerOpts []coretask.SchedulerOption

	baseCtx    context.Context
	mu         sync.Mutex
	schedulers map[string]*coretask.Scheduler

	// 以下为可替换接缝，默认对接真实 asynctask，测试可注入 fake 以摆脱 DB 依赖。
	resolveScheduler func(theme string) schedulerPort
	resultVars       func(taskID int64) map[string]any
}

// schedulerPort 抽象 worker 依赖的 asynctask 调度器能力（*coretask.Scheduler 满足它）。
type schedulerPort interface {
	ReceiveTask(task coretask.TaskBaseInfo) (coretask.TaskModel, error)
	TaskQuickQueue(taskID int64)
}

// Option 配置 Worker。
type Option func(*Worker)

// WithBindingStore 设置 workflow/asynctask 绑定仓储（默认内存实现）。
func WithBindingStore(store BindingStore) Option {
	return func(w *Worker) {
		if store != nil {
			w.bindingStore = store
		}
	}
}

// WithCallbackClient 设置回调 engine 的客户端（默认 HTTP 回调）。
func WithCallbackClient(client CallbackClient) Option {
	return func(w *Worker) {
		if client != nil {
			w.callback = client
		}
	}
}

// WithMaxRetryTime 设置 asynctask 业务执行最大重试次数。
func WithMaxRetryTime(maxRetry int) Option {
	return func(w *Worker) {
		if maxRetry >= 0 {
			w.maxRetryTime = maxRetry
		}
	}
}

// WithSchedulerOptions 追加自定义的 asynctask 调度器选项（worker 数、扫描间隔等）。
func WithSchedulerOptions(opts ...coretask.SchedulerOption) Option {
	return func(w *Worker) {
		w.schedulerOpts = append(w.schedulerOpts, opts...)
	}
}

// WithBootstrap 是否在启动调度器前自动迁移 asynctask 相关表。
func WithBootstrap(enabled bool) Option {
	return func(w *Worker) {
		w.bootstrap = enabled
	}
}

// New 创建 worker 门面。
func New(opts ...Option) *Worker {
	w := &Worker{
		catalog:      automation.NewMethodCatalog(),
		bindingStore: NewMemoryBindingStore(),
		callback:     HTTPCallbackClient{},
		baseCtx:      context.Background(),
		schedulers:   map[string]*coretask.Scheduler{},
	}
	for _, opt := range opts {
		opt(w)
	}
	if w.resolveScheduler == nil {
		w.resolveScheduler = func(theme string) schedulerPort { return w.ensureScheduler(theme) }
	}
	if w.resultVars == nil {
		w.resultVars = w.fetchResultVariables
	}
	return w
}

// Start 启动 worker：为已注册的全部 theme 拉起 asynctask 调度器（核心执行层）。
func (w *Worker) Start(ctx context.Context) {
	if ctx != nil {
		w.baseCtx = ctx
	}
	for _, theme := range w.registeredThemes() {
		w.ensureScheduler(theme)
	}
}

// DispatchTask 接收 workflow 下发任务：构造入参 -> 提交 asynctask -> 存关联表 -> 触发执行。
func (w *Worker) DispatchTask(ctx context.Context, req automation.DispatchRequest) (automation.DispatchResult, error) {
	if w == nil || w.catalog == nil {
		return automation.DispatchResult{}, fmt.Errorf("workflow worker catalog is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(req.TaskID) == "" {
		return automation.DispatchResult{}, fmt.Errorf("workflow task id is required")
	}
	if strings.TrimSpace(req.AutomationTaskKey) == "" {
		return automation.DispatchResult{}, fmt.Errorf("automation task key is required")
	}

	binding, ok := w.catalog.Binding(req.AutomationTaskKey)
	if !ok {
		return automation.DispatchResult{}, fmt.Errorf("automation task %s binding not found", req.AutomationTaskKey)
	}
	raw, err := w.catalog.BuildArgsJSON(req.AutomationTaskKey, req.Variables)
	if err != nil {
		return automation.DispatchResult{}, err
	}

	taskName := fmt.Sprintf("%s:%s", req.AutomationTaskKey, req.TaskID)
	scheduler := w.resolveScheduler(binding.Theme)

	// ① 下发给 asynctask（先落库拿到 asyncTaskID，此时任务尚未入队执行）。
	model, err := scheduler.ReceiveTask(coretask.TaskBaseInfo{
		ServiceName:  binding.ServiceName,
		MethodName:   binding.MethodName,
		TaskName:     taskName,
		Arg:          string(raw),
		MaxRetryTime: w.maxRetryTime,
	})
	if err != nil {
		return automation.DispatchResult{}, err
	}

	// ② 先把流程任务与异步任务的关联存入关联表（独立于 Pre/PostHandler），
	//    保证 PreHandler 触发时一定能查到绑定。
	if err := w.bindingStore.Save(ctx, bindingFromDispatch(model.Id, binding, req)); err != nil {
		return automation.DispatchResult{}, fmt.Errorf("save workflow async task binding failed: %w", err)
	}

	// ③ 触发执行。
	scheduler.TaskQuickQueue(model.Id)

	return automation.DispatchResult{
		TaskID:            req.TaskID,
		ProcessInstanceID: req.ProcessInstanceID,
		NodeID:            req.NodeID,
		Completed:         false,
	}, nil
}

// Health 返回 worker 状态。
func (w *Worker) Health(context.Context) (map[string]any, error) {
	w.mu.Lock()
	themes := make([]string, 0, len(w.schedulers))
	for theme := range w.schedulers {
		themes = append(themes, theme)
	}
	w.mu.Unlock()
	return map[string]any{
		"bindings":     len(w.catalog.Bindings()),
		"maxRetryTime": w.maxRetryTime,
		"themes":       themes,
	}, nil
}

// bindingFromDispatch 由下发请求构造一条绑定记录。
func bindingFromDispatch(asyncTaskID int64, binding automation.MethodBinding, req automation.DispatchRequest) domain.AsyncTaskBinding {
	now := time.Now().UTC()
	return domain.AsyncTaskBinding{
		AsyncTaskID:         asyncTaskID,
		WorkflowTaskID:      req.TaskID,
		ProcessInstanceID:   req.ProcessInstanceID,
		DefinitionID:        req.DefinitionID,
		DefinitionKey:       req.DefinitionKey,
		DefinitionName:      req.DefinitionName,
		DefinitionVersion:   req.DefinitionVersion,
		InstanceName:        req.InstanceName,
		NodeID:              req.NodeID,
		NodeName:            req.NodeName,
		AutomationTaskKey:   req.AutomationTaskKey,
		Theme:               binding.Theme,
		ServiceName:         binding.ServiceName,
		MethodName:          binding.MethodName,
		CallbackToken:       req.CallbackToken,
		CompleteCallbackURL: req.CallbackURL,
		FailCallbackURL:     req.FailCallbackURL,
		Status:              domain.AsyncTaskBindingStatusSubmitted,
		CallbackStatus:      domain.AsyncTaskCallbackStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}
