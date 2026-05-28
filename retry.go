// Package gaia
// @author: wanlizhan
// @created: 2024-12-05
package gaia

import (
	"context"
	"fmt"
	"time"
)

// Retry 通用重试逻辑
// 1. f func() error 只是一个函数执行体，具体被重试函数的返回值可以通过外部变量来接收(也包括error返回值)
// 2. 如果需要跳出重试，在 f func() error 的实现中直接返回nil
func Retry(f func() error, nTimes int, interval time.Duration) error {
	c := 0
	for {
		if err := f(); err != nil {
			c++
			if c > nTimes {
				return fmt.Errorf("retry exceeded, total %d times, last err: %s", nTimes, err.Error())
			}
			NewLogger("gaiaRetry").WarnF("ERROR: %s, will retry after %s", err.Error(),
				interval.String())
		} else {
			return nil
		}
		time.Sleep(interval)
	}
}

// RunInterval 严格以一定间隔重复运行
// ctx 用于控制优雅退出，如无此需求，可直接传入 context.Background();
// f func() error 只是一个通用的函数执行原型，如果待执行的函数有十分复杂的签名，只需要将其封装在闭包中传入即可;
// runInstantly 设置为 true 时，会立即执行一次，然后按照指定间隔运行；否则会等到第一次间隔满足才开始执行;
func RunInterval(ctx context.Context, f func() error, interval time.Duration, runInstantly bool) (err error) {
	// 立即运行一次
	if runInstantly {
		if err = f(); err != nil {
			return
		}
	}

	// 循环按照间隔运行
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			err = f()
			if err != nil {
				return
			}
		}
	}
}
