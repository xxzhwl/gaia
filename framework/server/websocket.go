package server

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/websocket"
	"github.com/xxzhwl/gaia"
)

type WSDataHandler func(msg []byte) (res []byte, err error)

type CommonWSHandler struct {
	Handler      WSDataHandler
	Title        string
	Wait         bool
	PushSecond   time.Duration // 最小 100ms，防止 CPU 空转
	PingInterval time.Duration // Ping 心跳间隔，默认 30s
	WriteTimeout time.Duration // 写超时，默认 10s
	ReadTimeout  time.Duration // 读超时，默认 60s
}

func MakeCommonWebSocketHandler(handler CommonWSHandler) app.HandlerFunc {
	var upgrader = websocket.HertzUpgrader{}

	// PushSecond 最小 100ms 防止 CPU 空转
	if !handler.Wait && handler.PushSecond < 100*time.Millisecond {
		handler.PushSecond = 100 * time.Millisecond
	}

	// 设置默认心跳间隔
	if handler.PingInterval <= 0 {
		handler.PingInterval = 30 * time.Second
	}
	// 设置默认写超时
	if handler.WriteTimeout <= 0 {
		handler.WriteTimeout = 10 * time.Second
	}
	// 设置默认读超时
	if handler.ReadTimeout <= 0 {
		handler.ReadTimeout = 60 * time.Second
	}

	return func(c context.Context, ctx *app.RequestContext) {
		err := upgrader.Upgrade(ctx, func(conn *websocket.Conn) {
			defer conn.Close() // 确保连接关闭

			// 设置读超时
			conn.SetReadDeadline(time.Now().Add(handler.ReadTimeout))
			conn.SetPongHandler(func(string) error {
				conn.SetReadDeadline(time.Now().Add(handler.ReadTimeout))
				return nil
			})

			// Ping 心跳协程
			go func() {
				ticker := time.NewTicker(handler.PingInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
							return
						}
					case <-c.Done():
						return
					}
				}
			}()

			for {
				var message []byte
				var err error

				if handler.Wait {
					_, message, err = conn.ReadMessage()
					if err != nil {
						gaia.ErrorF("websocket read error: %v", err)
						break
					}
				}

				if handler.Handler != nil {
					bytes, err := handler.Handler(message)
					if err != nil {
						gaia.ErrorF("websocket message handler error: %v", err)
						continue
					}

					// 设置写超时
					if err := conn.SetWriteDeadline(time.Now().Add(handler.WriteTimeout)); err != nil {
						break
					}
					if err := conn.WriteMessage(websocket.TextMessage, bytes); err != nil {
						gaia.ErrorF("websocket write error: %v", err)
						break
					}
				}

				if !handler.Wait {
					time.Sleep(handler.PushSecond)
				}
			}
		})
		if err != nil {
			gaia.ErrorF("websocket upgrade error: %v", err)
			gaia.SendSystemAlarm("WebSocketUpgradeErr", handler.Title)
			return
		}
	}
}