// Package ratelimiter 多限流器组合（Multi-Dimension Limiter）。
//
// 业务上常见诉求是同时叠加多个维度的限流，例如：
//   - 单用户每秒最多 10 次 + 全局每秒最多 1000 次
//   - 单 IP 每分钟最多 60 次 + 单接口每秒 200 次
//
// MultiLimiter 将多个 Limiter 串联，要求 全部通过 才放行（AND 语义）。
// 任一限流器拒绝则整体拒绝。
//
// 典型用法：
//
//	userLimiter   := ratelimiter.NewLocalLimiter(10, 5)
//	globalLimiter := ratelimiter.NewLocalLimiter(1000, 200)
//	multi := ratelimiter.NewMultiLimiter(userLimiter, globalLimiter)
//	if ok, _ := multi.AllowCtx(ctx); ok { /* 放行 */ }
//
// 注意：当前实现中如果前面的限流器已消耗了名额，但后面某个限流器拒绝，
// 已消耗的名额不会回滚（这是限流领域的常见折中——回滚成本高且无标准 API）。
// 如有严格诉求，应将"最严格"的限流器放在最前面，使其作为快速失败入口。
//
// @author wanlizhan
// @created 2026-05-28
package ratelimiter

import (
	"context"
	"errors"
)

// MultiLimiter 多限流器组合（AND 语义）
type MultiLimiter struct {
	limiters []Limiter
}

// NewMultiLimiter 组合多个限流器，按传入顺序依次评估
func NewMultiLimiter(limiters ...Limiter) *MultiLimiter {
	// 过滤 nil，避免运行时 panic
	cleaned := make([]Limiter, 0, len(limiters))
	for _, l := range limiters {
		if l != nil {
			cleaned = append(cleaned, l)
		}
	}
	return &MultiLimiter{limiters: cleaned}
}

// AllowCtx 实现 Limiter 接口；全部通过才返回 true
//
// 任一限流器返回 error 时，立即终止后续评估并返回该 error。
func (m *MultiLimiter) AllowCtx(ctx context.Context) (bool, error) {
	if len(m.limiters) == 0 {
		return true, nil
	}
	var firstErr error
	for _, l := range m.limiters {
		ok, err := l.AllowCtx(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			// 出错视为拒绝，但继续收集后续状态信息没意义，立即返回
			return false, firstErr
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// Add 追加一个限流器（非并发安全，建议仅在初始化阶段调用）
func (m *MultiLimiter) Add(l Limiter) *MultiLimiter {
	if l != nil {
		m.limiters = append(m.limiters, l)
	}
	return m
}

// Len 当前组合的限流器数量
func (m *MultiLimiter) Len() int {
	return len(m.limiters)
}

// ErrNoLimiter 没有任何限流器（保留供调用方判断使用）
var ErrNoLimiter = errors.New("ratelimiter: 没有配置任何限流器")
