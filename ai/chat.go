// Package ai Chat（非流式）调用实现
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/xxzhwl/gaia"
)

// Chat 同步发起一次 Chat 请求并返回完整结果
func (c *OpenAIClient) Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	params, err := c.buildChatParams(req)
	if err != nil {
		return nil, err
	}

	model := string(params.Model)

	// 1. 命中缓存？
	cacheKey, useCache := c.makeCacheKey(req, "chat")
	if useCache {
		if cached, ok := getCache[*ChatResult](cacheKey); ok && cached != nil {
			c.emitEvent(CallEvent{Op: "chat", Model: model, CacheHit: true, Usage: cached.Usage})
			return cached, nil
		}
	}

	cli := c.raw()
	start := time.Now()

	var res *openai.ChatCompletion
	err = c.withRetry(ctx, "chat", func() error {
		r, e := cli.Chat.Completions.New(ctx, params)
		if e != nil {
			return e
		}
		res = r
		return nil
	})

	latency := time.Since(start)
	if err != nil {
		c.emitEvent(CallEvent{Op: "chat", Model: model, Latency: latency, Err: err})
		return nil, fmt.Errorf("ai: chat completions failed: %w", err)
	}
	if res == nil || len(res.Choices) == 0 {
		c.emitEvent(CallEvent{Op: "chat", Model: model, Latency: latency, Err: errors.New("empty choices")})
		return nil, errors.New("ai: empty choices in response")
	}
	choice := res.Choices[0]
	out := &ChatResult{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		Model:        res.Model,
		Usage: Usage{
			PromptTokens:     res.Usage.PromptTokens,
			CompletionTokens: res.Usage.CompletionTokens,
			TotalTokens:      res.Usage.TotalTokens,
		},
		ToolCalls: convertRespToolCalls(choice.Message.ToolCalls),
		Raw:       res.RawJSON(),
	}

	c.emitEvent(CallEvent{Op: "chat", Model: out.Model, Latency: latency, Usage: out.Usage})

	if useCache && len(out.ToolCalls) == 0 {
		// 含 tool_calls 的结果不入缓存（语义上每次都要重新决策）
		setCache(cacheKey, out, c.cacheTTL())
	}
	return out, nil
}

// ChatOnce 简化版：只传一句 user 消息和 model，返回纯文本。
//
// 适用于一次性提问的场景。需要更多控制时请使用 Chat。
func (c *OpenAIClient) ChatOnce(ctx context.Context, msg, model string) (string, error) {
	res, err := c.Chat(ctx, ChatRequest{
		Model:    model,
		Messages: []Message{{Role: RoleUser, Content: msg}},
	})
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// ChatJSON 让模型以 JSON 模式回复，并自动反序列化到 out（必须是指针）。
//
// 内部会自动开启 response_format=json_object。
// 如果你的 prompt 里没有"请返回 JSON"之类的指示，部分模型会拒绝该请求；
// 调用方应在 messages 中明确说明输出 schema。
func (c *OpenAIClient) ChatJSON(ctx context.Context, req ChatRequest, out any) error {
	if out == nil {
		return errors.New("ai: ChatJSON out 不能为 nil")
	}
	req.JSONMode = true
	res, err := c.Chat(ctx, req)
	if err != nil {
		return err
	}
	if res.Content == "" {
		return errors.New("ai: ChatJSON 模型返回内容为空")
	}
	if err := json.Unmarshal([]byte(res.Content), out); err != nil {
		return fmt.Errorf("ai: ChatJSON 反序列化失败: %w, raw=%s", err, res.Content)
	}
	return nil
}

// buildChatParams 把通用 ChatRequest 转换为 openai-go 的 ChatCompletionNewParams
func (c *OpenAIClient) buildChatParams(req ChatRequest) (openai.ChatCompletionNewParams, error) {
	model := c.pickModel(req.Model)
	if model == "" {
		return openai.ChatCompletionNewParams{}, errors.New("ai: model 不能为空（请设置 ChatRequest.Model 或 OpenAIClient.DefaultModel）")
	}
	if len(req.Messages) == 0 {
		return openai.ChatCompletionNewParams{}, errors.New("ai: messages 不能为空")
	}

	msgs, err := convertMessages(req.Messages)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: msgs,
	}
	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = param.NewOpt(*req.TopP)
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = param.NewOpt(req.MaxTokens)
	}
	if req.Seed != nil {
		params.Seed = param.NewOpt(*req.Seed)
	}
	if req.User != "" {
		params.User = param.NewOpt(req.User)
	}
	if len(req.Stop) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{
			OfStringArray: req.Stop,
		}
	}
	if req.JSONMode {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	}
	if len(req.Tools) > 0 {
		params.Tools = convertTools(req.Tools)
		if tc := buildToolChoice(req.ToolChoice); tc != nil {
			params.ToolChoice = *tc
		}
	}
	return params, nil
}

// convertMessages 把通用 Message 转成 openai-go 的 union 类型
func convertMessages(in []Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(in))
	for i, m := range in {
		switch m.Role {
		case RoleSystem:
			out = append(out, openai.SystemMessage(m.Content))
		case RoleUser:
			out = append(out, buildUserMessage(m))
		case RoleAssistant:
			out = append(out, buildAssistantMessage(m))
		case RoleTool:
			if m.ToolCallID == "" {
				return nil, fmt.Errorf("ai: messages[%d] role=tool 必须设置 ToolCallID", i)
			}
			out = append(out, openai.ToolMessage(m.Content, m.ToolCallID))
		default:
			return nil, fmt.Errorf("ai: messages[%d] 未知角色 %q", i, m.Role)
		}
	}
	return out, nil
}

// buildUserMessage 支持纯文本 / 文本+图片
func buildUserMessage(m Message) openai.ChatCompletionMessageParamUnion {
	if len(m.Images) == 0 {
		return openai.UserMessage(m.Content)
	}
	parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(m.Images)+1)
	if m.Content != "" {
		parts = append(parts, openai.TextContentPart(m.Content))
	}
	for _, img := range m.Images {
		parts = append(parts, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL:    img.URL,
			Detail: img.Detail,
		}))
	}
	return openai.UserMessage(parts)
}

// buildAssistantMessage 支持文本 + tool_calls 历史回填
func buildAssistantMessage(m Message) openai.ChatCompletionMessageParamUnion {
	if len(m.ToolCalls) == 0 {
		return openai.AssistantMessage(m.Content)
	}
	asst := openai.ChatCompletionAssistantMessageParam{}
	if m.Content != "" {
		asst.Content.OfString = param.NewOpt(m.Content)
	}
	calls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		calls = append(calls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: tc.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			},
		})
	}
	asst.ToolCalls = calls
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &asst}
}

// convertTools 把通用 ToolDef 转为 SDK 类型
func convertTools(defs []ToolDef) []openai.ChatCompletionToolUnionParam {
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(defs))
	for _, d := range defs {
		fn := shared.FunctionDefinitionParam{
			Name: d.Name,
		}
		if d.Description != "" {
			fn.Description = param.NewOpt(d.Description)
		}
		if d.Strict {
			fn.Strict = param.NewOpt(true)
		}
		if len(d.Parameters) > 0 {
			fn.Parameters = shared.FunctionParameters(d.Parameters)
		}
		out = append(out, openai.ChatCompletionFunctionTool(fn))
	}
	return out
}

// buildToolChoice 解析 ChatRequest.ToolChoice
//
// 取值含义：
//   - ""        : 不设置（让 SDK 默认行为）
//   - "auto"    : 让模型自行决定
//   - "none"    : 禁止调用 tool
//   - "required": 必须调用某个 tool
//   - 其它      : 视为函数名，强制调用该函数
func buildToolChoice(s string) *openai.ChatCompletionToolChoiceOptionUnionParam {
	if s == "" {
		return nil
	}
	switch s {
	case "auto", "none", "required":
		return &openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt(s),
		}
	default:
		return &openai.ChatCompletionToolChoiceOptionUnionParam{
			OfFunctionToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
				Function: openai.ChatCompletionNamedToolChoiceFunctionParam{
					Name: s,
				},
			},
		}
	}
}

// convertRespToolCalls 把 SDK 响应 tool_calls 转成通用 ToolCall
func convertRespToolCalls(in []openai.ChatCompletionMessageToolCallUnion) []ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(in))
	for _, u := range in {
		// Type=="function" 时取 Function；其它（custom）暂不支持
		if u.Type != "" && u.Type != "function" {
			gaia.WarnF("ai: 暂不支持的 tool_call 类型 %q，跳过", u.Type)
			continue
		}
		out = append(out, ToolCall{
			ID:        u.ID,
			Name:      u.Function.Name,
			Arguments: u.Function.Arguments,
		})
	}
	return out
}

// makeCacheKey 计算缓存 key
//
// 仅当 c.CacheEnable && (req.UseCache || Temperature==0) 时返回 useCache=true
// 含 Tools 的请求不缓存（语义不幂等）。
func (c *OpenAIClient) makeCacheKey(req ChatRequest, op string) (string, bool) {
	if !c.CacheEnable {
		return "", false
	}
	if len(req.Tools) > 0 {
		return "", false
	}
	deterministic := req.UseCache
	if !deterministic && req.Temperature != nil && *req.Temperature == 0 {
		deterministic = true
	}
	if !deterministic {
		return "", false
	}

	// 把对结果有影响的字段聚合后做哈希
	digest := struct {
		BaseUrl  string
		Op       string
		Model    string
		Messages []Message
		Temp     *float64
		TopP     *float64
		MaxTok   int64
		Stop     []string
		Seed     *int64
		User     string
		JSONMode bool
	}{
		BaseUrl:  c.BaseUrl,
		Op:       op,
		Model:    c.pickModel(req.Model),
		Messages: req.Messages,
		Temp:     req.Temperature,
		TopP:     req.TopP,
		MaxTok:   req.MaxTokens,
		Stop:     req.Stop,
		Seed:     req.Seed,
		User:     req.User,
		JSONMode: req.JSONMode,
	}
	b, _ := json.Marshal(digest)
	sum := sha256.Sum256(b)
	return "ai:cache:" + hex.EncodeToString(sum[:]), true
}

func (c *OpenAIClient) cacheTTL() time.Duration {
	if c.CacheTTL > 0 {
		return c.CacheTTL
	}
	return 10 * time.Minute
}

// getCache 从 gaia.Cache 中按类型读取
func getCache[T any](key string) (T, bool) {
	var zero T
	v := gaia.NewCache().Get(key)
	if v == nil {
		return zero, false
	}
	t, ok := v.(T)
	if !ok {
		return zero, false
	}
	return t, true
}

func setCache(key string, value any, ttl time.Duration) {
	gaia.NewCache().Set(key, value, ttl)
}
