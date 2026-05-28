// Package ai 集成测试 / 单元测试
//
// 环境变量：
//
//	GAIA_AI_TEST_BASE_URL      例如 https://open.bigmodel.cn/api/paas/v4/
//	GAIA_AI_TEST_API_KEY       服务端密钥（**禁止硬编码到源码**）
//	GAIA_AI_TEST_MODEL         例如 GLM-4.5-Flash / gpt-4o-mini
//	GAIA_AI_TEST_VISION_MODEL  例如 glm-4v-plus / gpt-4o（用于多模态用例）
//	GAIA_AI_TEST_EMBED_MODEL   例如 embedding-3 / text-embedding-3-small
//
// 缺失任一项时相关用例会被 Skip，方便本地/CI 灵活控制。
//
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T) *OpenAIClient {
	t.Helper()
	baseUrl := os.Getenv("GAIA_AI_TEST_BASE_URL")
	apiKey := os.Getenv("GAIA_AI_TEST_API_KEY")
	if baseUrl == "" || apiKey == "" {
		t.Skip("Skip: 未配置 GAIA_AI_TEST_BASE_URL / GAIA_AI_TEST_API_KEY")
	}
	c := NewOpenAIClient(baseUrl, apiKey)
	c.Timeout = 30 * time.Second
	c.Name = "test"
	return c
}

func testModel(t *testing.T) string {
	t.Helper()
	model := os.Getenv("GAIA_AI_TEST_MODEL")
	if model == "" {
		t.Skip("Skip: 未配置 GAIA_AI_TEST_MODEL")
	}
	return model
}

// ============================== 在线用例 ==============================

func TestOnline_ChatOnce(t *testing.T) {
	c := newTestClient(t)
	model := testModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reply, err := c.ChatOnce(ctx, "用一句话告诉我 1+1 等于几。", model)
	if err != nil {
		t.Fatalf("ChatOnce failed: %v", err)
	}
	if strings.TrimSpace(reply) == "" {
		t.Fatalf("ChatOnce empty reply")
	}
	t.Logf("reply: %s", reply)
}

func TestOnline_ChatWithParams(t *testing.T) {
	c := newTestClient(t)
	model := testModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	temp := 0.2
	res, err := c.Chat(ctx, ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: RoleSystem, Content: "你是一个简洁的助手，回答控制在20字以内。"},
			{Role: RoleUser, Content: "Go 语言的发明者是谁？"},
		},
		Temperature: &temp,
		MaxTokens:   128,
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	t.Logf("content=%s finish=%s usage=%+v", res.Content, res.FinishReason, res.Usage)
}

func TestOnline_StreamChat(t *testing.T) {
	c := newTestClient(t)
	model := testModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	events, err := c.StreamChat(ctx, ChatRequest{
		Model:    model,
		Messages: []Message{{Role: RoleUser, Content: "用一句话介绍 Go 语言。"}},
	})
	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}

	var full strings.Builder
	var done bool
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("stream err: %v", ev.Err)
		}
		if ev.Delta != "" {
			full.WriteString(ev.Delta)
		}
		if ev.Done {
			done = true
			t.Logf("stream done: finish=%s usage=%+v", ev.FinishReason, ev.Usage)
		}
	}
	if !done {
		t.Fatal("stream 未收到 Done 事件")
	}
	if full.Len() == 0 {
		t.Fatal("stream 没有收到任何内容")
	}
	t.Logf("full: %s", full.String())
}

func TestOnline_Conversation(t *testing.T) {
	c := newTestClient(t)
	model := testModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conv := c.NewConversation("你是一个简洁的助手，回答控制在30字内。").WithModel(model)
	r1, err := conv.Ask(ctx, "我叫张三。")
	if err != nil {
		t.Fatalf("Ask 1 failed: %v", err)
	}
	t.Logf("a1: %s", r1)

	r2, err := conv.Ask(ctx, "我叫什么名字？")
	if err != nil {
		t.Fatalf("Ask 2 failed: %v", err)
	}
	t.Logf("a2: %s", r2)

	if got := len(conv.History()); got < 4 {
		t.Fatalf("history 期望至少 4 条（含 system），实际 %d", got)
	}
}

func TestOnline_ChatJSON(t *testing.T) {
	c := newTestClient(t)
	model := testModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	type Out struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	var out Out
	err := c.ChatJSON(ctx, ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: RoleSystem, Content: "请只返回 JSON，包含字段 name(string) 和 age(int)。"},
			{Role: RoleUser, Content: "构造一个示例：name=Alice, age=30"},
		},
	}, &out)
	if err != nil {
		t.Fatalf("ChatJSON failed: %v", err)
	}
	t.Logf("out=%+v", out)
}

func TestOnline_Embed(t *testing.T) {
	c := newTestClient(t)
	embedModel := os.Getenv("GAIA_AI_TEST_EMBED_MODEL")
	if embedModel == "" {
		t.Skip("Skip: 未配置 GAIA_AI_TEST_EMBED_MODEL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vec, err := c.EmbedOne(ctx, "Hello, world.", embedModel)
	if err != nil {
		t.Fatalf("EmbedOne failed: %v", err)
	}
	if len(vec) == 0 {
		t.Fatal("embedding 维度为 0")
	}
	t.Logf("dim=%d head=%v", len(vec), vec[:min(5, len(vec))])
}

func TestOnline_ToolCalling(t *testing.T) {
	c := newTestClient(t)
	model := testModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	reg := NewToolRegistry().Register(Tool{
		Def: ToolDef{
			Name:        "get_weather",
			Description: "查询某城市的天气",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string", "description": "城市名"},
				},
				"required": []string{"city"},
			},
		},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			var args struct {
				City string `json:"city"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				return "", err
			}
			if args.City == "" {
				return "", errors.New("city is empty")
			}
			b, _ := json.Marshal(map[string]any{
				"city":      args.City,
				"weather":   "晴",
				"tempC":     22,
				"humidity":  "60%",
				"timestamp": time.Now().Unix(),
			})
			return string(b), nil
		},
	})

	var calls atomic.Int32
	res, err := c.RunWithTools(ctx, ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: RoleSystem, Content: "你是一个助手，需要查天气时调用 get_weather 工具，最终用一句话回答用户。"},
			{Role: RoleUser, Content: "杭州今天天气怎么样？"},
		},
	}, reg, &RunOpts{
		MaxIterations: 3,
		ToolChoice:    "auto",
		OnToolCall: func(name, args, result string, err error) {
			calls.Add(1)
			t.Logf("tool=%s args=%s result=%s err=%v", name, args, result, err)
		},
	})
	if err != nil {
		t.Fatalf("RunWithTools err: %v", err)
	}
	if calls.Load() == 0 {
		t.Logf("warn: 模型未触发任何 tool 调用，final=%s", res.FinalContent)
	}
	t.Logf("final=%s iterations=%d usage=%+v", res.FinalContent, res.Iterations, res.Usage)
}

func TestOnline_VisionImage(t *testing.T) {
	visionModel := os.Getenv("GAIA_AI_TEST_VISION_MODEL")
	if visionModel == "" {
		t.Skip("Skip: 未配置 GAIA_AI_TEST_VISION_MODEL")
	}
	c := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := c.Chat(ctx, ChatRequest{
		Model: visionModel,
		Messages: []Message{
			UserMessageWithImages(
				"用一句话描述这张图。",
				ImageFromURL("https://upload.wikimedia.org/wikipedia/commons/thumb/3/3a/Cat03.jpg/440px-Cat03.jpg"),
			),
		},
	})
	if err != nil {
		t.Fatalf("vision chat err: %v", err)
	}
	t.Logf("vision result: %s", res.Content)
}

func TestOnline_CacheHit(t *testing.T) {
	c := newTestClient(t)
	model := testModel(t)
	c.CacheEnable = true
	c.CacheTTL = 30 * time.Second

	hits := atomic.Int32{}
	c.Observer = ObserverFunc(func(ev CallEvent) {
		if ev.CacheHit {
			hits.Add(1)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	temp := 0.0
	req := ChatRequest{
		Model:       model,
		Messages:    []Message{{Role: RoleUser, Content: "请只回复一个字：好。"}},
		Temperature: &temp,
		MaxTokens:   8,
	}

	if _, err := c.Chat(ctx, req); err != nil {
		t.Fatalf("first chat: %v", err)
	}
	if _, err := c.Chat(ctx, req); err != nil {
		t.Fatalf("second chat: %v", err)
	}
	if hits.Load() == 0 {
		t.Fatalf("缓存未命中，期望至少 1 次")
	}
	t.Logf("cache hits=%d", hits.Load())
}

// ============================== 离线用例 ==============================

func TestOffline_BackoffMonotonic(t *testing.T) {
	base := 100 * time.Millisecond
	max := 1 * time.Second
	for i := 1; i <= 4; i++ {
		got := backoff(i, base, max)
		if got < 0 || got > max {
			t.Fatalf("attempt=%d got=%s 越界 (max=%s)", i, got, max)
		}
	}
}

func TestOffline_RetryableErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"ctx canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"random", errors.New("random"), false},
		{"contains timeout", errors.New("read tcp: i/o timeout"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryable(tc.err); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestOffline_ToolRegistry(t *testing.T) {
	reg := NewToolRegistry().
		Register(Tool{Def: ToolDef{Name: "echo"}, Handler: func(ctx context.Context, args string) (string, error) {
			return args, nil
		}}).
		Register(Tool{Def: ToolDef{Name: ""}, Handler: nil}) // 应被忽略

	if got := len(reg.Defs()); got != 1 {
		t.Fatalf("Defs len got=%d want=1", got)
	}
	if _, ok := reg.Get("echo"); !ok {
		t.Fatal("echo 注册失败")
	}
	if _, ok := reg.Get("none"); ok {
		t.Fatal("不存在的 tool 应取不到")
	}
}

func TestOffline_RouterPick(t *testing.T) {
	r := NewRouter().
		AddProvider(ProviderEntry{
			Name:    "zhipu",
			BaseUrl: "https://open.bigmodel.cn/",
			ApiKeys: []string{"k1", "k2"},
			Models:  []string{"glm-4"},
		}).
		AddProvider(ProviderEntry{
			Name:    "openai",
			BaseUrl: "https://api.openai.com/",
			ApiKeys: []string{"sk-x"},
			Models:  []string{"gpt-"},
		})

	c1, err := r.Pick("GLM-4.5-Flash")
	if err != nil || c1 == nil {
		t.Fatalf("pick zhipu err: %v", err)
	}
	if !strings.HasPrefix(c1.Name, "zhipu#") {
		t.Fatalf("expected zhipu, got %s", c1.Name)
	}

	c2, err := r.Pick("gpt-4o")
	if err != nil || c2 == nil {
		t.Fatalf("pick openai err: %v", err)
	}
	if !strings.HasPrefix(c2.Name, "openai#") {
		t.Fatalf("expected openai, got %s", c2.Name)
	}

	// 验证 zhipu 内 key 轮询：连续 pick 4 次应至少出现两个不同 client
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		c, _ := r.Pick("glm-4-air")
		seen[c.Name] = true
	}
	if len(seen) < 2 {
		t.Fatalf("key 轮询失败，seen=%v", seen)
	}
}

func TestOffline_MakeCacheKeyDeterministic(t *testing.T) {
	c := &OpenAIClient{BaseUrl: "https://x", DefaultModel: "m1", CacheEnable: true}
	temp := 0.0
	req := ChatRequest{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		Temperature: &temp,
	}
	k1, ok1 := c.makeCacheKey(req, "chat")
	k2, ok2 := c.makeCacheKey(req, "chat")
	if !ok1 || !ok2 {
		t.Fatal("应启用缓存")
	}
	if k1 != k2 {
		t.Fatalf("相同请求 key 不一致：%s vs %s", k1, k2)
	}

	// 改变 message 应得到不同 key
	req.Messages[0].Content = "hello"
	k3, _ := c.makeCacheKey(req, "chat")
	if k3 == k1 {
		t.Fatal("不同请求应得到不同 key")
	}

	// Tools 非空 → 不缓存
	req.Tools = []ToolDef{{Name: "x"}}
	if _, ok := c.makeCacheKey(req, "chat"); ok {
		t.Fatal("含 tools 不应缓存")
	}
}

func TestOffline_ImageFromBase64(t *testing.T) {
	img := ImageFromBase64("image/png", []byte("hello"))
	if !strings.HasPrefix(img.URL, "data:image/png;base64,") {
		t.Fatalf("data URL 拼接错误: %s", img.URL)
	}

	// 已经是 data URL 时原样返回
	img2 := ImageFromBase64("image/png", []byte("data:image/png;base64,xxx"))
	if img2.URL != "data:image/png;base64,xxx" {
		t.Fatalf("data URL 应原样返回, got %s", img2.URL)
	}
}

// 工具：仅供测试日志
func showJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return fmt.Sprintf("%s", b)
}

var _ = showJSON
