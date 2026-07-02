package dispatcher

import (
	"context"
	"sync"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	workflowgrpc "github.com/xxzhwl/gaia/components/workflow/transport/grpc"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type directGRPCInvoker struct {
	mu       sync.Mutex
	conns    map[string]*googlegrpc.ClientConn
	dialOpts []googlegrpc.DialOption
}

func newDirectGRPCInvoker(dialOpts ...googlegrpc.DialOption) *directGRPCInvoker {
	return &directGRPCInvoker{
		conns:    map[string]*googlegrpc.ClientConn{},
		dialOpts: dialOpts,
	}
}

// WithGRPCDialOptions 设置默认 gRPC 调用器的拨号选项（如 TLS 凭据）。
//
// 默认调用器在未注入任何选项时使用明文连接（仅适合可信内网）。生产环境应通过本选项
// 注入 grpc.WithTransportCredentials(credentials.NewTLS(...)) 以加密任务入参与回调令牌。
// 注意：若同时使用 WithGRPCInvoker 注入了自定义调用器，则本选项不生效。
func WithGRPCDialOptions(opts ...googlegrpc.DialOption) Option {
	return func(d *Dispatcher) {
		if len(opts) > 0 {
			d.grpcInvoker = newDirectGRPCInvoker(opts...)
		}
	}
}

func (i *directGRPCInvoker) DispatchTask(ctx context.Context, target string, req automation.DispatchRequest) (automation.DispatchResult, error) {
	conn, err := i.conn(target)
	if err != nil {
		return automation.DispatchResult{}, err
	}
	return workflowgrpc.NewAutomationWorkerClient(conn).DispatchTask(ctx, req)
}

func (i *directGRPCInvoker) conn(target string) (*googlegrpc.ClientConn, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if conn := i.conns[target]; conn != nil {
		return conn, nil
	}
	opts := i.dialOpts
	if len(opts) == 0 {
		// 未注入任何拨号选项时回退到明文连接（仅适合可信内网；生产应通过
		// WithGRPCDialOptions 注入 TLS 凭据）。
		opts = []googlegrpc.DialOption{googlegrpc.WithTransportCredentials(insecure.NewCredentials())}
	}
	conn, err := googlegrpc.NewClient(target, opts...)
	if err != nil {
		return nil, err
	}
	i.conns[target] = conn
	return conn, nil
}
