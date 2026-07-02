package worker

import (
	"context"
	"encoding/json"

	"github.com/xxzhwl/gaia"
	coretask "github.com/xxzhwl/gaia/components/asynctask"
	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
)

// preHandler 返回注入到 asynctask 调度器的前置处理器。
//
// 在业务方法执行前：读取当前 asynctask 任务 ID -> 查绑定 -> 把流程元信息写入当前
// goroutine 的 ContextTrace（业务方法通过 automation.ContextFrom 读取）-> 标记 RUNNING。
// 非本 worker 下发（查不到绑定）的任务会被安全跳过，不影响其执行。
func (w *Worker) preHandler() coretask.PreHandlerFunc {
	return func() error {
		ctx := context.Background()
		taskID, ok := currentAsyncTaskID()
		if !ok {
			return nil
		}
		binding, err := w.bindingStore.GetByAsyncTaskID(ctx, taskID)
		if err != nil {
			// 查不到绑定说明不是 workflow 下发的任务，跳过上下文注入。
			return nil
		}
		if err := automation.SetTaskContextTrace(contextFromBinding(binding)); err != nil {
			gaia.ErrorF("workflow worker set task context failed: %v", err)
		}
		if err := w.bindingStore.MarkTaskStatus(ctx, taskID, domain.AsyncTaskBindingStatusRunning, ""); err != nil {
			gaia.ErrorF("workflow worker mark running failed: %v", err)
		}
		return nil
	}
}

// postHandler 返回注入到 asynctask 调度器的后置处理器。
//
// 在业务方法执行后：读取终态 -> Retry 只更新绑定状态、不回调；Success/Failed 更新绑定
// 状态并回调 engine（完成/失败）。回调失败仅记录 CallbackStatus=FAILED，不牵连业务判定。
func (w *Worker) postHandler() coretask.PostHandlerFunc {
	return func() error {
		ctx := context.Background()
		taskID, ok := currentAsyncTaskID()
		if !ok {
			return nil
		}
		binding, err := w.bindingStore.GetByAsyncTaskID(ctx, taskID)
		if err != nil {
			return nil
		}
		status := currentString(coretask.ExecutorTaskStatusCtxKey)
		errMsg := currentString(coretask.ExecutorTaskErrMsgCtxKey)

		switch status {
		case coretask.TaskStatusRetry.String():
			if err := w.bindingStore.MarkTaskStatus(ctx, taskID, domain.AsyncTaskBindingStatusRetry, errMsg); err != nil {
				gaia.ErrorF("workflow worker mark retry failed: %v", err)
			}
			return nil
		case coretask.TaskStatusSuccess.String():
			if err := w.bindingStore.MarkTaskStatus(ctx, taskID, domain.AsyncTaskBindingStatusSuccess, ""); err != nil {
				gaia.ErrorF("workflow worker mark success failed: %v", err)
			}
			variables := w.resultVars(taskID)
			if err := w.callback.Complete(ctx, binding, variables); err != nil {
				gaia.ErrorF("workflow worker complete callback failed: %v", err)
				_ = w.bindingStore.MarkCallback(ctx, taskID, domain.AsyncTaskCallbackStatusFailed, err.Error())
				return nil
			}
			_ = w.bindingStore.MarkCallback(ctx, taskID, domain.AsyncTaskCallbackStatusSuccess, "")
			return nil
		case coretask.TaskStatusFailed.String():
			if err := w.bindingStore.MarkTaskStatus(ctx, taskID, domain.AsyncTaskBindingStatusFailed, errMsg); err != nil {
				gaia.ErrorF("workflow worker mark failed failed: %v", err)
			}
			if err := w.callback.Fail(ctx, binding, errMsg); err != nil {
				gaia.ErrorF("workflow worker fail callback failed: %v", err)
				_ = w.bindingStore.MarkCallback(ctx, taskID, domain.AsyncTaskCallbackStatusFailed, err.Error())
				return nil
			}
			_ = w.bindingStore.MarkCallback(ctx, taskID, domain.AsyncTaskCallbackStatusSuccess, "")
			return nil
		default:
			return nil
		}
	}
}

// fetchResultVariables 从 asynctask 任务的 LastResult 解析回写变量（默认实现）。
func (w *Worker) fetchResultVariables(taskID int64) map[string]any {
	task, err := coretask.GetTask(taskID)
	if err != nil {
		return map[string]any{}
	}
	return parseResultVariables(task.LastResult)
}

// currentAsyncTaskID 从当前 goroutine 的 ContextTrace 读取正在执行的 asynctask 任务 ID。
func currentAsyncTaskID() (int64, bool) {
	value := gaia.NewContextTrace().GetKvField(coretask.ExecutorTaskIdCtxKey)
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

// currentString 从当前 goroutine 的 ContextTrace 读取字符串字段。
func currentString(key string) string {
	value := gaia.NewContextTrace().GetKvField(key)
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// contextFromBinding 由绑定构造供业务方法读取的 workflow 任务上下文。
func contextFromBinding(binding domain.AsyncTaskBinding) automation.WorkflowTaskContext {
	return automation.WorkflowTaskContext{
		WorkflowTaskID:    binding.WorkflowTaskID,
		ProcessInstanceID: binding.ProcessInstanceID,
		DefinitionID:      binding.DefinitionID,
		DefinitionKey:     binding.DefinitionKey,
		DefinitionName:    binding.DefinitionName,
		DefinitionVersion: binding.DefinitionVersion,
		InstanceName:      binding.InstanceName,
		NodeID:            binding.NodeID,
		NodeName:          binding.NodeName,
		AutomationTaskKey: binding.AutomationTaskKey,
		AsyncTaskID:       binding.AsyncTaskID,
	}
}

// parseResultVariables 把业务方法返回值（JSON）解析为回写 engine 的变量表。
func parseResultVariables(lastResult string) map[string]any {
	if lastResult == "" {
		return map[string]any{}
	}
	var variables map[string]any
	if err := json.Unmarshal([]byte(lastResult), &variables); err == nil && variables != nil {
		return variables
	}
	var raw any
	if err := json.Unmarshal([]byte(lastResult), &raw); err == nil && raw != nil {
		return map[string]any{"value": raw}
	}
	return map[string]any{}
}
