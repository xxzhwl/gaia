// Package redis 分布式锁（基于 Redis SET NX + Lua 原子释放）
// @author wanlizhan
// @created 2024/7/4
// @updated 2026-04-24  Lua 原子释放 + TryLock + LockCtx
// @updated 2026-05-28  移除包级全局 lockValues，改用 Client 实例隔离的 sync.Map
package redis

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// Lua 脚本：原子性续期（只续自己持有的锁，避免误续已被他人抢占的 key）
// 返回 1=续期成功，0=锁不存在或不属于自己（此时调用方应主动放弃临界区）
const renewScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
    return 0
end
`

// lockMap 返回当前 Client 实例独立的 key->lockValue 映射。
// 不同 *Client 实例之间互不影响，避免全局 map 竞争与误解锁。
// （NewClient 已初始化 lockVals；此处仅做兼容性兜底，避免外部直接 new(Client) 时 nil panic）
func (c *Client) lockMap() *sync.Map {
	if c.lockVals == nil {
		c.lockVals = &sync.Map{}
	}
	return c.lockVals
}

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
		c.lockMap().Store(key, value)
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
				c.lockMap().Store(key, value)
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
	c.lockMap().Delete(key)
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
		c.lockMap().Store(key, lockVal)
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
				c.lockMap().Store(key, lockVal)
				return lockVal, nil
			}
		}
	}
}

// UnLockByKey 根据 key 自动查找之前加锁的 value 并释放（仅在同一 *Client 实例内有效）
func (c *Client) UnLockByKey(key string) error {
	v, ok := c.lockMap().Load(key)
	if !ok {
		return fmt.Errorf("未持有锁: %s", key)
	}
	val, _ := v.(string)
	return c.UnLock(key, val)
}

// ================================ 自动续期锁 ================================
//
// 背景：Lock/LockWithVal 的 TTL 固定，业务执行超过 TTL 后锁会被 Redis 自动释放，
// 此时另一个实例可能拿到锁，导致同一临界区被并发执行。
// LockWithAutoRenew 在后台启动一个 watchdog goroutine 周期性续期（PEXPIRE），
// 直到调用方调用 LockGuard.Close() 主动释放。
//
// 续期策略：每 ttl/3 续一次，每次把 TTL 重置回原值；续期失败（锁已丢失）时
// 通过 OnLost 回调通知业务方，业务方应尽快结束临界区（已不持有锁）。
//
// 注意：watchdog 续期依赖进程存活；进程崩溃后锁仍会在 ttl 后自动释放（安全性由 Redis 保证）。
// 因此 ttl 不能设得过长（建议 10~30s），用续期换取"长任务也能安全持锁"。

// LockGuard 自动续期锁句柄，持有期间后台 goroutine 周期续期；Close 时停止续期并释放锁。
type LockGuard struct {
	client    *Client
	key       string
	value     string
	ttl       time.Duration
	stopCh    chan struct{}
	doneCh    chan struct{}
	onLost    func(key string) // 锁意外丢失时的回调，可为 nil
	lost      atomic.Bool      // 锁是否已意外丢失（续期时发现已不属于自己）
	closeOnce sync.Once
}

// LockWithAutoRenew 加锁并启动后台自动续期。
//
// 参数：
//   - key:        业务锁名（内部会加 "lock:" 前缀）
//   - ttl:        锁的单次有效期（建议 10~30s）；续期 goroutine 每 ttl/3 续一次
//   - maxWait:    抢锁最大等待时长（与 LockWithVal 语义一致）
//   - onLost:     锁意外丢失回调（如被显式 DEL、或续期时发现已不属于自己）。
//                 回调内不应做长耗时操作。可为 nil。
//
// 用法：
//
//	guard, err := cli.LockWithAutoRenew("order_123", 20*time.Second, 5*time.Second, func(k string){
//	    gaia.WarnF("锁丢失: %s", k)
//	})
//	if err != nil { return err }
//	defer guard.Close()
//	// ... 临界区 ...
func (c *Client) LockWithAutoRenew(key string, ttl, maxWait time.Duration, onLost func(key string)) (*LockGuard, error) {
	if ttl <= 0 {
		return nil, fmt.Errorf("ttl 必须大于 0")
	}
	value := gaia.GetUUID()
	if err := c.LockWithVal(key, value, ttl, maxWait); err != nil {
		return nil, err
	}
	g := &LockGuard{
		client: c,
		key:    key,
		value:  value,
		ttl:    ttl,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		onLost: onLost,
	}
	go g.renewLoop()
	return g, nil
}

// renewLoop 后台续期循环。每 ttl/3 续一次。
func (g *LockGuard) renewLoop() {
	defer close(g.doneCh)
	// 续期间隔：ttl/3，最小 1s；保证 ttl 内至少续 2 次，容忍一次续期失败。
	interval := g.ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lockKey := "lock:" + g.key
	ttlMs := int64(g.ttl / time.Millisecond)
	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			// 用 Lua 原子续期：只续"自己持有"的锁
			res, err := g.client.c.Eval(g.client.ctx, renewScript, []string{lockKey}, g.value, ttlMs).Int()
			if err != nil {
				// 网络抖动等错误不立即判定丢失，等下一轮重试
				continue
			}
			if res == 0 {
				// 锁已不属于自己（被他人抢占或已过期），通知业务方
				if g.lost.CompareAndSwap(false, true) {
					if g.onLost != nil {
						func() {
							defer func() { recover() }() // 回调 panic 不能拖垮续期 goroutine
							g.onLost(g.key)
						}()
					}
				}
				return
			}
		}
	}
}

// Close 停止续期并释放锁。可多次调用（幂等）。
// 释放失败（如锁已过期）会返回错误，但续期 goroutine 一定会停止。
func (g *LockGuard) Close() error {
	var err error
	g.closeOnce.Do(func() {
		close(g.stopCh)
		<-g.doneCh // 等续期 goroutine 退出，避免它与 UnLock 竞争
		err = g.client.UnLock(g.key, g.value)
	})
	return err
}

// IsLost 查询锁是否已意外丢失（续期时发现锁不属于自己）。
// 业务可在长任务执行过程中轮询此标志，及时放弃已不持有的临界区。
func (g *LockGuard) IsLost() bool {
	return g.lost.Load()
}