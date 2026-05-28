// Package asynctask 注释
// @author wanlizhan=ad
// @created 2026/05/27
package asynctask

import (
	"context"
	"sync"
	"time"
)

// TaskEvent 描述一次任务执行的关键时刻信息，传递给 Hook 用于自定义观测。
type TaskEvent struct {
	Theme       string
	TaskId      int64
	TaskName    string
	ServiceName string
	MethodName  string
	Status      string // 事件结束时的状态：Success/Failed/Retry/Panic
	StartTime   time.Time
	EndTime     time.Time
	WaitMillis  int64  // 从 create_time 到首次执行的等待毫秒数
	Duration    int64  // 执行耗时毫秒
	RetryTime   int    // 当前重试次数
	ErrMsg      string // 错误或 panic 信息
	IsPanic     bool
}

// TaskHook 是 asynctask 模块对外暴露的观测 Hook 接口。
// 用户可注册自定义实现，将事件转发到 Datadog / 自家监控平台 / 业务审计等。
// 所有方法都允许为 nil，且执行时会被框架内部 recover 保护，不影响任务主流程。
type TaskHook interface {
	OnTaskStart(ctx context.Context, ev TaskEvent)
	OnTaskFinish(ctx context.Context, ev TaskEvent)
	OnTaskPanic(ctx context.Context, ev TaskEvent)
}

// hookRegistry 内部注册中心，支持挂多个 Hook。
type hookRegistry struct {
	mu    sync.RWMutex
	hooks []TaskHook
}

var globalHookRegistry = &hookRegistry{}

// RegisterTaskHook 全局注册一个任务 Hook，对所有 Scheduler 生效。
// 多次注册按注册顺序触发；通过返回的 unregister 回调可以注销。
func RegisterTaskHook(h TaskHook) (unregister func()) {
	if h == nil {
		return func() {}
	}
	globalHookRegistry.mu.Lock()
	defer globalHookRegistry.mu.Unlock()
	globalHookRegistry.hooks = append(globalHookRegistry.hooks, h)
	idx := len(globalHookRegistry.hooks) - 1
	return func() {
		globalHookRegistry.mu.Lock()
		defer globalHookRegistry.mu.Unlock()
		if idx >= 0 && idx < len(globalHookRegistry.hooks) {
			globalHookRegistry.hooks[idx] = noopHook{}
		}
	}
}

// fireHook 安全调用所有注册 Hook。任意 Hook panic 不会影响主流程。
func fireHook(ctx context.Context, kind string, ev TaskEvent, schedulerHook TaskHook) {
	defer func() {
		recover()
	}()

	all := []TaskHook{}
	globalHookRegistry.mu.RLock()
	all = append(all, globalHookRegistry.hooks...)
	globalHookRegistry.mu.RUnlock()
	if schedulerHook != nil {
		all = append(all, schedulerHook)
	}

	for _, h := range all {
		if h == nil {
			continue
		}
		callHook(ctx, h, kind, ev)
	}
}

func callHook(ctx context.Context, h TaskHook, kind string, ev TaskEvent) {
	defer func() {
		recover()
	}()
	switch kind {
	case "start":
		h.OnTaskStart(ctx, ev)
	case "finish":
		h.OnTaskFinish(ctx, ev)
	case "panic":
		h.OnTaskPanic(ctx, ev)
	}
}

// noopHook 占位实现。
type noopHook struct{}

func (noopHook) OnTaskStart(context.Context, TaskEvent)  {}
func (noopHook) OnTaskFinish(context.Context, TaskEvent) {}
func (noopHook) OnTaskPanic(context.Context, TaskEvent)  {}
