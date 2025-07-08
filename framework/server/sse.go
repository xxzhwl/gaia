package server

import (
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/sse"
	"net/http"
	"time"
)

type SSEHandler func() []byte

func MakeSSEHandler(handler SSEHandler) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// 在第一次渲染调用之前必须先行设置状态代码和响应头文件
		c.SetStatusCode(http.StatusOK)
		s := sse.NewStream(c)
		for range time.NewTicker(1 * time.Second).C {
			event := &sse.Event{
				Event: "timestamp",
				Data:  handler(),
			}
			err := s.Publish(event)
			if err != nil {
				return
			}
		}
	}
}
