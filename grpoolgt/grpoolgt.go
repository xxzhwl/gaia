/*
Package grpoolgt 实现一个协程处理池
对于大规模(如大于1000)的并行任务处理，如果启动过多的goroutine，极容易对外部服务造成冲击，引发雪崩效应，同时也很容器造成本地端口耗尽等问题
通过使用grpool实现，控制对同一类任务的处理只启动特定数量的协程，抢占式处理，达到效率最大化，并避免对周边服务造成冲击
@author wanlizhan
@create 2023-11-18
@modified 2023-09-29
*/
package grpoolgt

import (
	"errors"
	"fmt"
	"github.com/xxzhwl/gaia"
	"strconv"
	"sync"
)

// RawTask 用于单个任务(一个goroutine完成的处理)处理的原数据
// 这是一个泛型结构体，需要实例化具体的类型
type RawTask[I any] struct {
	Id        string //一般来说，该Id和RetTask.Id是相对应的，如果单任务的输入与输出数据之间不需要对应关系，Id可以不指定
	InputData I      //任务的输入数据
}

// RetTask 用于返回单个任务(一个goroutine完成的处理)的处理结果
// 这是一个泛型结构体，需要实例化具体的类型
type RetTask[O any] struct {
	Id         string //与RawTask.Id对应，标识输入与输出数据之前的关联关系
	OutputData O      //任务的输出数据
	Err        error  //处理过程中的异常信息
}

// DoFunc 单个任务执行的方法
// 这是一个泛型结构体，需要实例化类型
type DoFunc[I, O any] func(task RawTask[I]) (outData O, err error)

// FlushFunc 处理单个任务结果的方法
// 这是一个泛型结构体，需要实例化类型
type FlushFunc[O any] func(data RetTask[O])

// Do 大规模任务执行入口
// concurrency 指定启动多少个goroutine处理任务
// rawTaskList []RawTask 待处理的任务列表。
// fun 所定义的处理单个任务的逻辑，要求返回RetTask类型的Id, OutputData, Err字段部分
// 最终，该函数返回一个任务处理完成的结果列表，类型为[]RetTask
//
// 这是一个泛型方法，需要实例化类型, 其中 I 类型表示任务输入数据, O 类型表示任务输出数据
func Do[I, O any](concurrency int, rawTaskList []RawTask[I], doTask DoFunc[I, O]) ([]RetTask[O], error) {
	return _do(concurrency, rawTaskList, doTask, nil)
}

// DoAndFlush 大规模执行任务，并提供对于每次执行结果的处理逻辑注入
// 在单次任务为 IO 密集型时，需要及时释放内存资源，避免内存泄漏
// 此时仅需传入 FlushFunc 用于处理每次执行的结果，及时释放资源
//
// 这是一个泛型方法，需要实例化类型
func DoAndFlush[I, O any](concurrency int, tasks []RawTask[I], doTask DoFunc[I, O], flush FlushFunc[O]) error {
	_, err := _do(concurrency, tasks, doTask, flush)
	return err
}

// 执行协程池逻辑
// flush FlushFunc 当任务执行完成后，执行结果直接通过flush的逻辑进行处理，如果不指定flush，则所有结果执行完成后，通过[]RetTask统一返回
// return:
// []RetTask 如果不指定 FlushFunc 参数，则返回所有的执行结果，如果指定了 FlushFunc 参数，结果在内部直接处理，不再集中返回
func _do[I, O any](concurrency int, rawTaskList []RawTask[I], doTask DoFunc[I, O], flush FlushFunc[O]) (
	[]RetTask[O], error) {
	//待处理任务长度
	taskLen := len(rawTaskList)
	if taskLen == 0 {
		return nil, errors.New("传入协程处理池的输入数据为空！")
	}

	//将所有的待处理数据统一压到channel中，以便后续多个协程来同时处理
	chInputDataList := make(chan RawTask[I], taskLen)
	for _, task := range rawTaskList {
		chInputDataList <- task
	}

	//将所有goroutine的处理结果压入该channel中，以便统一返回给上层调用
	chResultList := make(chan RetTask[O], taskLen)

	//获取任务数据时要加锁，主要是为了判断当前待处理任务列表是否为空，channel操作本身是原子性的
	var rw sync.RWMutex

	//启动指定数量的处理协程
	for i := 0; i < concurrency; i++ {
		go func(parentGoId string) {
			//注意所有的go执行内必须要调用该函数进行recover panic，
			//否则一旦逻辑内出现panic，将导致整个进行退出
			defer gaia.CatchPanic()

			//继承父协程的ContextTrace
			gaia.ResetContextTrace(parentGoId)
			defer gaia.RemoveContextTrace()

			//同一个goroutine内，需要循环执行，即对所有已有的任务抢占式执行，
			//直到再也没有可执行的任务为此
			for {
				rw.Lock()
				if len(chInputDataList) == 0 {
					//chan中已经没有可以再处理的数据，解锁后跳出
					rw.Unlock()
					break
				}
				//获取到可以处理的数据
				taskData := <-chInputDataList
				rw.Unlock()

				//执行过程中，单独catch panic
				func(taskData RawTask[I]) {
					defer func() {
						if r := recover(); r != nil {
							gaia.PanicLog(r)

							//出现了运行时异常
							chResultList <- RetTask[O]{Id: taskData.Id, Err: fmt.Errorf("%v", r)}
						}
					}()

					outputData, err := doTask(taskData)
					chResultList <- RetTask[O]{Id: taskData.Id, Err: err, OutputData: outputData}
				}(taskData)

			}
		}(gaia.GetGoRoutineId())
	}

	//定义一个返回结果集，统一是RetTask类型
	retval := make([]RetTask[O], 0)

	//所有逻辑执行完成，整理结果并返回
	for i := 0; i < taskLen; i++ {
		if flush != nil {
			flush(<-chResultList)
		} else {
			retval = append(retval, <-chResultList)
		}
	}

	return retval, nil
}

//
//以下是协程池附属逻辑封装
//提高协程池的易用性，降低业务层调用复杂度
//

// IntToId 将一个Int类型的值，转换成协程池中执行的任务ID(string类型)
func IntToId(id int) string {
	return strconv.Itoa(id)
}

// GetRetDataErrs 协程池执行完成后，会返回 []RetTask 列表，从此列表中提取出所有相关的错误信息
// 此逻辑比如适合检查协程池的执行结果是否存在报错的情况
// 如果不存在错误，则返回为空
// 这是一个泛型方法，需要实例化类型
func GetRetDataErrs[O any](results []RetTask[O]) (errlist []string) {
	if len(results) == 0 {
		return nil
	}
	for _, itm := range results {
		if itm.Err != nil {
			errlist = append(errlist, itm.Err.Error())
		}
	}
	return errlist
}
