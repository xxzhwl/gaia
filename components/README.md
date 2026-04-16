# Gaia 组件集成指南

本文档介绍如何在基于 gaia-server 脚手架的项目中集成 `asynctask` 和 `jobs` 两个组件。

## 目录

1. [AsyncTask 异步任务组件](#asynctask-异步任务组件)
2. [Jobs 定时任务组件](#jobs-定时任务组件)

---

## AsyncTask 异步任务组件

### 功能概述

AsyncTask 是一个分布式异步任务处理组件，支持：

- 任务持久化到数据库
- 自动重试机制
- 动态扩缩容 Worker
- 任务执行监控和统计
- 前置/后置处理器

### 数据库初始化

执行以下 SQL 创建任务表：

```sql
-- 任务主表
CREATE TABLE `asynctasks` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `service_name` varchar(255) NOT NULL COMMENT '服务名',
  `method_name` varchar(255) NOT NULL COMMENT '方法名',
  `task_name` varchar(255) NOT NULL COMMENT '任务名',
  `arg` text COMMENT '参数JSON',
  `max_retry_time` int(11) DEFAULT '3' COMMENT '最大重试次数',
  `system_name` varchar(100) NOT NULL COMMENT '系统标识',
  `task_status` varchar(50) DEFAULT 'Wait' COMMENT '任务状态:Wait/Running/Success/Failed/Retry',
  `last_run_time` datetime DEFAULT NULL COMMENT '最后运行时间',
  `last_run_end_time` datetime DEFAULT NULL COMMENT '最后运行结束时间',
  `last_run_duration` bigint(20) DEFAULT '0' COMMENT '执行耗时(ms)',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  `retry_time` int(11) DEFAULT '0' COMMENT '已重试次数',
  `last_result` text COMMENT '执行结果',
  `last_err_msg` text COMMENT '错误信息',
  `log_id` varchar(100) DEFAULT NULL COMMENT '日志追踪ID',
  PRIMARY KEY (`id`),
  KEY `idx_system_status` (`system_name`,`task_status`),
  KEY `idx_create_time` (`create_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='异步任务表';

-- 任务执行记录表
CREATE TABLE `asynctask_exec_row` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `task_id` bigint(20) NOT NULL COMMENT '任务ID',
  `task_status` varchar(50) DEFAULT NULL COMMENT '任务状态',
  `last_result` text COMMENT '执行结果',
  `last_err_msg` text COMMENT '错误信息',
  `last_run_time` datetime DEFAULT NULL COMMENT '运行时间',
  `last_run_end_time` datetime DEFAULT NULL COMMENT '结束时间',
  `last_run_duration` bigint(20) DEFAULT '0' COMMENT '执行耗时(ms)',
  `log_id` varchar(100) DEFAULT NULL COMMENT '日志追踪ID',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_task_id` (`task_id`),
  KEY `idx_run_time` (`last_run_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='异步任务执行记录表';
```

### 配置文件

在 `config.json` 中添加：

```json
{
  "AsyncTask": {
    "Mysql": "default"
  }
}
```

### 服务注册

在业务服务中注册需要被异步调用的方法：

```go
package service

import "github.com/xxzhwl/gaia"

type OrderService struct{}

// AsyncCreateOrder 异步创建订单（将被异步任务调用）
func (s *OrderService) AsyncCreateOrder(args []byte) (any, error) {
    // 解析参数
    var req CreateOrderReq
    if err := json.Unmarshal(args, &req); err != nil {
        return nil, err
    }
    
    // 执行业务逻辑
    orderId, err := s.CreateOrder(req)
    
    return map[string]any{"order_id": orderId}, err
}

func init() {
    // 注册服务到 Proxy
    gaia.RegisterProxy("OrderService", &OrderService{})
}
```

### 启动调度器

在 `main.go` 或 `bootstrap` 中启动：

```go
package main

import (
    "context"
    "github.com/xxzhwl/gaia/components/asynctask"
)

func main() {
    // 创建并配置调度器
    scheduler := asynctask.NewScheduler("order-task", // theme，用于区分不同业务
        asynctask.WithWorkerNum(30),                    // Worker 数量
        asynctask.WithScanTaskInterval(5*time.Second),  // 扫描间隔
        asynctask.WithTaskIdChanLength(1000),           // 任务队列长度
        asynctask.WithScanTaskNum(50),                  // 每次扫描任务数
        asynctask.WithPreHandler(func() error {         // 前置处理器（可选）
            // 如：检查数据库连接
            return nil
        }),
        asynctask.WithPostHandler(func(taskId int64) error { // 后置处理器（可选）
            // 如：发送通知
            return nil
        }),
    )
    
    // 启动调度器（阻塞，建议在单独 goroutine 中运行）
    go scheduler.Start(context.Background())
}
```

### 投递任务

在业务代码中投递异步任务：

```go
import (
    "encoding/json"
    "github.com/xxzhwl/gaia/components/asynctask"
)

func (s *OrderService) SubmitOrder(ctx context.Context, req CreateOrderReq) error {
    // 构造任务参数
    args, _ := json.Marshal(req)
    
    // 投递任务
    task := asynctask.TaskBaseInfo{
        ServiceName:  "OrderService",
        MethodName:   "AsyncCreateOrder",
        TaskName:     "创建订单",
        Arg:          string(args),
        MaxRetryTime: 3,
    }
    
    model, err := asynctask.AddTask(task, "order-task", ctx)
    if err != nil {
        return err
    }
    
    // 返回任务ID
    fmt.Printf("任务已投递，ID: %d\n", model.Id)
    return nil
}
```

### 快速入队（无缝处理）

如果需要在投递后立即处理（不等待下次扫描）：

```go
// 投递任务并快速入队
model, err := scheduler.ReceiveTask(task)
if err != nil {
    return err
}

// 尝试快速入队（异步，不阻塞）
scheduler.TaskQuickQueue(model.Id)
```

### 管理接口

组件提供了管理接口供后台使用：

```go
// 获取任务列表
result, err := asynctask.ListTasks(asynctask.ListTasksArgs{
    Page:       1,
    PageSize:   20,
    TaskStatus: []string{"Wait", "Retry"},
    TaskName:   "订单",
}, "order-task", ctx)

// 获取任务详情
detail, err := asynctask.GetTaskDetail(taskId, ctx)

// 获取执行记录
records, total, err := asynctask.GetTaskExecRecords(taskId, 1, 10, ctx)

// 手动重试任务
err = asynctask.RetryTask(taskId, ctx)

// 取消任务
err = asynctask.CancelTask(taskId, ctx)

// 获取所有调度器状态
statusMap := asynctask.GetAllSchedulerStatus()
```

---

## Jobs 定时任务组件

### 功能概述

Jobs 是一个定时任务调度组件，支持：

- Cron 表达式定时调度
- 服务方法调用（CronJob）
- HTTP Hook 调用（CronHook）
- 任务执行超时控制
- 任务执行记录
- 动态增删改任务

### 数据库初始化

执行以下 SQL 创建任务表：

```sql
-- 任务中心表
CREATE TABLE `job_center` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `job_name` varchar(255) NOT NULL COMMENT '任务名称',
  `job_type` varchar(50) NOT NULL COMMENT '任务类型: cron_job/cron_hook',
  `cron_expr` varchar(100) NOT NULL COMMENT 'Cron表达式',
  `hook_url` varchar(500) DEFAULT NULL COMMENT 'Hook URL（cron_hook类型）',
  `service_name` varchar(255) DEFAULT NULL COMMENT '服务名（cron_job类型）',
  `service_method` varchar(255) DEFAULT NULL COMMENT '方法名（cron_job类型）',
  `args` text COMMENT '参数JSON',
  `timeout` int(11) DEFAULT '300' COMMENT '超时时间（秒）',
  `enabled` tinyint(1) DEFAULT '1' COMMENT '是否启用',
  `run_status` varchar(50) DEFAULT '待运行' COMMENT '运行状态',
  `last_run_time` datetime DEFAULT NULL COMMENT '最后运行时间',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_enabled` (`enabled`),
  KEY `idx_job_type` (`job_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='定时任务中心';

-- 任务执行记录表
CREATE TABLE `job_record` (
  `id` bigint(20) NOT NULL AUTO_INCREMENT,
  `job_name` varchar(255) NOT NULL COMMENT '任务名称',
  `job_type` varchar(50) NOT NULL COMMENT '任务类型',
  `cron_expr` varchar(100) NOT NULL COMMENT 'Cron表达式',
  `hook_url` varchar(500) DEFAULT NULL COMMENT 'Hook URL',
  `service_name` varchar(255) DEFAULT NULL COMMENT '服务名',
  `service_method` varchar(255) DEFAULT NULL COMMENT '方法名',
  `args` text COMMENT '参数',
  `job_result_flag` varchar(50) DEFAULT NULL COMMENT '执行结果标记: success/failed',
  `run_result` text COMMENT '执行结果',
  `run_err` text COMMENT '错误信息',
  `create_time` datetime DEFAULT CURRENT_TIMESTAMP,
  `update_time` datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_job_name` (`job_name`),
  KEY `idx_create_time` (`create_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='定时任务执行记录';
```

### 配置文件

在 `config.json` 中添加：

```json
{
  "Framework": {
    "Mysql": "default"
  },
  "Jobs": {
    "BanEnvList": ["test", "local"]  // 禁止启动的环境
  }
}
```

### 服务注册（CronJob 类型）

```go
package service

import (
    "encoding/json"
    "github.com/xxzhwl/gaia"
)

type ReportService struct{}

// GenerateDailyReport 生成日报（将被定时任务调用）
func (s *ReportService) GenerateDailyReport(args []byte) (any, error) {
    var req struct {
        Date string `json:"date"`
    }
    if err := json.Unmarshal(args, &req); err != nil {
        return nil, err
    }
    
    // 生成报表逻辑
    // ...
    
    return "报表生成成功", nil
}

func init() {
    // 注册服务到 Proxy
    gaia.RegisterProxy("ReportService", &ReportService{})
}
```

### 启动 Jobs 服务

```go
package main

import (
    "github.com/xxzhwl/gaia/components/jobs"
)

func main() {
    // 创建 Jobs 实例（支持秒级调度）
    jobRunner := jobs.NewRunJob()
    // 或使用秒级调度：jobRunner := jobs.NewSecondJobs()
    
    // 可选：自定义数据库配置
    // jobRunner = jobRunner.WithDbSchema("Framework.Mysql")
    
    // 启动（阻塞，建议在单独 goroutine 中运行）
    go jobRunner.Run()
}
```

### 任务管理

#### 1. 通过代码添加任务

```go
// 添加 CronJob 类型任务（调用服务方法）
jobId, err := jobRunner.AddJob(jobs.AddJobArgs{
    JobBase: jobs.JobBase{
        JobName:       "每日报表生成",
        JobType:       jobs.CronJob,
        CronExpr:      "0 0 1 * * *",  // 每天凌晨1点
        ServiceName:   "ReportService",
        ServiceMethod: "GenerateDailyReport",
        Args:          []byte(`{"date":"2025-02-11"}`),
        Timeout:       300,  // 5分钟超时
    },
    Enable: true,
})

// 添加 CronHook 类型任务（调用 HTTP 接口）
jobId, err := jobRunner.AddJob(jobs.AddJobArgs{
    JobBase: jobs.JobBase{
        JobName:  "数据同步通知",
        JobType:  jobs.CronHook,
        CronExpr: "0 */5 * * * *",  // 每5分钟
        HookUrl:  "http://localhost:8080/api/sync",
        Args:     []byte(`{"type":"full"}`),
        Timeout:  60,
    },
    Enable: true,
})
```

#### 2. 通过数据库添加任务

直接插入 `job_center` 表：

```sql
-- CronJob 类型
INSERT INTO `job_center` 
(`job_name`, `job_type`, `cron_expr`, `service_name`, `service_method`, `args`, `enabled`)
VALUES 
('每日报表生成', 'cron_job', '0 0 1 * * *', 'ReportService', 'GenerateDailyReport', '{"date":"auto"}', 1);

-- CronHook 类型
INSERT INTO `job_center` 
(`job_name`, `job_type`, `cron_expr`, `hook_url`, `args`, `enabled`)
VALUES 
('数据同步通知', 'cron_hook', '0 */5 * * * *', 'http://localhost:8080/api/sync', '{"type":"full"}', 1);
```

#### 3. 管理接口

```go
// 获取任务列表
result, err := jobRunner.ListJobs(jobs.ListJobsArgs{
    Page:     1,
    PageSize: 20,
    JobType:  jobs.CronJob,
    JobName:  "报表",
}, ctx)

// 获取任务详情
detail, err := jobRunner.GetJobDetail(jobId, ctx)

// 获取执行记录
records, total, err := jobRunner.GetJobRecords(jobId, 1, 10, ctx)

// 启用/禁用任务
err = jobRunner.ToggleJob(jobId, false, ctx)  // 禁用
err = jobRunner.ToggleJob(jobId, true, ctx)   // 启用

// 立即执行一次
err = jobRunner.ExecuteJobImmediately(jobId, ctx)

// 获取正在运行的任务
runningJobs, err := jobRunner.GetRunningJobs(ctx)

// 更新任务
affected, err := jobRunner.UpdateJob(jobs.UpdateJobArgs{
    JobId: jobId,
    JobBase: jobs.JobBase{
        JobName:       "每日报表生成",
        JobType:       jobs.CronJob,
        CronExpr:      "0 30 2 * * *",  // 修改为凌晨2:30
        ServiceName:   "ReportService",
        ServiceMethod: "GenerateDailyReport",
        Args:          []byte(`{"date":"auto"}`),
    },
    Enable: true,
})

// 删除任务
err = jobRunner.RemoveJob(jobId)
```

### Cron 表达式说明

使用标准 Cron 表达式：

```
# 标准格式（分钟 小时 日 月 星期）
0 0 * * *      # 每天零点
0 */6 * * *    # 每6小时
0 0 * * 0      # 每周日零点
0 0 1 * *      # 每月1号零点

# 秒级格式（秒 分钟 小时 日 月 星期）- 使用 NewSecondJobs()
0 0 * * * *    # 每分钟
0 */5 * * * *  # 每5分钟
*/30 * * * * * # 每30秒
```

---

## 最佳实践

### 1. 任务幂等性

异步任务和定时任务都应保证幂等性，防止重复执行导致数据异常：

```go
func (s *OrderService) AsyncCreateOrder(args []byte) (any, error) {
    var req CreateOrderReq
    json.Unmarshal(args, &req)
    
    // 检查是否已处理过
    if s.orderExists(req.OrderNo) {
        return nil, nil  // 已处理，直接返回
    }
    
    // 执行业务逻辑
    // ...
}
```

### 2. 错误处理与重试

合理设置重试次数，对于不可重试的错误（如参数错误）应立即失败：

```go
func (s *OrderService) AsyncCreateOrder(args []byte) (any, error) {
    var req CreateOrderReq
    if err := json.Unmarshal(args, &req); err != nil {
        // 参数错误，不可重试
        return nil, fmt.Errorf("PARAM_ERROR: %w", err)
    }
    
    // 业务逻辑
    // ...
}
```

### 3. 超时控制

设置合理的超时时间，防止任务长时间占用资源：

```go
// Jobs 任务设置超时
JobBase{
    Timeout: 300,  // 5分钟
}

// AsyncTask 任务在业务层控制
func (s *OrderService) AsyncCreateOrder(args []byte) (any, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    
    // 使用 ctx 执行业务逻辑
    // ...
}
```

### 4. 监控与告警

关注以下监控指标：

- **AsyncTask**: 队列积压、执行成功率、平均执行时间
- **Jobs**: 任务执行超时次数、失败次数

### 5. 优雅关闭

服务停止时会自动处理：

- **AsyncTask**: 停止扫描新任务，等待正在执行的任务完成
- **Jobs**: 停止调度器，将运行中任务状态重置为待运行

---

## 常见问题

### Q: AsyncTask 任务一直不执行？

1. 检查调度器是否已启动
2. 检查数据库连接配置
3. 检查任务状态是否为 `Wait` 或 `Retry`
4. 查看日志是否有扫描到任务

### Q: Jobs 任务执行时间不准？

1. 检查服务器时间是否同步
2. 检查 Cron 表达式是否正确
3. 考虑使用 `NewSecondJobs()` 获取更精确的控制

### Q: 任务执行失败了如何排查？

1. 查看 `asynctask_exec_row` / `job_record` 表的执行记录
2. 查看业务服务日志（通过 `log_id` 关联）
3. 检查任务参数是否正确
