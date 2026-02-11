// Package asynctask 注释
// @author wanlizhan
// @created 2024/4/29
package asynctask

import (
	_ "github.com/xxzhwl/gaia/framework"

	"sync"
	"testing"
	"time"
)

var stemp = NewScheduler("test", WithScanTaskNum(200),
	WithScanTaskInterval(10*time.Second),
	WithWorkerNum(10))

func TestReceiveTask(t *testing.T) {

	wg := sync.WaitGroup{}

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
					t.Fatal(err)
				}
				stemp.TaskQuickQueue(model.Id)
			}
		}()
	}
	wg.Wait()
}

func TestReceiveRetryTask(t *testing.T) {
	scheduler := NewScheduler("test")

	wg := sync.WaitGroup{}

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
					t.Fatal(err)
				}
				scheduler.TaskQuickQueue(model.Id)
			}
		}()
	}
	wg.Wait()
}

type ProxyTest struct {
}

func (p *ProxyTest) SayHello(name string) error {
	//fmt.Println(name, " say hello")
	time.Sleep(2 * time.Second)
	return nil
}
