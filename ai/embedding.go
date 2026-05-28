// Package ai Embeddings 接口
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

// EmbedRequest Embedding 请求
type EmbedRequest struct {
	// Model 嵌入模型，例如 text-embedding-3-small / embedding-3 / bge-m3
	Model string
	// Input 待嵌入的文本（一条或多条）
	Input []string
	// Dimensions 期望的输出维度（仅 text-embedding-3 系列等支持），0 表示用默认
	Dimensions int64
	// User 终端用户标识
	User string
}

// EmbedResult Embedding 返回结果
type EmbedResult struct {
	// Vectors 与 Input 一一对应的向量
	Vectors [][]float64
	// Model 实际使用的模型
	Model string
	// Usage Token 用量
	Usage Usage
}

// Embed 把一组文本转成向量
func (c *OpenAIClient) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(req.Input) == 0 {
		return nil, errors.New("ai: embed input 不能为空")
	}
	model := req.Model
	if model == "" {
		return nil, errors.New("ai: embed model 不能为空")
	}

	cli := c.raw()
	params := openai.EmbeddingNewParams{
		Model: model,
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: req.Input,
		},
	}
	if req.Dimensions > 0 {
		params.Dimensions = param.NewOpt(req.Dimensions)
	}
	if req.User != "" {
		params.User = param.NewOpt(req.User)
	}

	var res *openai.CreateEmbeddingResponse
	start := time.Now()
	err := c.withRetry(ctx, "embed", func() error {
		r, e := cli.Embeddings.New(ctx, params)
		if e != nil {
			return e
		}
		res = r
		return nil
	})
	latency := time.Since(start)
	if err != nil {
		c.emitEvent(CallEvent{Op: "embed", Model: model, Latency: latency, Err: err})
		return nil, fmt.Errorf("ai: embedding failed: %w", err)
	}
	if res == nil {
		return nil, errors.New("ai: embedding empty response")
	}

	vectors := make([][]float64, 0, len(res.Data))
	for _, d := range res.Data {
		vectors = append(vectors, d.Embedding)
	}
	out := &EmbedResult{
		Vectors: vectors,
		Model:   res.Model,
		Usage: Usage{
			PromptTokens: res.Usage.PromptTokens,
			TotalTokens:  res.Usage.TotalTokens,
		},
	}
	c.emitEvent(CallEvent{Op: "embed", Model: out.Model, Latency: latency, Usage: out.Usage})
	return out, nil
}

// EmbedOne 单条文本的便捷方法
func (c *OpenAIClient) EmbedOne(ctx context.Context, text, model string) ([]float64, error) {
	res, err := c.Embed(ctx, EmbedRequest{Model: model, Input: []string{text}})
	if err != nil {
		return nil, err
	}
	if len(res.Vectors) == 0 {
		return nil, errors.New("ai: embedding 返回为空")
	}
	return res.Vectors[0], nil
}
