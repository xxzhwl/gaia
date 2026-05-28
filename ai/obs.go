// Package ai 可观测性
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"time"

	"github.com/xxzhwl/gaia"
)

// CallEvent 一次 LLM 调用产生的可观测事件
type CallEvent struct {
	// ClientName 客户端名（OpenAIClient.Name）
	ClientName string
	// Op 操作类型：chat / chat_stream / embed
	Op string
	// Model 实际使用的模型
	Model string
	// Latency 耗时
	Latency time.Duration
	// Usage Token 用量
	Usage Usage
	// CacheHit 是否命中缓存（仅 chat）
	CacheHit bool
	// Err 调用错误（成功为 nil）
	Err error
}

// Observer 客户端调用钩子；用户可自行实现以接入 Prometheus / OpenTelemetry / 业务日志
type Observer interface {
	OnCall(ev CallEvent)
}

// ObserverFunc 让普通函数实现 Observer
type ObserverFunc func(ev CallEvent)

// OnCall 实现 Observer
func (f ObserverFunc) OnCall(ev CallEvent) { f(ev) }

// emitEvent 向 Observer 投递一个事件；同时打 debug 日志便于排查。
// Observer 内部 panic 不会冒泡到调用方。
func (c *OpenAIClient) emitEvent(ev CallEvent) {
	defer func() {
		if r := recover(); r != nil {
			gaia.ErrorF("ai: observer panic: %v", r)
		}
	}()
	if ev.ClientName == "" {
		ev.ClientName = c.Name
	}

	if ev.Err != nil {
		gaia.ErrorF("ai: op=%s model=%s latency=%s err=%s",
			ev.Op, ev.Model, ev.Latency, ev.Err.Error())
	} else {
		gaia.InfoF("ai: op=%s model=%s latency=%s tokens(p/c/t)=%d/%d/%d cache=%v",
			ev.Op, ev.Model, ev.Latency,
			ev.Usage.PromptTokens, ev.Usage.CompletionTokens, ev.Usage.TotalTokens,
			ev.CacheHit)
	}

	if c.Observer != nil {
		c.Observer.OnCall(ev)
	}
}
