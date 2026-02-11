// Package g 包注释
// @author wanlizhan
// @created 2024/5/10
package g

import (
	"errors"
	"github.com/xxzhwl/gaia"
	"sync"
	"sync/atomic"
	"time"
)

var gNum int64 = 0

func incrGNum() {
	atomic.AddInt64(&gNum, 1)
}

func decrGNum() {
	atomic.AddInt64(&gNum, 1)
}

func loadGNum() int64 {
	return atomic.LoadInt64(&gNum)
}

func balanceGNum() bool {
	return loadGNum() == 0
}

func Go(handler func()) {
	_go(nil, handler)
}

func GoWithParam[T any](handler func(param T), param T) {
	_goWithParameter(nil, handler, param)
}

func WgGo(wg *sync.WaitGroup, handler func()) {
	_go(wg, handler)
}

func WgGoWithParam[T any](wg *sync.WaitGroup, handler func(param T), param T) {
	_goWithParameter(wg, handler, param)
}

func _go(wg *sync.WaitGroup, handler func()) {
	incrGNum()
	if wg != nil {
		wg.Add(1)
	}
	go func(wg *sync.WaitGroup, handler func(), ctx *gaia.TraceData) {
		defer func() {
			if wg != nil {
				wg.Done()
			}
		}()
		defer decrGNum()
		defer gaia.CatchPanic()
		defer gaia.RemoveContextTrace()

		gaia.ResetContextTraceByData(ctx)
		handler()
	}(wg, handler, gaia.GetContextTrace())
}

func _goWithParameter[T any](wg *sync.WaitGroup, fn func(T), param T) {
	incrGNum()
	if wg != nil {
		wg.Add(1)
	}
	go func(fn func(T), param T, wg *sync.WaitGroup, ctx *gaia.TraceData) {
		defer func() {
			if wg != nil {
				wg.Done()
			}
		}()
		defer decrGNum()
		defer gaia.CatchPanic()
		defer gaia.RemoveContextTrace()

		gaia.ResetContextTraceByData(ctx)
		fn(param)
	}(fn, param, wg, gaia.GetContextTrace())
}

func WaitG(timeout time.Duration) error {
	now := time.Now()
	for time.Now().Sub(now) < timeout {
		if balanceGNum() {
			return nil
		}
	}
	return errors.New("等待GNum归零超时")
}
