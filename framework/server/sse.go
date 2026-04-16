package server

import (
	"context"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/sse"
)

type SSEHandler func() []byte

// SSEHandlerV2 支持 context 和 RequestContext 的 SSE handler
type SSEHandlerV2 func(ctx context.Context, c *app.RequestContext) ([]byte, error)

// SSEConfig SSE 配置
type SSEConfig struct {
	Interval  time.Duration // 推送间隔，默认 1 秒
	EventName string        // 事件名，默认 "message"
}

// MakeSSEHandler 旧版 SSE Handler，保持向后兼容
func MakeSSEHandler(handler SSEHandler) app.HandlerFunc {
	return MakeSSEHandlerV2WithConfig(func(ctx context.Context, c *app.RequestContext) ([]byte, error) {
		return handler(), nil
	}, SSEConfig{Interval: 1 * time.Second, EventName: "message"})
}

// MakeSSEHandlerV2WithConfig 新版 SSE Handler，支持 context 感知客户端断连
func MakeSSEHandlerV2WithConfig(handler SSEHandlerV2, config SSEConfig) app.HandlerFunc {
	// 设置默认值
	if config.Interval <= 0 {
		config.Interval = 1 * time.Second
	}
	if config.EventName == "" {
		config.EventName = "message"
	}

	return func(ctx context.Context, c *app.RequestContext) {
		c.SetStatusCode(http.StatusOK)
		c.Response.Header.Set("Content-Type", "text/event-stream")
		c.Response.Header.Set("Cache-Control", "no-cache")
		c.Response.Header.Set("Connection", "keep-alive")

		s := sse.NewStream(c)
		ticker := time.NewTicker(config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done(): // 感知客户端断开
				return
			case <-ticker.C:
				data, err := handler(ctx, c)
				if err != nil {
					return
				}
				event := &sse.Event{
					Event: config.EventName,
					Data:  data,
				}
				if err := s.Publish(event); err != nil {
					return
				}
			}
		}
	}
}