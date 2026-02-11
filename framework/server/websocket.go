package server

import (
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/websocket"
	"github.com/xxzhwl/gaia"
	"time"
)

type WSDataHandler func(msg []byte) (res []byte, err error)

type CommonWSHandler struct {
	Handler    WSDataHandler
	Title      string
	Wait       bool
	PushSecond time.Duration
}

func MakeCommonWebSocketHandler(handler CommonWSHandler) app.HandlerFunc {
	var upgrader = websocket.HertzUpgrader{} // use default options

	return func(c context.Context, ctx *app.RequestContext) {
		err := upgrader.Upgrade(ctx, func(conn *websocket.Conn) {
			for {
				if handler.Handler == nil {
					gaia.Error("websocket handler is nil")
					gaia.SendSystemAlarm("WebSocketHandlerIsNil", handler.Title)
					break
				}

				var message []byte
				var err error
				if handler.Wait {
					_, message, err = conn.ReadMessage()
					if err != nil {
						gaia.ErrorF("websocket read error: %v", err)
						gaia.SendSystemAlarm("WebSocketHandlerReadErr", handler.Title)
						break
					}
				}

				bytes, err := handler.Handler(message)
				if err != nil {
					gaia.ErrorF("websocket message handler error: %v", err)
					gaia.SendSystemAlarm("WebSocketHandler", handler.Title)
					continue
				}

				err = conn.WriteMessage(websocket.TextMessage, bytes)
				if err != nil {
					gaia.ErrorF("websocket write error: %v", err)
					gaia.SendSystemAlarm("WebSocketHandlerWriteErr", handler.Title)
					break
				}
				time.Sleep(handler.PushSecond)
			}
		})
		if err != nil {
			gaia.ErrorF("websocket upgrade error: %v", err)
			gaia.SendSystemAlarm("WebSocketUpgradeErr", handler.Title)
			return
		}
	}
}
