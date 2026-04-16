// Package ai 包注释
// @author wanlizhan
// @created 2025-12-27
package ai

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/g"
)

type OpenAIClient struct {
	BaseUrl string
	ApiKey  string
}

func NewOpenAIClient(baseUrl, apiKey string) *OpenAIClient {
	return &OpenAIClient{
		BaseUrl: baseUrl,
		ApiKey:  apiKey,
	}
}

func NewOpenAIClientBySchema(schema string) (*OpenAIClient, error) {
	o := &OpenAIClient{}
	return o, gaia.LoadConfToObjWithErr(schema, &o)
}

func (c *OpenAIClient) ChatWithModel(msg, model string) (string, error) {
	client := openai.NewClient(
		option.WithBaseURL(c.BaseUrl),
		option.WithAPIKey(c.ApiKey), // or set OPENAI_API_KEY in your env
	)
	res, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(msg),
		},
		Model: model,
	})
	if err != nil {
		return "", err
	}

	return res.RawJSON(), nil
}

// StreamChat 流式聊天方法
func (c *OpenAIClient) StreamChat(msg, model string) (chan string, error) {
	client := openai.NewClient(
		option.WithBaseURL(c.BaseUrl),
		option.WithAPIKey(c.ApiKey), // or set OPENAI_API_KEY in your env
	)
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(msg),
		},
		Model: model,
	})
	acc := openai.ChatCompletionAccumulator{}
	res := make(chan string)
	g.Go(func() {
		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)

			// it's best to use chunks after handling JustFinished events
			if len(chunk.Choices) > 0 {
				res <- chunk.Choices[0].Delta.Content

			}
		}

		if stream.Err() != nil {
			gaia.ErrorF("ReadStreamERR:%s", stream.Err())
		}
		close(res)
	})
	return res, nil
}
