// Package buffer 包注释
// @author wanlizhan
// @created 2024/6/25
package buffer

import (
	"os"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/g"
)

var DefaultLoggerPath = "var" + gaia.Sep + "logs" + gaia.Sep

// 用于保护 GetDataBuffer 中检查和注册的原子性
var getBufferLock sync.Mutex

type DataBuffer struct {
	buffer       chan []byte //存放数据的缓冲区
	bufferLength int         //缓冲区大小

	logger       gaia.IBaseLog //缓冲队列日志
	handler      Handler       //对存放数据的处理方法
	minWorkerNum int           //最小处理单元
	maxWorkerNum int           //最大处理单元

	batchPopDataNum     int           //批量处理[]byte的最大个数，到这个数字全部拿出来
	batchPopDataTimeOut time.Duration //批量取数据的超时时间
	batchScanInterval   time.Duration //批量取数据的时间间隔

	sleepLine time.Duration //存在sleepLine时间片内无运行的话，就要休眠

	onceRun sync.Once

	disableDiskStore bool   //关闭磁盘存储，默认会打开
	filePath         string //若处理失败，则会将这批数据写入文件，等待后续处理

	workers map[string]*worker
	l       sync.RWMutex
}

type worker struct {
	workerId        string
	processDataSize int64
	lastRunTime     time.Time
	sleepChan       chan bool
}

func GetDataBuffer(title string, handler Handler, options ...Option) (*DataBuffer, error) {
	if len(title) == 0 {
		title = "default"
	}
	title = "DataBuffer-" + title

	// 先尝试获取已存在的实例（快速路径，无需加锁）
	instance := getInstance(title)
	if instance != nil {
		//对于已经存在的instance，直接返回
		return instance, nil
	}

	// 加锁保护检查和创建的原子性，防止并发创建同一缓冲区
	getBufferLock.Lock()
	defer getBufferLock.Unlock()

	// 双重检查：加锁后再次确认实例是否已被其他协程创建
	instance = getInstance(title)
	if instance != nil {
		return instance, nil
	}

	//不存在则要新建instance
	filePath := ""
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	} else {
		filePath = dir + gaia.Sep + DefaultLoggerPath + gaia.Sep + title
	}

	d := &DataBuffer{
		logger:   gaia.TempLog{},
		filePath: filePath,
		handler:  handler,
	}
	if gaia.NewLogger != nil {
		d.logger = gaia.NewLogger(title)
	}

	for _, option := range options {
		option(d)
	}

	if d.bufferLength <= 0 {
		d.bufferLength = DefaultBufferSize
	}
	d.buffer = make(chan []byte, d.bufferLength)

	if d.minWorkerNum > d.maxWorkerNum {
		d.minWorkerNum = DefaultMinWorkerNum
		d.maxWorkerNum = DefaultMaxWorkerNum
	}
	if d.minWorkerNum < MinWorkerNumFloor || d.minWorkerNum > MinWorkerNumTop {
		d.minWorkerNum = DefaultMinWorkerNum
	}
	if d.maxWorkerNum < MaxWorkerNumFloor || d.maxWorkerNum > MaxWorkerNumTop {
		d.maxWorkerNum = DefaultMaxWorkerNum
	}

	if d.batchPopDataNum <= 0 {
		d.batchPopDataNum = DefaultBatchPopDataNum
	}

	if d.batchPopDataTimeOut <= 0 {
		d.batchPopDataTimeOut = DefaultBatchPopDataTimeOut
	}

	if d.sleepLine <= 0 {
		d.sleepLine = DefaultSleepLine
	}

	if d.batchScanInterval <= 0 {
		d.batchScanInterval = DefaultBatchScanInterval
	}

	if err = registerInstance(title, d); err != nil {
		return nil, err
	}
	d.workers = make(map[string]*worker)
	return d, nil
}

func (d *DataBuffer) Push(data []byte) {
	if len(data) == 0 {
		return
	}

	d.buffer <- data

	d.onceRun.Do(d.process)
}

// 协程永远不停的处理任务
func (d *DataBuffer) process() {
	g.Go(func() {
		for {
			d.controlWorker()
			time.Sleep(1 * time.Second)
		}
	})
}

func (d *DataBuffer) controlWorker() {
	d.l.Lock()
	defer d.l.Unlock()
	if len(d.workers) == 0 && len(d.buffer) > 0 {
		//最开始要初始化
		d.logger.Debug("有数据无worker，需要初始化worker")
		for i := 0; i < d.minWorkerNum; i++ {
			d.work()
		}
	} else {
		//对于这种情况要扩容
		if len(d.workers) < d.maxWorkerNum && ((float64(len(d.buffer)/d.bufferLength) > 0.5) ||
			(len(d.workers) > 0 && (len(d.buffer)/len(d.workers)*d.batchPopDataNum > 100))) {
			d.logger.Debug("扩容worker")
			d.work()
		}
	}
}

func (d *DataBuffer) work() {
	go func() {
		defer gaia.CatchPanic()
		defer gaia.RemoveContextTrace()
		gaia.BuildContextTrace()
		goId := gaia.GetGoRoutineId()
		d.l.Lock()
		if _, ok := d.workers[goId]; !ok {
			d.workers[goId] = &worker{
				workerId:        goId,
				processDataSize: 0,
				lastRunTime:     time.Now(),
				sleepChan:       make(chan bool, 1),
			}
		} //如果不存在就要新增一个，然后开始运行
		w := d.workers[goId]
		d.l.Unlock()
		for {
			select {
			case <-time.After(d.batchScanInterval):
				dataList := d.popData()
				num := len(dataList)
				if num == 0 {
					if time.Since(w.lastRunTime) > d.sleepLine {
						gaia.DebugF("%s要休眠了", goId)
						d.l.Lock()
						delete(d.workers, goId)
						d.l.Unlock()
						return
					}
				} else {
					gaia.DebugF("%s开始干活儿", goId)
					if err := d.handler(dataList); err != nil {
						d.logger.Error(err.Error())
					}
					w.lastRunTime = time.Now()
					w.processDataSize += int64(num)
				}
			}
		}
	}()
}

// 批量取出数据执行
// 不过考虑到数据低频写入时，超过某个时间限度，强制取出数据处理
func (d *DataBuffer) popData() [][]byte {
	ret := make([][]byte, 0)
	for i := 0; i < d.batchPopDataNum; i++ {
		select {
		case data := <-d.buffer:
			ret = append(ret, data)
		case <-time.After(d.batchPopDataTimeOut):
			return ret
		}
	}
	return ret
}
