// Package server
//
// SSE 封装
// =========================================================================
// 设计目标：
//   1. 事件驱动：handler 签名为 func(ctx, c, w SSEWriter) error，
//      由 handler 决定何时推送，而不是框架按固定周期轮询。
//      天然支持 LLM 流式输出、Agent 进度事件、消息订阅等真实场景。
//   2. 完整事件控制：SSEWriter.Send 可以指定 Event/ID/Retry，支持
//      Last-Event-ID 断点续传。
//   3. 生产要点：
//      - 自动下发 Content-Type / Cache-Control / Connection
//      - 自动设置 X-Accel-Buffering: no（防 Nginx 默认 1s 缓冲）
//      - 内置心跳事件防 L7 空闲断连
//      - 客户端断连感知（ctx.Done()）
//      - panic recover 统一走 gaia.PanicLog
//
// @author wanlizhan
// @created 2026-04-28
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/sse"
	"github.com/xxzhwl/gaia"
)

// SSEEvent 表示一条 SSE 事件，完全对齐 hertz-contrib/sse.Event。
type SSEEvent struct {
	Event string // 事件名，默认 "message"
	ID    string // 事件 ID，客户端可通过 Last-Event-ID 断点续传
	Retry uint64 // 建议客户端重连间隔（毫秒）；0 表示不设置
	Data  []byte // 事件数据；若希望发 JSON，使用 SSEWriter.SendJSON
}

// SSEWriter 是 SSE handler 在运行期拿到的写入器。并发安全。
type SSEWriter interface {
	// Send 发送一条完整事件。
	Send(event SSEEvent) error
	// SendData 发送仅带 data 的默认事件（event 名为 "message"）。
	SendData(data []byte) error
	// SendJSON 以指定事件名 + 自动 JSON 序列化发送；name 为空时用 "message"。
	SendJSON(name string, v any) error
	// Ping 发送一条心跳事件保活，不触发浏览器 EventSource 的 message 监听。
	Ping() error
	// Context 返回随 handler 一起传入的 ctx；客户端断开后会被 cancel。
	Context() context.Context
	// LastEventID 读取 Last-Event-ID 请求头，用于断点续传。
	LastEventID() string
}

// SSEHandler 是事件驱动的 SSE handler。
//
// handler 返回即意味着流结束。返回非 nil error 时框架记录错误日志并触发 OnError 回调，
// 但不会再尝试向客户端写入（此时连接通常已不可用）。
type SSEHandler func(ctx context.Context, c *app.RequestContext, w SSEWriter) error

// SSEOptions 控制 SSE 流的框架级行为。
type SSEOptions struct {
	// Title 用于日志标识。
	Title string
	// HeartbeatInterval 心跳间隔，默认 15s；<0 禁用。
	HeartbeatInterval time.Duration
	// DisableNginxBuffering 为 true 时不下发 X-Accel-Buffering: no。
	// 默认 false，即默认下发该响应头，适配 Nginx/OpenResty 等反向代理。
	DisableNginxBuffering bool
	// OnError handler 返回 err 时的回调（除了默认日志）。
	OnError func(ctx context.Context, c *app.RequestContext, err error)
}

// MakeSSEHandler 创建事件驱动的 SSE 路由。
//
// 示例：
//
//	r.GET("/stream", server.MakeSSEHandler(
//	    func(ctx context.Context, c *app.RequestContext, w server.SSEWriter) error {
//	        sub := taskBus.Subscribe(ctx)
//	        defer sub.Close()
//	        for event := range sub.Events() {
//	            if err := w.SendJSON(event.Name, event.Payload); err != nil {
//	                return err
//	            }
//	        }
//	        return nil
//	    },
//	    server.SSEOptions{Title: "AgentFlowSSE"},
//	))
func MakeSSEHandler(handler SSEHandler, opts ...SSEOptions) app.HandlerFunc {
	if handler == nil {
		panic("server.MakeSSEHandler: handler must not be nil")
	}
	var opt SSEOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.Title == "" {
		opt.Title = "SSE"
	}
	if opt.HeartbeatInterval == 0 {
		opt.HeartbeatInterval = 15 * time.Second
	}
	autoNginxHeader := !opt.DisableNginxBuffering

	return func(parentCtx context.Context, c *app.RequestContext) {
		// 响应头
		c.SetStatusCode(200)
		c.Response.Header.Set("Content-Type", "text/event-stream")
		c.Response.Header.Set("Cache-Control", "no-cache")
		c.Response.Header.Set("Connection", "keep-alive")
		if autoNginxHeader {
			c.Response.Header.Set("X-Accel-Buffering", "no")
		}

		ctx, cancel := context.WithCancel(parentCtx)
		defer cancel()

		stream := sse.NewStream(c)
		w := &sseWriter{
			stream:      stream,
			ctx:         ctx,
			lastEventID: sse.GetLastEventID(c),
		}

		// 心跳 goroutine
		if opt.HeartbeatInterval > 0 {
			go func() {
				ticker := time.NewTicker(opt.HeartbeatInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := w.Ping(); err != nil {
							// 写失败说明客户端断开，取消主 handler
							cancel()
							return
						}
					}
				}
			}()
		}

		// 运行 handler
		defer func() {
			if r := recover(); r != nil {
				gaia.PanicLog(r)
				gaia.ErrorF("[%s] sse handler panic: %v", opt.Title, r)
			}
		}()

		if err := handler(ctx, c, w); err != nil && !errors.Is(err, context.Canceled) {
			gaia.ErrorF("[%s] sse handler err: %v", opt.Title, err)
			if opt.OnError != nil {
				opt.OnError(ctx, c, err)
			}
		}
	}
}

// sseWriter 是 SSEWriter 的默认实现（并发安全）。
type sseWriter struct {
	stream      *sse.Stream
	ctx         context.Context
	mu          sync.Mutex
	lastEventID string
	closed      bool
}

func (w *sseWriter) Context() context.Context { return w.ctx }
func (w *sseWriter) LastEventID() string      { return w.lastEventID }

func (w *sseWriter) Send(event SSEEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("sse stream closed")
	}
	ev := &sse.Event{
		Event: event.Event,
		ID:    event.ID,
		Retry: event.Retry,
		Data:  event.Data,
	}
	if ev.Event == "" {
		ev.Event = "message"
	}
	if err := w.stream.Publish(ev); err != nil {
		w.closed = true
		return err
	}
	return nil
}

func (w *sseWriter) SendData(data []byte) error {
	return w.Send(SSEEvent{Data: data})
}

func (w *sseWriter) SendJSON(name string, v any) error {
	bs, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sse marshal json: %w", err)
	}
	return w.Send(SSEEvent{Event: name, Data: bs})
}

// Ping 发送一条 event="ping" 的空事件帧，用于保活。
// 浏览器 EventSource 默认只派发 "message" 事件，"ping" 事件不会触发监听器，
// 但能重置 L7（Nginx/CDN/ELB）的 idle 计时器，达到心跳目的。
// hertz-contrib/sse 未暴露注释帧 API，这是最兼容的实现方式。
func (w *sseWriter) Ping() error {
	return w.Send(SSEEvent{Event: "ping", Data: []byte("{}")})
}
