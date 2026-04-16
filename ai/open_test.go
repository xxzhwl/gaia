// Package ai 包注释
// @author wanlizhan
// @created 2025-12-27
package ai

import (
	"context"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func TestName(t *testing.T) {
	client := openai.NewClient(
		option.WithBaseURL("https://open.bigmodel.cn/api/paas/v4/"),
		option.WithAPIKey("4d8b1b29ca8048eaafe4a9b17c9647ba.1ppg8lu5THTImzS7"), // or set OPENAI_API_KEY in your env
	)

	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage("今天是什么日子？"),
		},
		Model: "GLM-4.5-Flash",
	})
	acc := openai.ChatCompletionAccumulator{}

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		// it's best to use chunks after handling JustFinished events
		if len(chunk.Choices) > 0 {
			println(chunk.Choices[0].FinishReason)
		}
	}

	if stream.Err() != nil {
		panic(stream.Err())
	}
}

func TestNewOpenAIClient(t *testing.T) {
	client := NewOpenAIClient("https://open.bigmodel.cn/api/paas/v4/", "4d8b1b29ca8048eaafe4a9b17c9647ba.1ppg8lu5THTImzS7")
	chat, err := client.StreamChat("今天啥日子", "GLM-4.5-Flash")
	if err != nil {
		t.Fatal(err)
	}

	for v := range chat {
		if v == "\n" {
			continue
		}
		println(v)
	}
}
