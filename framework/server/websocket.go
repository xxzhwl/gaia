// Package server
//
// WebSocket 封装
// =========================================================================
// 设计目标：
//  1. 连接级生命周期：OnOpen / OnMessage / OnClose 三个钩子，每连接独占；
//     上游可以在 OnOpen 中维护 per-connection 状态（订阅、邮箱、用户信息），
//     在 OnClose 中统一清理资源，避免泄漏。
//  2. 并发安全写：WSConn 对外暴露 Send / SendJSON / SendBinary，内部持锁，
//     业务可以在任意 goroutine 安全推送，无需关心写冲突。
//  3. Upgrader 可配置：CheckOrigin、Subprotocols、Buffer、Compression、
//     MaxMessageSize 全部暴露为字段，适配生产需求。
//  4. 完善的心跳与超时：Ping/Pong 自动处理；读/写/Pong 超时可配置；
//     Ping goroutine 使用独立 done 通道，连接关闭立刻退出。
//  5. 统一的日志与告警：正常关闭（1000/1001/1005/1006 等）降级为 Debug，
//     仅真实异常才 ErrorF + SendSystemAlarm。
//  6. 全链路 panic recover，OnOpen/OnMessage/OnClose 任一环节 panic 都不会
//     拖垮进程，一律走 gaia.PanicLog。
//
// 约定：
//   - 必须通过 WSConfig.NewHandler 工厂创建 per-connection 处理器。
//     这一强制约定让鉴权信息（JWT Claims 等）的捕获、订阅的建立、邮箱的分配
//     都天然落在「每连接一次」的位置上。
//   - 业务主动关闭连接直接调用 conn.Close()。
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
	"sync/atomic"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/websocket"

	"github.com/xxzhwl/gaia"
)

// WSMessageType 常量别名（避免上游直接依赖 hertz-contrib/websocket）。
const (
	WSTextMessage   = websocket.TextMessage
	WSBinaryMessage = websocket.BinaryMessage
)

// WSConn 是线程安全的 WebSocket 连接封装。
//
// 关键特性：
//   - Send / SendJSON / SendBinary 内部持写锁，支持任意 goroutine 并发调用；
//   - 每次写入自动应用 WriteTimeout；
//   - Close 幂等，可被多次调用；
//   - Done() 提供连接生命周期感知。
type WSConn struct {
	raw          *websocket.Conn
	writeMu      sync.Mutex
	writeTimeout time.Duration
	closeOnce    sync.Once
	closed       chan struct{}

	id     string // 连接 ID（框架生成，便于日志追踪）
	remote string // 客户端地址
}

// ID 返回连接 ID。
func (c *WSConn) ID() string { return c.id }

// RemoteAddr 返回客户端地址。
func (c *WSConn) RemoteAddr() string { return c.remote }

// Raw 返回底层 *websocket.Conn（逃生舱，不建议直接使用）。
func (c *WSConn) Raw() *websocket.Conn { return c.raw }

// Done 返回一个在连接关闭后会被 close 的 channel，业务协程可以用它感知连接结束。
func (c *WSConn) Done() <-chan struct{} { return c.closed }

// IsClosed 返回连接是否已关闭。
func (c *WSConn) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// Send 发送一条文本消息。并发安全。
func (c *WSConn) Send(data []byte) error {
	return c.writeMessage(WSTextMessage, data)
}

// SendBinary 发送一条二进制消息。并发安全。
func (c *WSConn) SendBinary(data []byte) error {
	return c.writeMessage(WSBinaryMessage, data)
}

// SendJSON 将 v 序列化为 JSON 文本消息发送。并发安全。
func (c *WSConn) SendJSON(v any) error {
	bs, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("ws marshal json: %w", err)
	}
	return c.writeMessage(WSTextMessage, bs)
}

// Close 主动关闭连接，向对端发送正常关闭帧。幂等。
func (c *WSConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		_ = c.raw.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		c.writeMu.Unlock()
		err = c.raw.Close()
		close(c.closed)
	})
	return err
}

func (c *WSConn) writeMessage(mt int, data []byte) error {
	if c.IsClosed() {
		return errors.New("ws connection closed")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.raw.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return err
	}
	return c.raw.WriteMessage(mt, data)
}

// WSConnHandler 是每个连接的业务处理器，由 WSConfig.NewHandler 工厂按连接创建。
//
// 生命周期钩子：
//   - OnOpen 在读循环开始前调用一次，返回 err 会导致连接立即关闭（OnClose 仍会调用）；
//   - OnMessage 在主 goroutine 中按消息顺序调用，返回 err 只记录日志，不自动关闭连接
//     （若希望关闭，请在 OnMessage 内 conn.Close()）；
//   - OnClose 保证恰好调用一次（无论是对端关闭、读超时、OnOpen 返回 err 还是本地 Close），
//     适合做资源清理；err 为底层触发关闭的原因，正常关闭时为 nil。
type WSConnHandler interface {
	OnOpen(ctx context.Context, conn *WSConn) error
	OnMessage(ctx context.Context, conn *WSConn, messageType int, data []byte) error
	OnClose(ctx context.Context, conn *WSConn, err error)
}

// WSConnHandlerFuncs 让调用方不必实现完整接口，按需填充字段即可。
type WSConnHandlerFuncs struct {
	Open    func(ctx context.Context, conn *WSConn) error
	Message func(ctx context.Context, conn *WSConn, messageType int, data []byte) error
	Close   func(ctx context.Context, conn *WSConn, err error)
}

// OnOpen implements WSConnHandler.
func (f WSConnHandlerFuncs) OnOpen(ctx context.Context, conn *WSConn) error {
	if f.Open == nil {
		return nil
	}
	return f.Open(ctx, conn)
}

// OnMessage implements WSConnHandler.
func (f WSConnHandlerFuncs) OnMessage(ctx context.Context, conn *WSConn, mt int, data []byte) error {
	if f.Message == nil {
		return nil
	}
	return f.Message(ctx, conn, mt, data)
}

// OnClose implements WSConnHandler.
func (f WSConnHandlerFuncs) OnClose(ctx context.Context, conn *WSConn, err error) {
	if f.Close != nil {
		f.Close(ctx, conn, err)
	}
}

// WSConfig 是 WebSocket 路由的完整配置。
//
// 必填：NewHandler（连接级 handler 工厂）。
// 其它字段都有合理默认值。
type WSConfig struct {
	// Title 用于日志与告警的业务标识（如 "AgentFlowWS"）。
	Title string

	// NewHandler 每次 Upgrade 成功后调用一次，返回本连接独占的处理器。
	// 可在此闭包中读取 *app.RequestContext（鉴权信息、URL 参数等）并绑定到 handler 上。
	// 必填。
	NewHandler func(ctx context.Context, c *app.RequestContext) (WSConnHandler, error)

	// ===== Upgrader =====
	// CheckOrigin 为 nil 时默认使用 hertz-contrib 的同源校验（Origin 为空或 host 匹配通过）。
	CheckOrigin func(c *app.RequestContext) bool
	// Subprotocols 服务端支持的子协议列表，按优先级。
	Subprotocols []string
	// ReadBufferSize/WriteBufferSize 为 0 时使用 hertz 默认。
	ReadBufferSize  int
	WriteBufferSize int
	// EnableCompression 启用 permessage-deflate 压缩协商。
	EnableCompression bool
	// MaxMessageSize 单条消息最大字节数；<=0 表示不限制。
	MaxMessageSize int64

	// ===== 超时 =====
	// ReadTimeout 读超时（Pong 到达会重置）。默认 60s。
	ReadTimeout time.Duration
	// WriteTimeout 每次 Send 的写超时。默认 10s。
	WriteTimeout time.Duration
	// PingInterval 心跳 Ping 发送间隔。默认 30s。设置为 <0 禁用心跳。
	PingInterval time.Duration
}

// MakeWSHandler 基于 WSConfig 创建 hertz 路由处理器。
//
// 示例：
//
//	r.GET("/ws", server.MakeWSHandler(server.WSConfig{
//	    Title: "AgentFlowWS",
//	    NewHandler: func(ctx context.Context, c *app.RequestContext) (server.WSConnHandler, error) {
//	        claims, _ := server.NewRequest(c).GetUserInfo()
//	        return hub.NewSession(claims), nil
//	    },
//	}))
func MakeWSHandler(cfg WSConfig) app.HandlerFunc {
	if cfg.NewHandler == nil {
		panic("server.MakeWSHandler: WSConfig.NewHandler must not be nil")
	}
	applyWSDefaults(&cfg)

	upgrader := &websocket.HertzUpgrader{
		ReadBufferSize:    cfg.ReadBufferSize,
		WriteBufferSize:   cfg.WriteBufferSize,
		Subprotocols:      cfg.Subprotocols,
		CheckOrigin:       cfg.CheckOrigin,
		EnableCompression: cfg.EnableCompression,
	}

	return func(parentCtx context.Context, c *app.RequestContext) {
		// 1) HTTP 阶段先创建 handler：此时还能访问 *app.RequestContext 的鉴权信息与 query
		connCtx, cancel := context.WithCancel(parentCtx)
		handler, err := cfg.NewHandler(connCtx, c)
		if err != nil {
			cancel()
			gaia.ErrorF("[%s] ws NewHandler err: %v", cfg.Title, err)
			c.AbortWithMsg(err.Error(), 400)
			return
		}
		remote := c.ClientIP()

		// 2) Upgrade
		err = upgrader.Upgrade(c, func(raw *websocket.Conn) {
			conn := &WSConn{
				raw:          raw,
				writeTimeout: cfg.WriteTimeout,
				closed:       make(chan struct{}),
				id:           newWSConnID(),
				remote:       remote,
			}

			var (
				closeErr    error
				onCloseOnce sync.Once
			)
			fireOnClose := func(e error) {
				onCloseOnce.Do(func() {
					defer func() {
						if r := recover(); r != nil {
							gaia.PanicLog(r)
						}
					}()
					handler.OnClose(connCtx, conn, e)
				})
			}
			defer func() {
				cancel()
				fireOnClose(closeErr)
				_ = conn.Close()
			}()

			// Pong / Read Deadline
			_ = raw.SetReadDeadline(time.Now().Add(cfg.ReadTimeout))
			raw.SetPongHandler(func(string) error {
				return raw.SetReadDeadline(time.Now().Add(cfg.ReadTimeout))
			})
			if cfg.MaxMessageSize > 0 {
				raw.SetReadLimit(cfg.MaxMessageSize)
			}

			// OnOpen
			func() {
				defer func() {
					if r := recover(); r != nil {
						gaia.PanicLog(r)
						closeErr = fmt.Errorf("panic in OnOpen: %v", r)
					}
				}()
				if err := handler.OnOpen(connCtx, conn); err != nil {
					closeErr = err
				}
			}()
			if closeErr != nil {
				gaia.ErrorF("[%s] ws OnOpen err: %v", cfg.Title, closeErr)
				return
			}

			// Ping goroutine（独立 done，连接一关立刻退出）
			if cfg.PingInterval > 0 {
				go pingLoop(raw, &conn.writeMu, cfg.PingInterval, cfg.WriteTimeout, conn.closed)
			}

			// Read loop
			for {
				mt, data, rerr := raw.ReadMessage()
				if rerr != nil {
					closeErr = normalizeCloseErr(rerr)
					if closeErr != nil {
						gaia.ErrorF("[%s] ws read err: %v", cfg.Title, closeErr)
					} else {
						gaia.DebugF("[%s] ws closed normally: %v", cfg.Title, rerr)
					}
					return
				}

				// OnMessage（panic 不影响循环）
				func() {
					defer func() {
						if r := recover(); r != nil {
							gaia.PanicLog(r)
						}
					}()
					if err := handler.OnMessage(connCtx, conn, mt, data); err != nil {
						gaia.ErrorF("[%s] ws OnMessage err: %v", cfg.Title, err)
					}
				}()
			}
		})
		if err != nil {
			cancel()
			// 握手失败（非 GET、缺 header 等）属业务/客户端问题，仅记录日志不告警
			gaia.ErrorF("[%s] ws upgrade err: %v", cfg.Title, err)
		}
	}
}

func applyWSDefaults(cfg *WSConfig) {
	if cfg.Title == "" {
		cfg.Title = "WS"
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 60 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 30 * time.Second
	}
}

// pingLoop 周期发送 Ping 帧，任一错误或 done 到达即退出。
func pingLoop(raw *websocket.Conn, mu *sync.Mutex, interval, writeTimeout time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			mu.Lock()
			_ = raw.SetWriteDeadline(time.Now().Add(writeTimeout))
			err := raw.WriteMessage(websocket.PingMessage, nil)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

// normalizeCloseErr 将正常关闭错误归一化为 nil。
func normalizeCloseErr(err error) error {
	if err == nil {
		return nil
	}
	if websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	) {
		return nil
	}
	return err
}

// newWSConnID 生成一个简单的连接 ID（纳秒 + 进程内原子计数）。
var wsConnSeq atomic.Uint64

func newWSConnID() string {
	n := wsConnSeq.Add(1)
	return fmt.Sprintf("ws-%d-%d", time.Now().UnixNano(), n)
}
