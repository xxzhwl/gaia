package workflow

import (
	"context"
	"testing"

	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/notification"
)

// 编译期契约断言：运行时实现必须满足调度器/通知端口。
//
// 一旦 dispatcher / notification 端口与 engine 运行时之间的请求类型发生分叉
// （例如各自重复定义结构相同但具名不同的 MarkOutboxFailedRequest），
// Engine.ConfigureDispatcher / ConfigureNotificationDispatcher 中的类型断言
// 会在运行时静默失败，导致外部任务与通知投递整体不可用且无任何报错。
// 把契约提升到编译期，可在该类回归发生时立即让构建失败。
var (
	_ DispatcherRuntime   = (*memoryRuntimeAdapter)(nil)
	_ NotificationRuntime = (*memoryRuntimeAdapter)(nil)
	_ DispatcherRuntime   = (*workflowengine.PersistentRuntime)(nil)
	_ NotificationRuntime = (*workflowengine.PersistentRuntime)(nil)
)

func TestConfigureDispatcherWiresMemoryRuntime(t *testing.T) {
	e := NewMemoryEngine()
	e.ConfigureDispatcher(DispatcherConfig{Enabled: true, CallbackBaseURL: "http://callback"})
	if e.dispatcher == nil {
		t.Fatal("expected dispatcher to be wired for memory runtime, got nil (runtime no longer satisfies DispatcherRuntime)")
	}
}

func TestConfigureDispatcherDisabledStaysNil(t *testing.T) {
	e := NewMemoryEngine()
	e.ConfigureDispatcher(DispatcherConfig{Enabled: false})
	if e.dispatcher != nil {
		t.Fatal("expected dispatcher to stay nil when disabled")
	}
}

func TestConfigureNotificationDispatcherWiresMemoryRuntime(t *testing.T) {
	e := NewMemoryEngine()
	sender := notification.SenderFunc(func(_ context.Context, _ notification.Message) error { return nil })
	e.ConfigureNotificationDispatcher(sender, NotificationConfig{})
	if e.notificationDispatcher == nil {
		t.Fatal("expected notification dispatcher to be wired for memory runtime, got nil (runtime no longer satisfies NotificationRuntime)")
	}
}

func TestConfigureNotificationDispatcherNilSenderStaysNil(t *testing.T) {
	e := NewMemoryEngine()
	e.ConfigureNotificationDispatcher(nil, NotificationConfig{})
	if e.notificationDispatcher != nil {
		t.Fatal("expected notification dispatcher to stay nil when sender is nil")
	}
}
