package dispatcher

import (
	"context"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	workflowgrpc "github.com/xxzhwl/gaia/components/workflow/transport/grpc"
	"github.com/xxzhwl/gaia/framework/rpcclient"
)

// grpcClientInvoker 基于 rpcclient.GrpcClient 的 gRPC 调用器，提供连接复用、
// 链路追踪、指标、日志、TLS/KeepAlive 配置等能力。
type grpcClientInvoker struct {
	client *rpcclient.GrpcClient
}

func newGRPCClientInvoker() *grpcClientInvoker {
	return &grpcClientInvoker{
		client: rpcclient.New("WorkflowDispatcher", nil),
	}
}

// WithGRPCClient 注入自定义 gRPC 客户端管理器，覆盖默认的 rpcclient.GrpcClient。
//
// 默认的 GrpcClient 走直连模式（无服务发现），配置从 gaia 配置中心的 WorkflowDispatcher
// 节读取（TLS、KeepAlive、重试、消息大小等）。调用方可注入带服务发现或其他自定义配置的
// GrpcClient 来覆盖此行为。
func WithGRPCClient(client *rpcclient.GrpcClient) Option {
	return func(d *Dispatcher) {
		if client != nil {
			d.grpcInvoker = &grpcClientInvoker{client: client}
		}
	}
}

func (i *grpcClientInvoker) DispatchTask(ctx context.Context, target string, req automation.DispatchRequest) (automation.DispatchResult, error) {
	conn, err := i.client.DialDirect(target)
	if err != nil {
		return automation.DispatchResult{}, err
	}
	return workflowgrpc.NewAutomationWorkerClient(conn).DispatchTask(ctx, req)
}
