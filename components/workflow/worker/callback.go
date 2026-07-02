package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xxzhwl/gaia/components/workflow/domain"
	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/framework/httpclient"
)

// callbackTokenHeader 是外部 worker 回调完成任务时携带回调令牌的请求头。
const callbackTokenHeader = "X-Workflow-Callback-Token"

// CallbackClient 把 asynctask 终态回调给 workflow-engine（完成或失败）。
//
// 提供三种实现，按部署形态按需选择：
//   - HTTPCallbackClient：跨进程、engine 暴露 HTTP。
//   - GRPCCallbackClient：跨进程、engine 暴露 gRPC。
//   - EngineCallbackClient：worker 内嵌 engine 同进程，直连调用，零网络。
type CallbackClient interface {
	Complete(ctx context.Context, binding domain.AsyncTaskBinding, variables map[string]any) error
	Fail(ctx context.Context, binding domain.AsyncTaskBinding, errMsg string) error
}

// ---------------------------------------------------------------------------
// HTTP 回调
// ---------------------------------------------------------------------------

// HTTPCallbackClient 用绑定里保存的 callback URL 回调 engine。
type HTTPCallbackClient struct{}

// Complete 回调 workflow 完成任务。
func (HTTPCallbackClient) Complete(ctx context.Context, binding domain.AsyncTaskBinding, variables map[string]any) error {
	url := strings.TrimSpace(binding.CompleteCallbackURL)
	if url == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"taskId":        binding.WorkflowTaskID,
		"variables":     variables,
		"callbackToken": binding.CallbackToken,
	})
	if err != nil {
		return err
	}
	_, status, err := httpclient.NewHttpRequest(url).
		WithContext(ctx).
		AddHeader(callbackTokenHeader, binding.CallbackToken).
		WithTitle("workflow-worker-complete").
		Post(body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("workflow complete callback status %d", status)
	}
	return nil
}

// Fail 回调 workflow 任务失败。
func (HTTPCallbackClient) Fail(ctx context.Context, binding domain.AsyncTaskBinding, errMsg string) error {
	url := strings.TrimSpace(binding.FailCallbackURL)
	if url == "" && strings.HasSuffix(binding.CompleteCallbackURL, "/complete") {
		url = strings.TrimSuffix(binding.CompleteCallbackURL, "/complete") + "/fail"
	}
	if url == "" {
		return nil
	}
	body, err := json.Marshal(map[string]any{
		"taskId":        binding.WorkflowTaskID,
		"message":       errMsg,
		"retryable":     false,
		"callbackToken": binding.CallbackToken,
	})
	if err != nil {
		return err
	}
	_, status, err := httpclient.NewHttpRequest(url).
		WithContext(ctx).
		AddHeader(callbackTokenHeader, binding.CallbackToken).
		WithTitle("workflow-worker-fail").
		Post(body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("workflow fail callback status %d", status)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 进程内直连回调
// ---------------------------------------------------------------------------

// EngineCallback 抽象 worker 需要回调的 engine 能力。
//
// *workflow.Engine 与 engine.Runtime 均满足该接口，可直接作为内嵌回调目标传入，
// 从而避免 worker 直接依赖 workflow 根包。
type EngineCallback interface {
	CompleteTask(ctx context.Context, req workflowengine.CompleteTaskRequest) (domain.ProcessInstance, error)
	FailTask(ctx context.Context, req workflowengine.FailTaskRequest) (domain.ProcessInstance, error)
}

// EngineCallbackClient 在同进程内直接调用 engine 完成/失败任务。
type EngineCallbackClient struct {
	Engine EngineCallback
}

// NewEngineCallbackClient 创建进程内直连回调客户端。
func NewEngineCallbackClient(engine EngineCallback) *EngineCallbackClient {
	return &EngineCallbackClient{Engine: engine}
}

// Complete 直连完成任务。
func (c *EngineCallbackClient) Complete(ctx context.Context, binding domain.AsyncTaskBinding, variables map[string]any) error {
	if c == nil || c.Engine == nil {
		return fmt.Errorf("engine callback is nil")
	}
	_, err := c.Engine.CompleteTask(ctx, workflowengine.CompleteTaskRequest{
		TaskID:        binding.WorkflowTaskID,
		Variables:     variables,
		CallbackToken: binding.CallbackToken,
	})
	return err
}

// Fail 直连标记任务失败。
func (c *EngineCallbackClient) Fail(ctx context.Context, binding domain.AsyncTaskBinding, errMsg string) error {
	if c == nil || c.Engine == nil {
		return fmt.Errorf("engine callback is nil")
	}
	_, err := c.Engine.FailTask(ctx, workflowengine.FailTaskRequest{
		TaskID:        binding.WorkflowTaskID,
		Message:       errMsg,
		Retryable:     false,
		CallbackToken: binding.CallbackToken,
	})
	return err
}

// ---------------------------------------------------------------------------
// gRPC 回调
// ---------------------------------------------------------------------------

// GRPCEngineClient 抽象 workflow gRPC 客户端所需的能力。
//
// transport/grpc.Client 满足该接口（其方法带可变 CallOption，Go 结构化匹配允许）。
type GRPCEngineClient interface {
	CompleteTask(ctx context.Context, req workflowengine.CompleteTaskRequest) (domain.ProcessInstance, error)
	FailTask(ctx context.Context, req workflowengine.FailTaskRequest) (domain.ProcessInstance, error)
}

// GRPCCallbackClient 通过 workflow gRPC 客户端回调 engine。
//
// 由于 transport/grpc.Client 的方法签名带有可变 CallOption 参数，不能直接满足
// GRPCEngineClient，需用 gRPCClientAdapter 适配后传入（见 NewGRPCCallbackClient）。
type GRPCCallbackClient struct {
	Client GRPCEngineClient
}

// NewGRPCCallbackClient 创建 gRPC 回调客户端。
func NewGRPCCallbackClient(client GRPCEngineClient) *GRPCCallbackClient {
	return &GRPCCallbackClient{Client: client}
}

// Complete 通过 gRPC 完成任务。
func (c *GRPCCallbackClient) Complete(ctx context.Context, binding domain.AsyncTaskBinding, variables map[string]any) error {
	if c == nil || c.Client == nil {
		return fmt.Errorf("grpc callback client is nil")
	}
	_, err := c.Client.CompleteTask(ctx, workflowengine.CompleteTaskRequest{
		TaskID:        binding.WorkflowTaskID,
		Variables:     variables,
		CallbackToken: binding.CallbackToken,
	})
	return err
}

// Fail 通过 gRPC 标记任务失败。
func (c *GRPCCallbackClient) Fail(ctx context.Context, binding domain.AsyncTaskBinding, errMsg string) error {
	if c == nil || c.Client == nil {
		return fmt.Errorf("grpc callback client is nil")
	}
	_, err := c.Client.FailTask(ctx, workflowengine.FailTaskRequest{
		TaskID:        binding.WorkflowTaskID,
		Message:       errMsg,
		Retryable:     false,
		CallbackToken: binding.CallbackToken,
	})
	return err
}

var (
	_ CallbackClient = HTTPCallbackClient{}
	_ CallbackClient = (*EngineCallbackClient)(nil)
	_ CallbackClient = (*GRPCCallbackClient)(nil)
)
