// Package asynctask 注释
// @author wanlizhan
// @created 2024/4/29
package asynctask

import (
	"github.com/xxzhwl/gaia"
	_ "github.com/xxzhwl/gaia/framework"

	"sync"
	"testing"
	"time"
)

var stemp = NewScheduler("test", WithScanTaskNum(200),
	WithScanTaskInterval(10*time.Second),
	WithWorkerNum(10))

func TestReceiveTask(t *testing.T) {
	if gaia.GetSafeConfString("Framework.Mysql") == "" {
		t.Skip("skip integration test: Framework.Mysql dsn is empty")
	}

	wg := sync.WaitGroup{}
	errCh := make(chan error, 4)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < 2; i++ {
				model, err := stemp.ReceiveTask(TaskBaseInfo{
					ServiceName: "ProxyTest",
					MethodName:  "SayHello",
					TaskName:    "sayHello",
					Arg:         "wanli",
				})
				if err != nil {
					errCh <- err
					return
				}
				stemp.TaskQuickQueue(model.Id)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestReceiveRetryTask(t *testing.T) {
	if gaia.GetSafeConfString("Framework.Mysql") == "" {
		t.Skip("skip integration test: Framework.Mysql dsn is empty")
	}

	scheduler := NewScheduler("test")

	wg := sync.WaitGroup{}
	errCh := make(chan error, 4000)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for i := 0; i < 200; i++ {
				model, err := scheduler.ReceiveTask(TaskBaseInfo{
					ServiceName:  "ProxyTest",
					MethodName:   "SayHello",
					TaskName:     "sayHello",
					Arg:          "wanli",
					MaxRetryTime: 5,
				})
				if err != nil {
					errCh <- err
					return
				}
				scheduler.TaskQuickQueue(model.Id)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

type ProxyTest struct {
}

func (p *ProxyTest) SayHello(name string) error {
	//fmt.Println(name, " say hello")
	time.Sleep(2 * time.Second)
	return nil
}
