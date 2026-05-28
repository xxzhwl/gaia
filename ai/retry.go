// Package ai 应用层重试 / 指数退避
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/xxzhwl/gaia"
)

const (
	defaultRetryBaseDelay = 500 * time.Millisecond
	defaultRetryMaxDelay  = 10 * time.Second
)

// retryable 判断错误是否值得重试
//
// 命中条件（任一）：
//   - context 取消/超时：不重试
//   - openai.Error 且 StatusCode 为 429 / 408 / 5xx
//   - net.Error 且 Timeout()=true
//   - 字符串里出现常见的瞬时错误关键字
func retryable(err error) bool {
	if err == nil {
		return false
	}
	// context 错误一律不重试（调用方已主动取消/超时）
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 408, 409, 425, 429, 500, 502, 503, 504:
			return true
		}
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "temporarily unavailable"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "eof"),
		strings.Contains(msg, "broken pipe"):
		return true
	}
	return false
}

// backoff 计算第 attempt 次重试前的等待（指数退避 + 全抖动）
//
// attempt 从 1 开始（即第 1 次重试前的等待）
func backoff(attempt int, base, max time.Duration) time.Duration {
	if base <= 0 {
		base = defaultRetryBaseDelay
	}
	if max <= 0 {
		max = defaultRetryMaxDelay
	}
	exp := base << (attempt - 1)
	if exp <= 0 || exp > max {
		exp = max
	}
	// 全抖动：[0, exp)
	return time.Duration(rand.Int63n(int64(exp)))
}

// withRetry 对 fn 应用应用层重试（指数退避 + 抖动）。
// 当 RetryMax<=0 时直接执行一次。
func (c *OpenAIClient) withRetry(ctx context.Context, op string, fn func() error) error {
	if c.RetryMax <= 0 {
		return fn()
	}

	var lastErr error
	for attempt := 0; attempt <= c.RetryMax; attempt++ {
		if attempt > 0 {
			wait := backoff(attempt, c.RetryBaseDelay, c.RetryMaxDelay)
			gaia.WarnF("ai: %s retry %d/%d after %s, last err: %s",
				op, attempt, c.RetryMax, wait, lastErr.Error())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}

		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable(err) {
			return err
		}
	}
	return lastErr
}
