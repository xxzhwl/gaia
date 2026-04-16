// Package redis 包注释
// @author wanlizhan
// @created 2024/7/4
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
)

// LockWithVal 加锁
func (c *Client) LockWithVal(key, value string, ttl, maxWaitTime time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), maxWaitTime)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	// 先尝试一次
	has, err := c.SetNx(key, value, ttl)
	if err != nil {
		return fmt.Errorf("加锁失败：%s", err.Error())
	}
	if has {
		return nil
	}

	// 轮询重试直到超时
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("加锁超时")
		case <-ticker.C:
			has, err := c.SetNx(key, value, ttl)
			if err != nil {
				return fmt.Errorf("加锁失败：%s", err.Error())
			}
			if has {
				return nil
			}
		}
	}
}

// UnLock 解锁的话要拿到这个key，获取到value以后与之前设置锁的value比较，如果一样就要删除释放
func (c *Client) UnLock(key, value string) error {
	getString, err := c.GetString(key)
	if err != nil {
		return err
	}

	//比较旧值
	if getString != value {
		return fmt.Errorf("当前获取锁的值[%s]和给定值不同[%s]", getString, value)
	}

	//释放
	return c.Del(key)
}

// Lock 直接加锁，这里value我们给一个随机值
func (c *Client) Lock(key string, ttl, maxWaitTime time.Duration) (string, error) {
	uuidS := gaia.GetUUID()
	return uuidS, c.LockWithVal(key, uuidS, ttl, maxWaitTime)
}
