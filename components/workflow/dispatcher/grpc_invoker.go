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
	mu    sync.Mutex
	conns map[string]*googlegrpc.ClientConn
}

func newDirectGRPCInvoker() *directGRPCInvoker {
	return &directGRPCInvoker{conns: map[string]*googlegrpc.ClientConn{}}
}

func (i *directGRPCInvoker) DispatchTask(ctx context.Context, target string, req automation.DispatchRequest) (automation.DispatchResult, error) {
	conn, err := i.conn(target)
	if err != nil {
		return automation.DispatchResult{}, err
	}
	var out automation.DispatchResult
	err = conn.Invoke(ctx,
		"/"+workflowgrpc.AutomationWorkerServiceName+"/"+workflowgrpc.AutomationWorkerDispatchMethod,
		req,
		&out,
		googlegrpc.CallContentSubtype("json"),
	)
	return out, err
}

func (i *directGRPCInvoker) conn(target string) (*googlegrpc.ClientConn, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if conn := i.conns[target]; conn != nil {
		return conn, nil
	}
	conn, err := googlegrpc.NewClient(target, googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	i.conns[target] = conn
	return conn, nil
}
