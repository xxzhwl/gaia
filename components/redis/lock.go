// Package redis 分布式锁（基于 Redis SET NX + Lua 原子释放）
// @author wanlizhan
// @created 2024/7/4
// @updated 2026-04-24  Lua 原子释放 + TryLock + LockCtx
package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
)

// Lua 脚本：原子性释放锁（只删自己加的锁）
const unlockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`

// lockStore 保存 key -> lockValue 的映射（用于安全释放）
var (
	lockValues   = make(map[string]string)
	lockValuesMu sync.Mutex
)

// LockWithVal 加锁（带自旋重试）
func (c *Client) LockWithVal(key, value string, ttl, maxWaitTime time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitTime)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	lockKey := "lock:" + key

	// 先尝试一次
	has, err := c.SetNx(lockKey, value, ttl)
	if err != nil {
		return fmt.Errorf("加锁失败：%s", err.Error())
	}
	if has {
		lockValuesMu.Lock()
		lockValues[key] = value
		lockValuesMu.Unlock()
		return nil
	}

	// 轮询重试直到超时
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("加锁超时")
		case <-ticker.C:
			has, err := c.SetNx(lockKey, value, ttl)
			if err != nil {
				return fmt.Errorf("加锁失败：%s", err.Error())
			}
			if has {
				lockValuesMu.Lock()
				lockValues[key] = value
				lockValuesMu.Unlock()
				return nil
			}
		}
	}
}

// UnLock 解锁（Lua 原子操作：只删自己加的锁，防止误删）
func (c *Client) UnLock(key, value string) error {
	lockKey := "lock:" + key
	result, err := c.c.Eval(c.ctx, unlockScript, []string{lockKey}, value).Int()
	if err != nil {
		return fmt.Errorf("解锁失败: %w", err)
	}
	lockValuesMu.Lock()
	delete(lockValues, key)
	lockValuesMu.Unlock()
	if result == 0 {
		return fmt.Errorf("锁已被其他持有者释放或已过期")
	}
	return nil
}

// Lock 直接加锁，自动生成随机 value
func (c *Client) Lock(key string, ttl, maxWaitTime time.Duration) (string, error) {
	uuidS := gaia.GetUUID()
	return uuidS, c.LockWithVal(key, uuidS, ttl, maxWaitTime)
}

// TryLock 尝试加锁（非阻塞），成功返回 lockValue + true
func (c *Client) TryLock(key string, ttl time.Duration) (string, bool, error) {
	lockKey := "lock:" + key
	lockVal := gaia.GetUUID()

	ok, err := c.SetNx(lockKey, lockVal, ttl)
	if err != nil {
		return "", false, err
	}
	if ok {
		lockValuesMu.Lock()
		lockValues[key] = lockVal
		lockValuesMu.Unlock()
		return lockVal, true, nil
	}
	return "", false, nil
}

// LockCtx 带 context 的加锁（ctx 取消即放弃等待）
func (c *Client) LockCtx(ctx context.Context, key string, ttl time.Duration) (string, error) {
	lockKey := "lock:" + key
	lockVal := gaia.GetUUID()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			ok, err := c.SetNx(lockKey, lockVal, ttl)
			if err != nil {
				return "", fmt.Errorf("加锁失败: %w", err)
			}
			if ok {
				lockValuesMu.Lock()
				lockValues[key] = lockVal
				lockValuesMu.Unlock()
				return lockVal, nil
			}
		}
	}
}

// UnLockByKey 根据 key 自动查找之前加锁的 value 并释放（简化调用）
func (c *Client) UnLockByKey(key string) error {
	lockValuesMu.Lock()
	val, ok := lockValues[key]
	lockValuesMu.Unlock()
	if !ok {
		return fmt.Errorf("未持有锁: %s", key)
	}
	return c.UnLock(key, val)
}
