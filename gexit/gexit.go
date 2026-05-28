// Package gexit 包注释
// @author wanlizhan
// @created 2024-08-14
package gexit

import (
	"context"
	"github.com/xxzhwl/gaia"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

var exitContext context.Context

var exitCancel context.CancelFunc

var exitCode int64

var exitLocker sync.Mutex

func GetExitContext() context.Context {
	exitLocker.Lock()
	defer exitLocker.Unlock()

	if exitContext != nil {
		return exitContext
	}

	exitContext, exitCancel = context.WithCancel(context.Background())
	go func() {
		//监听退出信号
		listenExit()

		//退出
		exitCancel()

		atomic.StoreInt64(&exitCode, 1)
	}()
	return exitContext
}

func listenExit() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-signalChan
	gaia.InfoF("Got signal [%v] to exit.", sig)
}
