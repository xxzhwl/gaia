// Package ai Stream（流式）调用实现
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"errors"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/g"
)

// streamChanBufSize 流式 channel 默认缓冲数，避免消费者慢导致 goroutine 阻塞
const streamChanBufSize = 32

// StreamChat 发起一次流式 Chat 请求，返回事件 channel。
//
// 调用方应使用 for ev := range ch 消费事件，并根据 ev.Err / ev.Done 判断结束。
// 通过 ctx 可以取消请求，goroutine 会随之退出并关闭 channel。
func (c *OpenAIClient) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	params, err := c.buildChatParams(req)
	if err != nil {
		return nil, err
	}

	cli := c.raw()
	stream := cli.Chat.Completions.NewStreaming(ctx, params)
	out := make(chan StreamEvent, streamChanBufSize)
	model := string(params.Model)
	start := time.Now()

	g.Go(func() {
		defer close(out)
		// 关闭底层 SSE 连接，防止资源泄漏
		defer func() {
			if cerr := stream.Close(); cerr != nil {
				gaia.DebugF("ai: stream.Close err: %s", cerr.Error())
			}
		}()

		acc := openai.ChatCompletionAccumulator{}
		var finishReason string

		for stream.Next() {
			// ctx 已取消则提前退出
			if ctx.Err() != nil {
				c.emitEvent(CallEvent{Op: "chat_stream", Model: model, Latency: time.Since(start), Err: ctx.Err()})
				out <- StreamEvent{Done: true, Err: ctx.Err()}
				return
			}

			chunk := stream.Current()
			acc.AddChunk(chunk)

			if len(chunk.Choices) > 0 {
				ch0 := chunk.Choices[0]
				if ch0.FinishReason != "" {
					finishReason = ch0.FinishReason
				}
				if ch0.Delta.Content != "" {
					out <- StreamEvent{Delta: ch0.Delta.Content}
				}
			}
		}

		if err := stream.Err(); err != nil {
			c.emitEvent(CallEvent{Op: "chat_stream", Model: model, Latency: time.Since(start), Err: err})
			gaia.ErrorF("ai: stream read err: %s", err.Error())
			out <- StreamEvent{Done: true, Err: err}
			return
		}

		usage := Usage{
			PromptTokens:     acc.Usage.PromptTokens,
			CompletionTokens: acc.Usage.CompletionTokens,
			TotalTokens:      acc.Usage.TotalTokens,
		}
		c.emitEvent(CallEvent{Op: "chat_stream", Model: model, Latency: time.Since(start), Usage: usage})

		// 正常结束
		out <- StreamEvent{
			Done:         true,
			FinishReason: finishReason,
			Usage:        usage,
		}
	})

	return out, nil
}

// StreamChatOnce 简化版：只传 user 消息，返回纯 delta 文本通道。
//
// 注意：此通道不区分错误，错误只会被记录到日志；
// 想要拿到完整事件请直接使用 StreamChat。
func (c *OpenAIClient) StreamChatOnce(ctx context.Context, msg, model string) (<-chan string, error) {
	events, err := c.StreamChat(ctx, ChatRequest{
		Model:    model,
		Messages: []Message{{Role: RoleUser, Content: msg}},
	})
	if err != nil {
		return nil, err
	}
	out := make(chan string, streamChanBufSize)
	g.Go(func() {
		defer close(out)
		for ev := range events {
			if ev.Err != nil {
				gaia.ErrorF("ai: StreamChatOnce err: %s", ev.Err.Error())
				return
			}
			if ev.Delta != "" {
				out <- ev.Delta
			}
		}
	})
	return out, nil
}

// CollectStream 把流式事件聚合成完整的 ChatResult
//
// 适用于"实现用了流式接口、但当前调用点想要完整结果"的场景。
func CollectStream(events <-chan StreamEvent) (*ChatResult, error) {
	if events == nil {
		return nil, errors.New("ai: events 不能为 nil")
	}
	res := &ChatResult{}
	var buf []byte
	for ev := range events {
		if ev.Err != nil {
			return nil, ev.Err
		}
		if ev.Delta != "" {
			buf = append(buf, ev.Delta...)
		}
		if ev.Done {
			res.FinishReason = ev.FinishReason
			res.Usage = ev.Usage
		}
	}
	res.Content = string(buf)
	return res, nil
}
