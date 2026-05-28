// Package ai 多轮会话封装
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"sync"
)

// Conversation 维护一段多轮对话上下文。
//
// 典型用法：
//
//	conv := client.NewConversation("你是一个简洁的助手").
//	    WithModel("gpt-4o-mini").
//	    WithMaxHistory(20)
//	reply, _ := conv.Ask(ctx, "今天天气如何？")
//	reply2, _ := conv.Ask(ctx, "那适合穿什么？")  // 自动带上 history
//
// 该结构是并发安全的（同一个 Conversation 同时只能有一次 Ask 在飞）。
type Conversation struct {
	client       *OpenAIClient
	systemPrompt string
	model        string
	temperature  *float64
	maxHistory   int

	mu       sync.Mutex
	messages []Message
}

// NewConversation 在已有客户端上开启一段会话。systemPrompt 可为空。
func (c *OpenAIClient) NewConversation(systemPrompt string) *Conversation {
	conv := &Conversation{
		client:       c,
		systemPrompt: systemPrompt,
		maxHistory:   40, // 默认保留最近 40 条用户/助手消息
	}
	return conv
}

// WithModel 设置该会话使用的模型
func (cv *Conversation) WithModel(model string) *Conversation {
	cv.model = model
	return cv
}

// WithTemperature 设置该会话采样温度
func (cv *Conversation) WithTemperature(t float64) *Conversation {
	cv.temperature = &t
	return cv
}

// WithMaxHistory 设置最大保留的历史消息数（不含 system prompt）。
// 超过部分会按时间顺序丢弃最早的（保留最新 N 条）。<=0 表示不限制。
func (cv *Conversation) WithMaxHistory(n int) *Conversation {
	cv.maxHistory = n
	return cv
}

// History 返回当前历史消息的拷贝（含 system prompt 作为首条）
func (cv *Conversation) History() []Message {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	return cv.snapshotLocked()
}

// Reset 清空对话历史（保留 system prompt）
func (cv *Conversation) Reset() {
	cv.mu.Lock()
	defer cv.mu.Unlock()
	cv.messages = nil
}

// Ask 发起一轮提问，自动把 user/assistant 消息追加到历史，返回助手回复文本。
func (cv *Conversation) Ask(ctx context.Context, userMsg string) (string, error) {
	cv.mu.Lock()
	cv.messages = append(cv.messages, Message{Role: RoleUser, Content: userMsg})
	cv.trimLocked()
	snap := cv.snapshotLocked()
	cv.mu.Unlock()

	res, err := cv.client.Chat(ctx, ChatRequest{
		Model:       cv.model,
		Messages:    snap,
		Temperature: cv.temperature,
	})
	if err != nil {
		// 失败时回滚刚加入的 user 消息，避免污染历史
		cv.mu.Lock()
		if n := len(cv.messages); n > 0 && cv.messages[n-1].Role == RoleUser && cv.messages[n-1].Content == userMsg {
			cv.messages = cv.messages[:n-1]
		}
		cv.mu.Unlock()
		return "", err
	}

	cv.mu.Lock()
	cv.messages = append(cv.messages, Message{Role: RoleAssistant, Content: res.Content})
	cv.trimLocked()
	cv.mu.Unlock()

	return res.Content, nil
}

// AskStream 流式提问；事件流结束后会自动把完整 assistant 消息追加到历史。
func (cv *Conversation) AskStream(ctx context.Context, userMsg string) (<-chan StreamEvent, error) {
	cv.mu.Lock()
	cv.messages = append(cv.messages, Message{Role: RoleUser, Content: userMsg})
	cv.trimLocked()
	snap := cv.snapshotLocked()
	cv.mu.Unlock()

	src, err := cv.client.StreamChat(ctx, ChatRequest{
		Model:       cv.model,
		Messages:    snap,
		Temperature: cv.temperature,
	})
	if err != nil {
		// 回滚 user 消息
		cv.mu.Lock()
		if n := len(cv.messages); n > 0 && cv.messages[n-1].Role == RoleUser && cv.messages[n-1].Content == userMsg {
			cv.messages = cv.messages[:n-1]
		}
		cv.mu.Unlock()
		return nil, err
	}

	out := make(chan StreamEvent, streamChanBufSize)
	go func() {
		defer close(out)
		var buf []byte
		var finalErr error
		for ev := range src {
			if ev.Err != nil {
				finalErr = ev.Err
			}
			if ev.Delta != "" {
				buf = append(buf, ev.Delta...)
			}
			out <- ev
		}
		// 完成后回填 assistant 消息（出错则不写入，并回滚 user 消息）
		cv.mu.Lock()
		defer cv.mu.Unlock()
		if finalErr != nil {
			if n := len(cv.messages); n > 0 && cv.messages[n-1].Role == RoleUser && cv.messages[n-1].Content == userMsg {
				cv.messages = cv.messages[:n-1]
			}
			return
		}
		cv.messages = append(cv.messages, Message{Role: RoleAssistant, Content: string(buf)})
		cv.trimLocked()
	}()
	return out, nil
}

// snapshotLocked 拼接 system prompt + 历史消息，返回独立切片
// 调用方必须持有 cv.mu
func (cv *Conversation) snapshotLocked() []Message {
	out := make([]Message, 0, len(cv.messages)+1)
	if cv.systemPrompt != "" {
		out = append(out, Message{Role: RoleSystem, Content: cv.systemPrompt})
	}
	out = append(out, cv.messages...)
	return out
}

// trimLocked 按 maxHistory 截断
// 调用方必须持有 cv.mu
func (cv *Conversation) trimLocked() {
	if cv.maxHistory <= 0 {
		return
	}
	if extra := len(cv.messages) - cv.maxHistory; extra > 0 {
		cv.messages = append([]Message(nil), cv.messages[extra:]...)
	}
}
