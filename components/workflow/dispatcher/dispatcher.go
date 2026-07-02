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

	"github.com/xxzhwl/gaia"
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
}

// Alert 描述外部任务 outbox 投递失败或死信告警。
type Alert struct {
	EventID     string
	EventType   string
	TaskID      string
	Status      domain.OutboxStatus
	RetryCount  int
	DispatchURL string
	Reason      string
}

// AlertHandler 处理外部任务投递告警。
type AlertHandler interface {
	Send(ctx context.Context, alert Alert) error
}

// AlertHandlerFunc 允许用函数快速实现 AlertHandler。
type AlertHandlerFunc func(ctx context.Context, alert Alert) error

// Send 调用底层函数发送告警。
func (fn AlertHandlerFunc) Send(ctx context.Context, alert Alert) error {
	return fn(ctx, alert)
}

// Dispatcher 从 outbox 中领取外部任务事件并投递给 worker。
type Dispatcher struct {
	runtime         RuntimePort
	client          HTTPClient
	grpcInvoker     GRPCInvoker
	alertHandler    AlertHandler
	registry        automation.Registry
	endpointGuard   EndpointValidator
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

// WithAlertHandler 设置外部任务投递失败/死信告警处理器。
func WithAlertHandler(handler AlertHandler) Option {
	return func(d *Dispatcher) {
		if handler != nil {
			d.alertHandler = handler
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

// WithEndpointValidator 设置出站地址校验器，用于防护 SSRF。
//
// 传入 nil 表示关闭出站校验（不推荐用于生产）。默认使用 DefaultEndpointGuard 拒绝
// 环回、私有网段与 link-local 地址。
func WithEndpointValidator(validator EndpointValidator) Option {
	return func(d *Dispatcher) {
		d.endpointGuard = validator
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
		grpcInvoker:     newGRPCClientInvoker(),
		alertHandler:    defaultAlertHandler{},
		endpointGuard:   NewDefaultEndpointGuard(),
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
			_ = d.markFailedOrDead(ctx, event, err)
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

func (d *Dispatcher) markFailedOrDead(ctx context.Context, event domain.OutboxEvent, reason error) error {
	maxAttempts := d.maxAttempts
	retryDelay := d.retryDelay
	// 节点级 RetryPolicy 覆盖全局默认值（MaxAttempts 和 BackoffSeconds 均在 outbox payload 中）。
	if v, ok := intPayload(event.Payload, "retryMaxAttempts"); ok && v > 0 {
		maxAttempts = v
	}
	if v, ok := intPayload(event.Payload, "retryBackoffSeconds"); ok && v > 0 {
		retryDelay = time.Duration(v) * time.Second
	}
	if maxAttempts > 0 && event.RetryCount+1 >= maxAttempts {
		err := d.runtime.MarkOutboxDead(ctx, event.ID)
		d.sendAlert(ctx, event, domain.OutboxStatusDead, reason)
		return err
	}
	nextRetryAt := time.Now().UTC().Add(retryDelay)
	err := d.runtime.MarkOutboxFailed(ctx, engine.MarkOutboxFailedRequest{
		EventID:     event.ID,
		NextRetryAt: &nextRetryAt,
	})
	d.sendAlert(ctx, event, domain.OutboxStatusFailed, reason)
	return err
}

func intPayload(payload map[string]any, key string) (int, bool) {
	v, ok := payload[key]
	if !ok {
		return 0, false
	}
	switch val := v.(type) {
	case int:
		return val, true
	case float64:
		return int(val), true
	case int64:
		return int(val), true
	}
	return 0, false
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
	if d.endpointGuard != nil {
		if err := d.endpointGuard.ValidateHTTP(dispatchURL); err != nil {
			return fmt.Errorf("outbox event %s dispatch url rejected: %w", event.ID, err)
		}
	}

	body := copyPayload(event.Payload)
	delete(body, "dispatchUrl")
	body["dispatchMode"] = "ASYNC"
	if d.callbackBaseURL != "" {
		body["callbackUrl"] = fmt.Sprintf("%s/api/workflow/tasks/%s/complete", d.callbackBaseURL, taskID)
		body["failCallbackUrl"] = fmt.Sprintf("%s/api/workflow/tasks/%s/fail", d.callbackBaseURL, taskID)
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
	if err := checkGaiaResponse(resp.Body, fmt.Sprintf("dispatch event %s", event.ID)); err != nil {
		return err
	}
	if err := d.runtime.MarkTaskDispatched(ctx, taskID); err != nil {
		return err
	}
	return d.runtime.MarkOutboxSent(ctx, event.ID)
}

type gaiaResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func checkGaiaResponse(body io.Reader, operation string) error {
	if body == nil {
		return nil
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var resp gaiaResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil
	}
	if resp.Code != 0 {
		if resp.Msg == "" {
			resp.Msg = "gaia response code is not zero"
		}
		return fmt.Errorf("%s failed: %s", operation, resp.Msg)
	}
	return nil
}

func (d *Dispatcher) dispatchGRPC(ctx context.Context, event domain.OutboxEvent, service automation.Service, task automation.Task) error {
	target := strings.TrimSpace(task.Endpoint)
	if target == "" {
		target = strings.TrimSpace(service.BaseURL)
	}
	if target == "" {
		return fmt.Errorf("automation service %s grpc target is empty", service.ID)
	}
	if d.endpointGuard != nil {
		if err := d.endpointGuard.ValidateGRPC(target); err != nil {
			return fmt.Errorf("automation service %s grpc target rejected: %w", service.ID, err)
		}
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
	if d.callbackBaseURL != "" && req.FailCallbackURL == "" {
		req.FailCallbackURL = fmt.Sprintf("%s/api/workflow/tasks/%s/fail", d.callbackBaseURL, req.TaskID)
	}
	req.DispatchMode = "ASYNC"
	_, err = d.grpcInvoker.DispatchTask(ctx, target, req)
	if err != nil {
		return err
	}
	if err := d.runtime.MarkTaskDispatched(ctx, req.TaskID); err != nil {
		return err
	}
	return d.runtime.MarkOutboxSent(ctx, event.ID)
}

func (d *Dispatcher) sendAlert(ctx context.Context, event domain.OutboxEvent, status domain.OutboxStatus, reason error) {
	if d.alertHandler == nil {
		return
	}
	taskID, _ := event.Payload["taskId"].(string)
	if taskID == "" {
		taskID = event.AggregateID
	}
	dispatchURL, _ := event.Payload["dispatchUrl"].(string)
	reasonText := ""
	if reason != nil {
		reasonText = reason.Error()
	}
	_ = d.alertHandler.Send(ctx, Alert{
		EventID:     event.ID,
		EventType:   event.EventType,
		TaskID:      taskID,
		Status:      status,
		RetryCount:  event.RetryCount + 1,
		DispatchURL: dispatchURL,
		Reason:      reasonText,
	})
}

type defaultAlertHandler struct{}

func (defaultAlertHandler) Send(_ context.Context, alert Alert) error {
	title := fmt.Sprintf("Workflow outbox %s", alert.Status)
	content := fmt.Sprintf("event_id: %s\nevent_type: %s\ntask_id: %s\ndispatch_url: %s\nretry_count: %d\nreason: %s",
		alert.EventID, alert.EventType, alert.TaskID, alert.DispatchURL, alert.RetryCount, alert.Reason)
	return gaia.SendSystemAlarm(title, content)
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
		DefinitionID:      stringPayload(payload, "definitionId"),
		DefinitionKey:     stringPayload(payload, "definitionKey"),
		DefinitionName:    stringPayload(payload, "definitionName"),
		DefinitionVersion: intPayloadDefault(payload, "definitionVersion"),
		InstanceName:      stringPayload(payload, "instanceName"),
		NodeID:            stringPayload(payload, "nodeId"),
		NodeName:          stringPayload(payload, "nodeName"),
		AutomationTaskKey: stringPayload(payload, "automationTaskKey"),
		Variables:         variables,
		CallbackToken:     stringPayload(payload, "callbackToken"),
		CallbackURL:       stringPayload(payload, "callbackUrl"),
		FailCallbackURL:   stringPayload(payload, "failCallbackUrl"),
		DispatchMode:      stringPayload(payload, "dispatchMode"),
	}, nil
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func intPayloadDefault(payload map[string]any, key string) int {
	value, _ := intPayload(payload, key)
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
