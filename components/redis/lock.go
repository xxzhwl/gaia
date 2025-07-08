// Package redis 包注释
// @author wanlizhan
// @created 2024/7/4
package redis

import (
	"fmt"
	"github.com/xxzhwl/gaia"
	"time"
)

// LockWithVal 加锁
func (c *Client) LockWithVal(key, value string, ttl, maxWaitTime time.Duration) error {
	waitChan := make(chan struct{}, 1)
	var err error
	lastTime := time.Now().Add(maxWaitTime)
	go func() {
		defer gaia.CatchPanic()

		for {
			//1.阻塞式锁，加完以后要等待
			has, err := c.SetNx(key, value, ttl)
			if err != nil {
				//失败退出
				err = fmt.Errorf("加锁失败：%s", err.Error())
				waitChan <- struct{}{}
				return
			}

			if has {
				//加锁成功开始计时
				waitChan <- struct{}{}
				return
			}

			//获取锁失败，重复准备加锁直到超时
			if time.Now().After(lastTime) {
				return
			}

			time.Sleep(20 * time.Millisecond)
		}
	}()
	select {
	case <-waitChan:
		if err != nil {
			return err
		}
		return nil
	case <-time.After(maxWaitTime):
		return fmt.Errorf("加锁超时")
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
