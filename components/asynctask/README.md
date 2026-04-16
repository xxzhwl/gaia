# AsyncTask — 异步任务调度模块

基于 MySQL 的分布式异步任务调度框架，支持任务接收、调度、执行、重试、心跳检测和管理后台接口。

## 核心特性

- **基于 DB 的任务持久化**：任务存储在 MySQL 中，进程重启不丢失
- **自动扩缩容**：Worker 根据队列负载自动扩容/缩容
- **心跳检测**：检测执行中断的任务，自动重置为等待状态
- **重试机制**：支持配置最大重试次数，失败后自动进入重试队列
- **前/后置处理器**：支持注入任务执行前后的自定义逻辑
- **管理后台 API**：提供任务列表、详情、重试、取消等管理接口
- **OpenTelemetry 链路追踪**：内置 Tracer 支持

## 快速开始

### 1. 初始化数据库

执行 `db.sql` 中的建表语句，创建以下三张表：
- `asynctasks` — 异步任务主表
- `async_task_heartbeat` — 任务心跳表
- `asynctask_exec_row` — 任务执行记录表

### 2. 配置数据库连接

在项目配置文件（如 `configs/local/config.json`）中添加：

```json
{
  "AsyncTask": {
    "Mysql": {
      "Host": "127.0.0.1",
      "Port": 3306,
      "User": "root",
      "Pass": "password",
      "Db": "your_database"
    }
  }
}
```

### 3. 注册业务 Service

异步任务通过反射调用已注册的 Service 方法。需要先注册 Proxy：

```go
// 定义业务 Service
type OrderService struct{}

func (s *OrderService) ProcessOrder(arg string) error {
    // 业务逻辑
    return nil
}

// 注册到 gaia proxy（在服务初始化时）
gaia.RegisterProxy("MySystem", "OrderService", &OrderService{})
```

### 4. 创建调度器并启动

```go
import (
    "context"
    "time"
    "github.com/xxzhwl/gaia/components/asynctask"
)

// 方式一：一键启动（推荐，非阻塞）
scheduler := asynctask.StartScheduler(context.Background(), "MySystem",
    asynctask.WithWorkerNum(30),
    asynctask.WithScanTaskInterval(5*time.Second),
    asynctask.WithScanTaskNum(50),
    asynctask.WithTaskIdChanLength(200),
)

// 方式二：手动创建 + 启动
scheduler := asynctask.NewScheduler("MySystem", asynctask.WithWorkerNum(30))
go scheduler.Start(context.Background())
```

### 5. 提交任务

```go
// 方式一：快捷提交（推荐，自动快速入队）
model, err := asynctask.SubmitTask("MySystem", asynctask.TaskBaseInfo{
    ServiceName:  "OrderService",
    MethodName:   "ProcessOrder",
    TaskName:     "处理订单#12345",
    Arg:          `{"orderId": 12345}`,
    MaxRetryTime: 3,
})

// 方式二：一行提交带重试
model, err := asynctask.SubmitTaskWithRetry("MySystem", "OrderService", "ProcessOrder", "处理订单", `{"id":1}`, 3)

// 方式三：通过 Scheduler 实例提交
model, err := scheduler.ReceiveTask(asynctask.TaskBaseInfo{...})
scheduler.TaskQuickQueue(model.Id) // 可选：快速入队
```

## 配置选项

| Option | 说明 | 默认值 |
|--------|------|--------|
| `WithWorkerNum(n)` | 最大工作协程数 | 30 |
| `WithScanTaskInterval(d)` | 扫描 DB 间隔 | 5s |
| `WithScanTaskNum(n)` | 每次扫描任务数 | 50 |
| `WithTaskIdChanLength(n)` | 任务队列缓冲长度 | 100 |
| `WithHearBeatInterval(d)` | 心跳间隔 | 5s |
| `WithPreHandler(fn)` | 前置处理器 | nil |
| `WithPostHandler(fn)` | 后置处理器 | nil |
| `WithAlarmFunc(fn)` | 告警处理器 | nil |

## 任务状态流转

```
  ┌──────────────────────────────────────┐
  │                                      │
  ▼                                      │
Wait ──► Running ──► Success             │
  ▲         │                            │
  │         ├──► Failed                  │
  │         │                            │
  │         └──► Retry ─────────────────┘
  │                │
  └────────────────┘ (心跳超时重置)
```

- **Wait**：等待调度
- **Running**：执行中
- **Success**：执行成功
- **Failed**：执行失败（超过最大重试次数或不可重试的错误）
- **Retry**：需要重试（未超过最大重试次数）

## 便捷查询 API

```go
// 查询任务状态
status, err := asynctask.GetTaskStatus(12345)
// 返回: "Wait" / "Running" / "Success" / "Failed" / "Retry"

// 判断任务是否完成（成功或失败都算完成）
done, err := asynctask.IsTaskDone(12345)

// 判断任务是否成功
ok, err := asynctask.IsTaskSuccess(12345)

// 阻塞等待任务完成（支持超时），适用于异步转同步场景
task, err := asynctask.WaitTaskDone(12345, 30*time.Second)
if err != nil {
    // 超时
}
fmt.Println(task.TaskStatus, task.LastResult)

// 按状态统计任务数量（适用于监控大屏）
counts, err := asynctask.GetTaskCountByStatus("MySystem")
fmt.Printf("等待:%d, 运行:%d, 成功:%d, 失败:%d, 总计:%d\n",
    counts.Wait, counts.Running, counts.Success, counts.Failed, counts.Total)

// 清理 7 天前的已完成任务（建议定时调用，防止表无限增长）
cleaned, err := asynctask.CleanFinishedTasks("MySystem", 7*24*time.Hour)
fmt.Printf("清理了 %d 条历史任务\n", cleaned)
```

## 管理后台 API

```go
// 查询任务列表
result, err := asynctask.ListTasks(asynctask.ListTasksArgs{
    Page: 1, PageSize: 20,
    TaskStatus: []string{"Wait", "Running"},
    TaskName:   "订单",
}, "MySystem", ctx)

// 查询任务详情
task, err := asynctask.GetTaskDetail(taskId, ctx)

// 查询执行记录
records, total, err := asynctask.GetTaskExecRecords(taskId, 1, 20, ctx)

// 手动重试
err := asynctask.RetryTask(taskId, ctx)

// 取消任务
err := asynctask.CancelTask(taskId, ctx)

// 获取调度器状态
statuses := asynctask.GetAllSchedulerStatus()

// 停止调度器
asynctask.StopScheduler("MySystem")
```

## 架构说明

```
Scheduler (调度器)
├── scanTasks()     — 定时扫描 DB，将待执行任务 ID 入队
├── wakeUpWorker()  — 动态启动 Worker 协程
├── monit()         — 监控队列状态，触发告警
└── heartBeat()     — 心跳检测，重置断连任务

Worker (工作协程)
├── 从 taskIdChan 获取任务 ID
├── tryLockTask()   — 乐观锁抢占任务
└── Executor.Run()  — 反射调用业务方法

Executor (执行器)
├── preRun()        — 前置处理（带 panic recover）
├── run()           — 反射调用业务 Service 方法 + 心跳维持
└── postRun()       — 后置处理（带 panic recover）
```

## 注意事项

1. **Service 方法签名**：被调用的方法需接收 `string` 类型参数（JSON），返回 `error`
2. **任务幂等性**：由于有重试机制，业务方法应保证幂等性
3. **心跳超时**：默认 30 秒无心跳视为断连，任务会被重置为 Wait 状态
4. **Worker 缩容**：空闲超过 10 秒且空闲 Worker 数大于队列任务数时触发，最低保持 10 个 Worker
