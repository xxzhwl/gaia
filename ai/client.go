// Package ai 基于 openai-go v3 SDK 的 LLM 能力封装
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"errors"
	"sync"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/xxzhwl/gaia"
)

// OpenAIClient 是对 openai-go SDK 的轻量封装，
// 支持 OpenAI、智谱 GLM、DeepSeek、Ollama 等所有兼容 OpenAI Chat Completions 协议的服务。
type OpenAIClient struct {
	// BaseUrl 服务地址，例如 https://open.bigmodel.cn/api/paas/v4/
	BaseUrl string `json:"BaseUrl"`
	// ApiKey 调用密钥
	ApiKey string `json:"ApiKey"`
	// DefaultModel 默认模型名，可在调用时覆盖
	DefaultModel string `json:"DefaultModel"`
	// Timeout 单次请求超时时间，0 表示不设置（默认 60s）
	Timeout time.Duration `json:"Timeout"`
	// MaxRetries SDK 层的重试次数（针对 5xx/网络抖动），0 走 SDK 默认值
	MaxRetries int `json:"MaxRetries"`

	// ====== 应用层重试（指数退避，覆盖更广的可重试错误） ======
	// RetryMax 应用层重试次数。0 表示不重试。
	RetryMax int `json:"RetryMax"`
	// RetryBaseDelay 第一次重试前等待时间，默认 500ms
	RetryBaseDelay time.Duration `json:"RetryBaseDelay"`
	// RetryMaxDelay 退避封顶，默认 10s
	RetryMaxDelay time.Duration `json:"RetryMaxDelay"`

	// ====== 缓存（针对幂等的 Chat/Embed 调用） ======
	// CacheEnable 是否启用缓存。仅 Temperature==0 或调用方在 ChatRequest.UseCache=true 时生效
	CacheEnable bool `json:"CacheEnable"`
	// CacheTTL 缓存时长，0 时取默认 10 分钟
	CacheTTL time.Duration `json:"CacheTTL"`

	// ====== 可观测性 ======
	// Observer 调用完成钩子，nil 时只打 Debug 日志
	Observer Observer `json:"-"`
	// Name 客户端名（用于日志/指标），可为空
	Name string `json:"Name"`

	once   sync.Once
	client openai.Client
}

// NewOpenAIClient 创建一个客户端实例
func NewOpenAIClient(baseUrl, apiKey string) *OpenAIClient {
	return &OpenAIClient{
		BaseUrl: baseUrl,
		ApiKey:  apiKey,
	}
}

// NewOpenAIClientBySchema 根据 gaia 配置 schema 创建客户端
//
// 配置示例：
//
//	{
//	  "OpenAI": {
//	    "BaseUrl": "https://api.openai.com/v1/",
//	    "ApiKey": "sk-xxx",
//	    "DefaultModel": "gpt-4o-mini",
//	    "Timeout": 60000000000,
//	    "MaxRetries": 2
//	  }
//	}
func NewOpenAIClientBySchema(schema string) (*OpenAIClient, error) {
	c := &OpenAIClient{}
	if err := gaia.LoadConfToObjWithErr(schema, c); err != nil {
		return nil, err
	}
	if c.BaseUrl == "" || c.ApiKey == "" {
		return nil, errors.New("ai: BaseUrl 和 ApiKey 不能为空")
	}
	return c, nil
}

// raw 返回缓存的 openai.Client。第一次调用时按配置初始化，后续复用。
func (c *OpenAIClient) raw() openai.Client {
	c.once.Do(func() {
		opts := []option.RequestOption{
			option.WithBaseURL(c.BaseUrl),
			option.WithAPIKey(c.ApiKey),
		}
		if c.Timeout > 0 {
			opts = append(opts, option.WithRequestTimeout(c.Timeout))
		}
		if c.MaxRetries > 0 {
			opts = append(opts, option.WithMaxRetries(c.MaxRetries))
		}
		c.client = openai.NewClient(opts...)
	})
	return c.client
}

// pickModel 选择最终使用的 model，优先用调用方指定的，否则回退到 DefaultModel
func (c *OpenAIClient) pickModel(model string) string {
	if model != "" {
		return model
	}
	return c.DefaultModel
}
