// Package ai 多 Provider / Key 池路由
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProviderEntry 单个 provider 配置（即一个上游服务 + 一组 key）
type ProviderEntry struct {
	// Name 别名，便于日志识别（如 "zhipu" / "openai" / "deepseek"）
	Name string
	// BaseUrl 服务地址
	BaseUrl string
	// ApiKeys 可用的 Key 列表，路由层会做轮询
	ApiKeys []string
	// Models 该 provider 支持的模型名集合（小写比较，前缀匹配）
	// 例如 ["glm-4", "glm-4.5"]，允许调用方传 "GLM-4-flash" 命中 "glm-4"
	Models []string
	// DefaultModel 该 provider 的默认模型
	DefaultModel string
	// Timeout 超时（0 表示不设置）
	Timeout time.Duration
	// SDK 层重试
	MaxRetries int
	// CacheEnable 是否启用请求缓存
	CacheEnable bool
	// RetryMax 应用层重试次数
	RetryMax int
	// Observer 允许为该 provider 下的所有 client 指定同一个观察者
	Observer Observer
}

// Router 多 Provider 路由器
//
// 用法：
//
//	r := ai.NewRouter().
//	    AddProvider(ai.ProviderEntry{Name:"zhipu", BaseUrl:"...", ApiKeys:[]string{"k1","k2"}, Models:[]string{"glm-4"}}).
//	    AddProvider(ai.ProviderEntry{Name:"openai", BaseUrl:"...", ApiKeys:[]string{"sk-..."}, Models:[]string{"gpt-4o"}})
//	res, _ := r.Chat(ctx, ai.ChatRequest{Model:"GLM-4.5-Flash", Messages: ...})
type Router struct {
	mu        sync.RWMutex
	providers []*providerSlot
}

type providerSlot struct {
	cfg    ProviderEntry
	keyIdx atomic.Uint32
	// clients[keyIndex] 与 cfg.ApiKeys 一一对应；懒构造
	clients []*OpenAIClient
	cMu     sync.Mutex // 保护 clients 列表的懒构造
}

// NewRouter 创建一个空 Router
func NewRouter() *Router { return &Router{} }

// AddProvider 注册一个 provider
func (r *Router) AddProvider(p ProviderEntry) *Router {
	if p.BaseUrl == "" || len(p.ApiKeys) == 0 {
		return r
	}
	slot := &providerSlot{cfg: p, clients: make([]*OpenAIClient, len(p.ApiKeys))}
	r.mu.Lock()
	r.providers = append(r.providers, slot)
	r.mu.Unlock()
	return r
}

// Pick 根据 model 选出一个可用 client
//
// 当 model 为空时返回第一个 provider 的轮询结果（便于"任选一个"的场景）。
func (r *Router) Pick(model string) (*OpenAIClient, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.providers) == 0 {
		return nil, errors.New("ai: router 未注册任何 provider")
	}

	modelLower := strings.ToLower(model)
	// 优先匹配 Models 列表
	if modelLower != "" {
		for _, p := range r.providers {
			for _, m := range p.cfg.Models {
				if strings.HasPrefix(modelLower, strings.ToLower(m)) {
					return slotPickClient(p), nil
				}
			}
		}
	}
	// 兜底：用第一个
	return slotPickClient(r.providers[0]), nil
}

// Chat 路由一次 Chat 请求
func (r *Router) Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	cli, err := r.Pick(req.Model)
	if err != nil {
		return nil, err
	}
	return cli.Chat(ctx, req)
}

// StreamChat 路由一次流式请求
func (r *Router) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	cli, err := r.Pick(req.Model)
	if err != nil {
		return nil, err
	}
	return cli.StreamChat(ctx, req)
}

// Embed 路由一次嵌入请求（按 model 选 provider）
func (r *Router) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	cli, err := r.Pick(req.Model)
	if err != nil {
		return nil, err
	}
	return cli.Embed(ctx, req)
}

// slotPickClient 在一个 provider slot 上以原子轮询挑一个 client
func slotPickClient(slot *providerSlot) *OpenAIClient {
	n := uint32(len(slot.cfg.ApiKeys))
	idx := slot.keyIdx.Add(1) - 1
	pos := idx % n

	slot.cMu.Lock()
	defer slot.cMu.Unlock()
	if c := slot.clients[pos]; c != nil {
		return c
	}
	// 懒构造
	c := buildClientFromSlot(slot, int(pos))
	slot.clients[pos] = c
	return c
}

func buildClientFromSlot(slot *providerSlot, keyIdx int) *OpenAIClient {
	c := &OpenAIClient{
		BaseUrl:      slot.cfg.BaseUrl,
		ApiKey:       slot.cfg.ApiKeys[keyIdx],
		DefaultModel: slot.cfg.DefaultModel,
		Timeout:      slot.cfg.Timeout,
		MaxRetries:   slot.cfg.MaxRetries,
		RetryMax:     slot.cfg.RetryMax,
		CacheEnable:  slot.cfg.CacheEnable,
		Observer:     slot.cfg.Observer,
		Name:         fmt.Sprintf("%s#%d", slot.cfg.Name, keyIdx),
	}
	return c
}
