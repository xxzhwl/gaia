// Package ai Function/Tool Calling
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// ToolDef 是暴露给模型的"函数声明"
//
// Schema 应当遵循 JSON Schema：
//
//	{
//	  "type": "object",
//	  "properties": {"city": {"type": "string"}},
//	  "required": ["city"]
//	}
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	// Strict 是否启用严格 schema（仅 OpenAI 部分模型支持）
	Strict bool `json:"strict,omitempty"`
}

// ToolHandler 工具的本地执行函数
//
// argsJSON 是模型生成的 JSON 参数串（未必是合法 JSON），
// 实现方应自行校验/反序列化，并返回 string 形式的结果（一般是 JSON）。
type ToolHandler func(ctx context.Context, argsJSON string) (string, error)

// Tool 把 ToolDef 与本地执行函数绑定起来
type Tool struct {
	Def     ToolDef
	Handler ToolHandler
}

// ToolRegistry 维护一组可被模型调用的工具
type ToolRegistry struct {
	tools map[string]Tool
}

// NewToolRegistry 创建一个空注册表
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register 注册工具，重名会被覆盖
func (r *ToolRegistry) Register(t Tool) *ToolRegistry {
	if t.Def.Name == "" || t.Handler == nil {
		return r
	}
	r.tools[t.Def.Name] = t
	return r
}

// Defs 返回所有工具的声明（用于 ChatRequest.Tools）
func (r *ToolRegistry) Defs() []ToolDef {
	out := make([]ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Def)
	}
	return out
}

// Get 取出某个工具
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// RunOpts 控制 RunWithTools 的行为
type RunOpts struct {
	// MaxIterations 最大循环轮数（防止模型陷入死循环）。<=0 时取默认 5。
	MaxIterations int
	// ToolChoice 透传给 ChatRequest.ToolChoice，默认 "auto"
	ToolChoice string
	// OnToolCall 调试钩子：每次执行 tool 前后回调；可选
	OnToolCall func(name string, args string, result string, err error)
}

// RunResult RunWithTools 的最终结果
type RunResult struct {
	// FinalContent 模型最后一次自然语言回复（无 tool_calls 时）
	FinalContent string
	// Iterations 实际进行了几轮交互
	Iterations int
	// Messages 完整对话过程（含 tool 消息），方便审计/继续会话
	Messages []Message
	// Usage 累积 Token 用量
	Usage Usage
}

// RunWithTools 自动驱动一次"模型 ↔ 工具"循环：
//
//  1. 把 req.Messages 发给模型
//  2. 如果模型返回 tool_calls，本地执行对应工具，把结果作为 role=tool 消息追加，再回到第 1 步
//  3. 直到模型不再请求工具或达到 MaxIterations
//
// 注意：调用方传入的 ChatRequest 中如果设置了 Tools 会被忽略，以 reg.Defs() 为准。
func (c *OpenAIClient) RunWithTools(ctx context.Context, req ChatRequest, reg *ToolRegistry, opts *RunOpts) (*RunResult, error) {
	if reg == nil || len(reg.tools) == 0 {
		return nil, errors.New("ai: ToolRegistry 为空")
	}
	if opts == nil {
		opts = &RunOpts{}
	}
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 5
	}
	choice := opts.ToolChoice
	if choice == "" {
		choice = "auto"
	}

	// 拷贝 messages，避免修改调用方的切片
	messages := append([]Message(nil), req.Messages...)
	out := &RunResult{}

	for i := 0; i < opts.MaxIterations; i++ {
		out.Iterations = i + 1

		callReq := req
		callReq.Messages = messages
		callReq.Tools = reg.Defs()
		callReq.ToolChoice = choice

		res, err := c.Chat(ctx, callReq)
		if err != nil {
			return nil, fmt.Errorf("ai: RunWithTools iter=%d chat err: %w", i+1, err)
		}
		out.Usage.PromptTokens += res.Usage.PromptTokens
		out.Usage.CompletionTokens += res.Usage.CompletionTokens
		out.Usage.TotalTokens += res.Usage.TotalTokens

		// 没有 tool_calls 就视为最终回复
		if len(res.ToolCalls) == 0 {
			out.FinalContent = res.Content
			out.Messages = append(messages, Message{Role: RoleAssistant, Content: res.Content})
			return out, nil
		}

		// 把 assistant 的 tool_calls 消息追加进历史
		messages = append(messages, Message{
			Role:      RoleAssistant,
			Content:   res.Content,
			ToolCalls: res.ToolCalls,
		})

		// 逐个执行
		for _, call := range res.ToolCalls {
			tool, ok := reg.Get(call.Name)
			var result string
			var execErr error
			if !ok {
				execErr = fmt.Errorf("tool %q not registered", call.Name)
			} else {
				result, execErr = tool.Handler(ctx, call.Arguments)
			}
			if opts.OnToolCall != nil {
				opts.OnToolCall(call.Name, call.Arguments, result, execErr)
			}

			content := result
			if execErr != nil {
				// 把错误也作为 tool 消息回喂给模型，让它自行修正
				content = encodeToolError(execErr)
			}
			messages = append(messages, Message{
				Role:       RoleTool,
				Content:    content,
				ToolCallID: call.ID,
				Name:       call.Name,
			})
		}
	}

	out.Messages = messages
	return out, fmt.Errorf("ai: RunWithTools 超过最大迭代次数 %d", opts.MaxIterations)
}

// encodeToolError 把工具执行错误编码为 JSON 字符串，方便模型理解
func encodeToolError(err error) string {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}
