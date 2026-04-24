// Package redis 分布式限流（滑动窗口 + 固定窗口）
//
// 典型用法：
//
//	cli := redis.NewFrameworkClient()
//
//	// 滑动窗口：每秒最多 100 次请求
//	allowed, _ := cli.RateLimitAllow("api:/users", 100, time.Second)
//
//	// 固定窗口：每分钟最多 1000 次
//	allowed, _ := cli.FixedWindowAllow("api:/orders", 1000, time.Minute)
//
//	// 剩余额度
//	remaining, _ := cli.RateLimitRemaining("api:/users", 100, time.Second)
//
// @author wanlizhan
// @created 2026-04-24
package redis

import (
	"fmt"
	"time"
)

// slidingWindowScript Redis Lua 脚本：滑动窗口限流
const slidingWindowScript = `
local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local clearBefore = now - window

redis.call('ZREMRANGEBYSCORE', key, 0, clearBefore)
local count = redis.call('ZCARD', key)

if count < limit then
    redis.call('ZADD', key, now, now .. '-' .. math.random(1000000))
    redis.call('PEXPIRE', key, window)
    return 1
else
    redis.call('PEXPIRE', key, window)
    return 0
end
`

// RateLimitAllow 分布式滑动窗口限流
//   - key: 限流标识（如 "api:/users"、"user:12345"）
//   - limit: 窗口内最大允许请求数
//   - window: 滑动窗口大小
//
// 返回 true 表示放行，false 表示限流
func (c *Client) RateLimitAllow(key string, limit int, window time.Duration) (bool, error) {
	fullKey := fmt.Sprintf("ratelimit:%s", key)
	now := time.Now().UnixMilli()
	result, err := c.c.Eval(c.ctx, slidingWindowScript,
		[]string{fullKey},
		window.Milliseconds(),
		limit,
		now,
	).Int()
	if err != nil {
		return false, fmt.Errorf("限流脚本执行失败: %w", err)
	}
	return result == 1, nil
}

// RateLimitRemaining 获取滑动窗口内剩余可用额度
func (c *Client) RateLimitRemaining(key string, limit int, window time.Duration) (int, error) {
	fullKey := fmt.Sprintf("ratelimit:%s", key)
	now := time.Now().UnixMilli()
	clearBefore := now - window.Milliseconds()
	c.c.ZRemRangeByScore(c.ctx, fullKey, "0", fmt.Sprintf("%d", clearBefore))
	count, err := c.c.ZCard(c.ctx, fullKey).Result()
	if err != nil {
		return 0, err
	}
	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

// RateLimitReset 重置限流计数
func (c *Client) RateLimitReset(key string) error {
	return c.c.Del(c.ctx, fmt.Sprintf("ratelimit:%s", key)).Err()
}

// FixedWindowAllow 固定窗口计数器限流（INCR + EXPIRE）
//   - key: 限流标识
//   - limit: 窗口内最大允许请求数
//   - window: 窗口大小
func (c *Client) FixedWindowAllow(key string, limit int64, window time.Duration) (bool, error) {
	fullKey := fmt.Sprintf("ratelimit:fw:%s", key)
	count, err := c.c.Incr(c.ctx, fullKey).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		c.c.Expire(c.ctx, fullKey, window)
	}
	return count <= limit, nil
}
