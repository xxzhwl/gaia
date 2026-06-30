package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/framework/httpclient"
)

// HTTPClient 抽象外部任务投递使用的 HTTP 客户端。
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// GRPCInvoker 抽象自动化 worker 的 gRPC 调用能力。
type GRPCInvoker interface {
	DispatchTask(ctx context.Context, target string, req automation.DispatchRequest) (automation.DispatchResult, error)
}

// RuntimePort 定义调度器和工作流运行时之间的交互端口。
type RuntimePort interface {
	ClaimOutboxBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error)
	MarkOutboxSent(ctx context.Context, eventID string) error
	MarkOutboxFailed(ctx context.Context, req engine.MarkOutboxFailedRequest) error
	MarkOutboxDead(ctx context.Context, eventID string) error
	MarkTaskDispatched(ctx context.Context, taskID string) error
	CompleteTask(ctx context.Context, req engine.CompleteTaskRequest) (domain.ProcessInstance, error)
}

// Dispatcher 从 outbox 中领取外部任务事件并投递给 worker。
type Dispatcher struct {
	runtime         RuntimePort
	client          HTTPClient
	grpcInvoker     GRPCInvoker
	registry        automation.Registry
	callbackBaseURL string
	retryDelay      time.Duration
	maxAttempts     int
	pollInterval    time.Duration
	batchSize       int
	running         atomic.Bool
}

// Option 用于配置 Dispatcher。
type Option func(*Dispatcher)

// WithHTTPClient 设置外部任务投递使用的 HTTP 客户端。
func WithHTTPClient(client HTTPClient) Option {
	return func(d *Dispatcher) {
		if client != nil {
			d.client = client
		}
	}
}

// WithGRPCInvoker 设置自动化 worker 的 gRPC 调用器。
func WithGRPCInvoker(invoker GRPCInvoker) Option {
	return func(d *Dispatcher) {
		if invoker != nil {
			d.grpcInvoker = invoker
		}
	}
}

// WithCallbackBaseURL 设置 worker 回调工作流服务的基础地址。
func WithCallbackBaseURL(baseURL string) Option {
	return func(d *Dispatcher) {
		d.callbackBaseURL = strings.TrimRight(baseURL, "/")
	}
}

// WithAutomationRegistry 设置自动化服务注册表，用于解析 gaia:// 端点。
func WithAutomationRegistry(registry automation.Registry) Option {
	return func(d *Dispatcher) {
		d.registry = registry
	}
}

// WithRetryDelay 设置投递失败后的重试间隔。
func WithRetryDelay(delay time.Duration) Option {
	return func(d *Dispatcher) {
		d.retryDelay = delay
	}
}

// WithMaxAttempts 设置 outbox 事件最大投递次数。
func WithMaxAttempts(maxAttempts int) Option {
	return func(d *Dispatcher) {
		d.maxAttempts = maxAttempts
	}
}

// WithPollInterval 设置调度器轮询间隔。
func WithPollInterval(interval time.Duration) Option {
	return func(d *Dispatcher) {
		d.pollInterval = interval
	}
}

// WithBatchSize 设置每次领取 outbox 事件的数量。
func WithBatchSize(batchSize int) Option {
	return func(d *Dispatcher) {
		d.batchSize = batchSize
	}
}

// New 为内存运行时创建外部任务调度器。
func New(runtime *engine.Runtime, opts ...Option) *Dispatcher {
	return NewWithRuntime(memoryRuntimeAdapter{runtime: runtime}, opts...)
}

// NewWithRuntime 为任意实现 RuntimePort 的运行时创建外部任务调度器。
func NewWithRuntime(runtime RuntimePort, opts ...Option) *Dispatcher {
	d := &Dispatcher{
		runtime:         runtime,
		client:          gaiaHTTPClient{},
		grpcInvoker:     newDirectGRPCInvoker(),
		callbackBaseURL: "",
		retryDelay:      10 * time.Second,
		maxAttempts:     3,
		pollInterval:    2 * time.Second,
		batchSize:       20,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// DispatchPending 立即领取并投递一批待处理 outbox 事件。
func (d *Dispatcher) DispatchPending(ctx context.Context, limit int) (int, error) {
	events, err := d.runtime.ClaimOutboxBatch(ctx, limit)
	if err != nil {
		return 0, err
	}
	var firstErr error
	success := 0
	for _, event := range events {
		if err := d.dispatchOne(ctx, event); err != nil {
			_ = d.markFailedOrDead(ctx, event)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}
	return success, firstErr
}

// Run 按轮询间隔持续投递 outbox 事件，直到上下文结束。
func (d *Dispatcher) Run(ctx context.Context) error {
	if !d.running.CompareAndSwap(false, true) {
		return fmt.Errorf("dispatcher is already running")
	}
	defer d.running.Store(false)

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		if _, err := d.DispatchPending(ctx, d.batchSize); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) markFailedOrDead(ctx context.Context, event domain.OutboxEvent) error {
	if d.maxAttempts > 0 && event.RetryCount+1 >= d.maxAttempts {
		return d.runtime.MarkOutboxDead(ctx, event.ID)
	}
	nextRetryAt := time.Now().UTC().Add(d.retryDelay)
	return d.runtime.MarkOutboxFailed(ctx, engine.MarkOutboxFailedRequest{
		EventID:     event.ID,
		NextRetryAt: &nextRetryAt,
	})
}

func (d *Dispatcher) dispatchOne(ctx context.Context, event domain.OutboxEvent) error {
	dispatchURL, _ := event.Payload["dispatchUrl"].(string)
	if dispatchURL == "" {
		return fmt.Errorf("outbox event %s dispatchUrl is empty", event.ID)
	}
	if d.registry != nil {
		if strings.HasPrefix(dispatchURL, "gaia://") {
			service, task, err := d.registry.ResolveTask(ctx, dispatchURL)
			if err != nil {
				return err
			}
			if automation.NormalizeProtocol(service.Protocol) == automation.ProtocolGRPC {
				return d.dispatchGRPC(ctx, event, service, task)
			}
			if strings.TrimSpace(task.Endpoint) != "" {
				dispatchURL = task.Endpoint
			} else {
				dispatchURL = joinURL(service.BaseURL, task.Key)
			}
		} else {
			resolved, err := d.registry.ResolveEndpoint(ctx, dispatchURL)
			if err != nil {
				return err
			}
			dispatchURL = resolved
		}
	}
	taskID, _ := event.Payload["taskId"].(string)
	if taskID == "" {
		return fmt.Errorf("outbox event %s taskId is empty", event.ID)
	}

	body := copyPayload(event.Payload)
	delete(body, "dispatchUrl")
	if d.callbackBaseURL != "" {
		body["callbackUrl"] = fmt.Sprintf("%s/api/workflow/tasks/%s/complete", d.callbackBaseURL, taskID)
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dispatchURL, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dispatch event %s failed with status %d", event.ID, resp.StatusCode)
	}
	if err := d.runtime.MarkTaskDispatched(ctx, taskID); err != nil {
		return err
	}
	return d.runtime.MarkOutboxSent(ctx, event.ID)
}

func (d *Dispatcher) dispatchGRPC(ctx context.Context, event domain.OutboxEvent, service automation.Service, task automation.Task) error {
	target := strings.TrimSpace(task.Endpoint)
	if target == "" {
		target = strings.TrimSpace(service.BaseURL)
	}
	if target == "" {
		return fmt.Errorf("automation service %s grpc target is empty", service.ID)
	}
	req, err := dispatchRequestFromPayload(event.Payload)
	if err != nil {
		return err
	}
	if req.AutomationTaskKey == "" {
		req.AutomationTaskKey = task.Key
	}
	if d.callbackBaseURL != "" && req.CallbackURL == "" {
		req.CallbackURL = fmt.Sprintf("%s/api/workflow/tasks/%s/complete", d.callbackBaseURL, req.TaskID)
	}
	result, err := d.grpcInvoker.DispatchTask(ctx, target, req)
	if err != nil {
		return err
	}
	if err := d.runtime.MarkTaskDispatched(ctx, req.TaskID); err != nil {
		return err
	}
	if result.Completed {
		if _, err := d.runtime.CompleteTask(ctx, engine.CompleteTaskRequest{
			TaskID:    req.TaskID,
			Variables: result.Variables,
		}); err != nil {
			return err
		}
	}
	return d.runtime.MarkOutboxSent(ctx, event.ID)
}

type gaiaHTTPClient struct{}

func (gaiaHTTPClient) Do(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		defer req.Body.Close()
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = raw
	}
	respBody, statusCode, err := httpclient.NewHttpRequest(req.URL.String()).
		WithContext(req.Context()).
		WithMethod(req.Method).
		WithHeader(req.Header.Clone()).
		WithBody(body).
		WithRetryTimes(0).
		WithTitle("workflow-dispatcher").
		Do()
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(string(respBody))),
	}, nil
}

type memoryRuntimeAdapter struct {
	runtime *engine.Runtime
}

func (a memoryRuntimeAdapter) ClaimOutboxBatch(_ context.Context, limit int) ([]domain.OutboxEvent, error) {
	return a.runtime.ClaimOutboxBatch(limit), nil
}

func (a memoryRuntimeAdapter) MarkOutboxSent(_ context.Context, eventID string) error {
	return a.runtime.MarkOutboxSent(eventID)
}

func (a memoryRuntimeAdapter) MarkOutboxFailed(_ context.Context, req engine.MarkOutboxFailedRequest) error {
	return a.runtime.MarkOutboxFailed(req)
}

func (a memoryRuntimeAdapter) MarkOutboxDead(_ context.Context, eventID string) error {
	return a.runtime.MarkOutboxDead(eventID)
}

func (a memoryRuntimeAdapter) MarkTaskDispatched(_ context.Context, taskID string) error {
	return a.runtime.MarkTaskDispatched(taskID)
}

func (a memoryRuntimeAdapter) CompleteTask(ctx context.Context, req engine.CompleteTaskRequest) (domain.ProcessInstance, error) {
	return a.runtime.CompleteTask(ctx, req)
}

func copyPayload(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for k, v := range input {
		result[k] = v
	}
	return result
}

func dispatchRequestFromPayload(payload map[string]any) (automation.DispatchRequest, error) {
	taskID, _ := payload["taskId"].(string)
	if taskID == "" {
		return automation.DispatchRequest{}, fmt.Errorf("dispatch payload taskId is empty")
	}
	variables, _ := payload["variables"].(map[string]any)
	return automation.DispatchRequest{
		TaskID:            taskID,
		ProcessInstanceID: stringPayload(payload, "processInstanceId"),
		NodeID:            stringPayload(payload, "nodeId"),
		AutomationTaskKey: stringPayload(payload, "automationTaskKey"),
		Variables:         variables,
		CallbackToken:     stringPayload(payload, "callbackToken"),
		CallbackURL:       stringPayload(payload, "callbackUrl"),
	}, nil
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func joinURL(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	path = strings.TrimLeft(path, "/")
	if baseURL == "" || path == "" {
		return baseURL + path
	}
	return baseURL + "/" + path
}
