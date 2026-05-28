// Package jobs 任务事件 Hook。
// @author wanlizhan
// @created 2026/05/27
package jobs

import (
	"context"
	"sync"
	"time"
)

// JobEvent 描述一次 job 执行的关键时刻信息。
type JobEvent struct {
	JobId         int64
	JobName       string
	JobType       string // cron_job / cron_hook
	ServiceName   string
	ServiceMethod string
	HookUrl       string
	CronExpr      string
	Status        string // success / failed / panic / timeout / skip
	StartTime     time.Time
	EndTime       time.Time
	Duration      int64 // ms
	ErrMsg        string
	IsPanic       bool
	IsTimeout     bool
	InstanceId    string // 本实例 ID（多副本部署下便于定位）
}

// JobHook 是 jobs 模块对外暴露的观测 Hook 接口。
// 用户可注册自定义实现，将事件转发到 Datadog / 自家监控平台。
// 所有方法都允许为 nil，且执行时会被框架内部 recover 保护。
type JobHook interface {
	OnJobStart(ctx context.Context, ev JobEvent)
	OnJobFinish(ctx context.Context, ev JobEvent)
	OnJobPanic(ctx context.Context, ev JobEvent)
	OnJobTimeout(ctx context.Context, ev JobEvent)
}

type jobHookRegistry struct {
	mu    sync.RWMutex
	hooks []JobHook
}

var globalJobHookRegistry = &jobHookRegistry{}

// RegisterJobHook 全局注册一个 job Hook，对所有 RunJob 实例生效。
func RegisterJobHook(h JobHook) (unregister func()) {
	if h == nil {
		return func() {}
	}
	globalJobHookRegistry.mu.Lock()
	defer globalJobHookRegistry.mu.Unlock()
	globalJobHookRegistry.hooks = append(globalJobHookRegistry.hooks, h)
	idx := len(globalJobHookRegistry.hooks) - 1
	return func() {
		globalJobHookRegistry.mu.Lock()
		defer globalJobHookRegistry.mu.Unlock()
		if idx >= 0 && idx < len(globalJobHookRegistry.hooks) {
			globalJobHookRegistry.hooks[idx] = noopJobHook{}
		}
	}
}

// fireJobHook 安全调用所有注册 Hook。任意 Hook panic 不影响主流程。
func fireJobHook(ctx context.Context, kind string, ev JobEvent, instanceHook JobHook) {
	defer func() {
		recover()
	}()

	all := []JobHook{}
	globalJobHookRegistry.mu.RLock()
	all = append(all, globalJobHookRegistry.hooks...)
	globalJobHookRegistry.mu.RUnlock()
	if instanceHook != nil {
		all = append(all, instanceHook)
	}

	for _, h := range all {
		if h == nil {
			continue
		}
		callJobHook(ctx, h, kind, ev)
	}
}

func callJobHook(ctx context.Context, h JobHook, kind string, ev JobEvent) {
	defer func() {
		recover()
	}()
	switch kind {
	case "start":
		h.OnJobStart(ctx, ev)
	case "finish":
		h.OnJobFinish(ctx, ev)
	case "panic":
		h.OnJobPanic(ctx, ev)
	case "timeout":
		h.OnJobTimeout(ctx, ev)
	}
}

// noopJobHook 占位实现。
type noopJobHook struct{}

func (noopJobHook) OnJobStart(context.Context, JobEvent)   {}
func (noopJobHook) OnJobFinish(context.Context, JobEvent)  {}
func (noopJobHook) OnJobPanic(context.Context, JobEvent)   {}
func (noopJobHook) OnJobTimeout(context.Context, JobEvent) {}
