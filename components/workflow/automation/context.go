package automation

import (
	"context"

	"github.com/xxzhwl/gaia"
)

const taskContextTraceKey = "WorkflowTaskContext"

type taskContextKey struct{}

// WorkflowTaskContext 描述当前 asynctask 正在处理的 workflow 外部任务上下文。
//
// 该上下文由 worker 的 PreHandler 在业务方法执行前注入，业务方法可通过
// ContextFrom 直接读取所属流程实例、节点等元信息。
type WorkflowTaskContext struct {
	WorkflowTaskID    string
	ProcessInstanceID string
	DefinitionID      string
	DefinitionKey     string
	DefinitionName    string
	DefinitionVersion int
	InstanceName      string
	NodeID            string
	NodeName          string
	AutomationTaskKey string
	AsyncTaskID       int64
}

// WithTaskContext 将 workflow 任务上下文写入标准 context。
func WithTaskContext(ctx context.Context, taskCtx WorkflowTaskContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, taskContextKey{}, taskCtx)
}

// ContextFrom 从标准 context 或 Gaia ContextTrace 中读取 workflow 任务上下文。
func ContextFrom(ctx context.Context) (WorkflowTaskContext, bool) {
	if ctx != nil {
		if taskCtx, ok := ctx.Value(taskContextKey{}).(WorkflowTaskContext); ok {
			return taskCtx, true
		}
	}
	value := gaia.NewContextTrace().GetKvField(taskContextTraceKey)
	taskCtx, ok := value.(WorkflowTaskContext)
	return taskCtx, ok
}

// SetTaskContextTrace 将 workflow 任务上下文写入当前 goroutine 的 Gaia ContextTrace。
func SetTaskContextTrace(taskCtx WorkflowTaskContext) error {
	return gaia.NewContextTrace().SetKvData(taskContextTraceKey, taskCtx)
}
