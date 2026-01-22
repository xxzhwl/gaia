# Gaia SDK

为企业级 Go 应用开发提供的高性能、模块化开发框架和工具集。Gaia SDK 封装了大量常用功能模块，提供统一的配置管理、数据验证、异步任务、定时任务、数据库操作等核心能力，帮助开发者快速构建稳定、可扩展的后台服务。

## ✨ 特性

- **模块化设计**: 独立的功能模块，按需使用，代码清晰易于维护
- **高性能 HTTP 服务**: 基于 CloudWeGo Hertz 框架，提供高性能的 HTTP API 服务
- **统一配置管理**: 支持本地 JSON 配置、环境变量和远程配置中心，配置优先级明确
- **数据验证框架**: 强大的结构体标签验证系统，支持多种验证规则
- **异步任务处理**: 内置异步任务调度器，支持任务持久化和重试机制
- **定时任务系统**: 灵活的定时任务管理，支持分布式调度
- **数据库操作**: 集成 GORM 和 GORM Gen，提供类型安全的 DAO 层
- **缓存支持**: Redis 客户端封装，支持分布式锁和缓存策略
- **多种存储集成**: 支持腾讯云 COS、MinIO 等对象存储服务
- **消息通知**: 集成飞书机器人等消息通知渠道
- **链路追踪**: 内置 OpenTelemetry 支持，便于分布式系统调试
- **AI 能力集成**: 封装 OpenAI 等 AI 服务客户端

## 🛠 技术栈

- **编程语言**: Go 1.24.4+
- **Web 框架**: CloudWeGo Hertz
- **ORM**: GORM + GORM Gen
- **数据库**: MySQL, PostgreSQL, ClickHouse
- **缓存**: Redis (go-redis)
- **搜索**: Elasticsearch
- **对象存储**: 腾讯云 COS, MinIO
- **消息队列**: Apache Kafka
- **服务发现**: Consul
- **链路追踪**: OpenTelemetry + Jaeger
- **配置中心**: 支持 Consul 远程配置
- **AI 服务**: OpenAI API

## 📦 安装

```bash
# 使用 go get 安装
go get github.com/xxzhwl/gaia

# 或者在 go.mod 中添加依赖
require github.com/xxzhwl/gaia v0.0.0
```

## 🚀 快速开始

### 1. 创建简单的 HTTP 服务

```go
package main

import (
    "github.com/xxzhwl/gaia"
    "github.com/xxzhwl/gaia/framework"
    "github.com/xxzhwl/gaia/framework/server"
)

func init() {
    // 框架初始化
    framework.Init()
}

func main() {
    // 创建默认 HTTP 服务器
    s := server.DefaultApp()
    
    // 注册路由
    s.GET("/api/health", server.MakeHandler(func(arg server.Request) (any, error) {
        return map[string]string{"status": "healthy"}, nil
    }))
    
    // 启动服务
    s.Run()
}
```

### 2. 配置数据验证

```go
type LoginRequest struct {
    Username string `json:"username" require:"1"`
    Password string `json:"password" require:"1"`
    Email    string `json:"email" validate:"email"`
}

func LoginHandler(arg server.Request) (any, error) {
    req := LoginRequest{}
    // 自动验证请求数据
    if err := arg.BindJsonWithChecker(&req); err != nil {
        return nil, err
    }
    
    // 业务逻辑
    return map[string]any{"message": "登录成功"}, nil
}
```

### 3. 数据库操作

```go
import "github.com/xxzhwl/gaia"

// 获取数据库连接
db, err := gaia.NewFrameworkMysql()
if err != nil {
    gaia.ErrorLog(err.Error())
    return
}

// 使用 GORM 查询
var users []User
result := db.GetGormDb().Where("status = ?", "active").Find(&users)
```

## 📁 项目结构

```
gaia/
├── core modules (根目录文件)
│   ├── config.go           # 配置管理模块
│   ├── cache.go            # 缓存操作模块
│   ├── context.go          # 上下文管理
│   ├── datachecker.go      # 数据验证框架
│   ├── datetime.go         # 时间处理工具
│   ├── filesystem.go       # 文件系统操作
│   ├── httpserver.go       # HTTP 服务器封装
│   ├── list.go             # 列表/切片操作
│   ├── log.go              # 日志系统
│   ├── mail.go             # 邮件发送
│   ├── message.go          # 消息通知
│   ├── mysql.go            # MySQL 数据库操作
│   ├── proxy.go            # 代理模式注册与调用
│   ├── retry.go            # 重试机制
│   └── ...
├── framework/              # 框架核心
│   ├── init.go             # 框架初始化
│   ├── server/             # HTTP 服务器实现
│   │   ├── server.go       # 服务器主逻辑
│   │   ├── base.go         # 基础请求处理
│   │   ├── auth.go         # 认证中间件
│   │   ├── jwt.go          # JWT 处理
│   │   ├── cors.go         # 跨域支持
│   │   ├── operate.go      # 通用操作
│   │   └── query.go        # 通用查询
│   ├── httpclient/         # HTTP 客户端
│   ├── logImpl/            # 日志实现
│   ├── messageImpl/        # 消息实现
│   └── ...
├── components/             # 扩展组件
│   ├── asynctask/          # 异步任务系统
│   ├── jobs/               # 定时任务系统
│   ├── redis/              # Redis 客户端
│   ├── es/                 # Elasticsearch 客户端
│   ├── kafka/              # Kafka 客户端
│   ├── storage/            # 存储服务
│   └── ...
├── cvt/                    # 数据类型转换
├── dic/                    # 字典/Map 操作
├── errwrap/                # 错误包装处理
└── ai/                     # AI 服务集成
```

## 🧩 核心模块详解

### 配置管理 (config.go)

提供统一的配置管理接口，支持多种配置源：

```go
// 获取配置值
port := gaia.GetSafeConfString("Server.Port")
timeout := gaia.GetSafeConfInt64("Server.Timeout")

// 加载配置到结构体
type ServerConfig struct {
    Port string `json:"port"`
}
config := &ServerConfig{}
gaia.LoadConfToObj("Server", config)

// 支持环境变量覆盖
// 优先级: 环境变量 > 本地配置 > 远程配置
```

### 数据验证 (datachecker.go)

基于结构体标签的强大验证系统：

```go
type UserRequest struct {
    ID       int    `json:"id" require:"1"`
    Name     string `json:"name" require:"1"`
    Email    string `json:"email" validate:"email"`
    Age      int    `json:"age" validate:"range:18-100"`
    Phone    string `json:"phone" validate:"phone"`
    Birthday string `json:"birthday" validate:"date"`
}

// 自动验证
checker := gaia.NewDataChecker()
err := checker.CheckStruct(&request)
```

支持的验证规则：
- `require:"1"` - 必填字段
- `validate:"email"` - 邮箱格式
- `validate:"phone"` - 手机号格式
- `validate:"date"` - 日期格式
- `validate:"range:min-max"` - 数值范围
- `validate:"minlen:5"` - 最小长度
- `validate:"maxlen:20"` - 最大长度

### 缓存操作 (cache.go)

统一的缓存操作接口：

```go
// 设置缓存
gaia.CacheSet("user:1", userData, 3600)

// 获取缓存
data, found := gaia.CacheGet("user:1")

// 批量操作
gaia.CacheMSet(map[string]any{
    "key1": "value1",
    "key2": "value2",
}, 3600)

// 分布式锁
lock := gaia.NewRedisLock("resource:lock", 10*time.Second)
if lock.Acquire() {
    defer lock.Release()
    // 临界区操作
}
```

### HTTP 服务器封装 (httpserver.go)

简化的 HTTP 服务器创建和管理：

```go
// 创建服务器配置
config := &gaia.HTTPServerConfig{
    Port: "8080",
    ReadTimeout: 30,
    WriteTimeout: 30,
}

// 创建服务器
server := gaia.NewHTTPServer(config)

// 注册路由
server.GET("/api/users", getUserHandler)
server.POST("/api/users", createUserHandler)

// 启动服务
server.Run()
```

## 🏗 框架使用指南

### 框架初始化

```go
import "github.com/xxzhwl/gaia/framework"

func main() {
    // 框架初始化 - 必须调用
    framework.Init()
    
    // 初始化后可以使用所有 Gaia 功能
    gaia.Info("框架初始化完成")
}
```

初始化过程包括：
1. 日志系统注入和配置
2. 系统名称设置
3. 远程配置中心注册
4. 链路追踪设置
5. HTTP 客户端前置处理器
6. 消息提醒系统
7. 数据库日志配置

### 创建 HTTP 服务

```go
import "github.com/xxzhwl/gaia/framework/server"

// 创建默认应用（使用 "Server" schema）
app := server.DefaultApp()

// 或指定 schema
app := server.NewApp("CustomSchema")

// 注册通用处理器（健康检查、通用查询/操作）
app.RegisterCommonHandler()

// 自定义路由注册
app.GET("/api/demo", server.MakeHandler(demoHandler))

// 启动服务
app.Run()
```

### 请求处理

```go
// 请求处理器定义
func demoHandler(arg server.Request) (any, error) {
    // 获取 URL 参数
    id := arg.GetUrlParam("id")
    
    // 获取查询参数
    page := arg.GetUrlQuery("page", "1")
    
    // 绑定 JSON 请求体（带验证）
    req := DemoRequest{}
    if err := arg.BindJsonWithChecker(&req); err != nil {
        return nil, err
    }
    
    // 获取请求上下文
    ctx := arg.TraceContext
    
    // 返回响应
    return map[string]any{
        "id":   id,
        "data": req,
    }, nil
}

// 中间件创建
func authMiddleware(arg server.Request) error {
    token := string(arg.C().GetHeader("Authorization"))
    if token == "" {
        return errors.New("未授权")
    }
    // 验证逻辑
    return nil
}

// 注册中间件
server.MakePlugin(authMiddleware)
```

## 🔧 组件集成

### 异步任务系统 (components/asynctask/)

```go
import "github.com/xxzhwl/gaia/components/asynctask"

// 定义任务
type EmailTask struct {
    To      string
    Subject string
    Body    string
}

func (t *EmailTask) Execute() error {
    // 发送邮件逻辑
    return gaia.SendEmail(t.To, t.Subject, t.Body)
}

// 注册任务
asynctask.RegisterTask("send_email", func(data []byte) (asynctask.Task, error) {
    task := &EmailTask{}
    if err := json.Unmarshal(data, task); err != nil {
        return nil, err
    }
    return task, nil
})

// 提交任务
taskData, _ := json.Marshal(&EmailTask{
    To:      "user@example.com",
    Subject: "欢迎邮件",
    Body:    "欢迎使用我们的服务",
})
asynctask.SubmitTask("send_email", taskData)
```

### 定时任务系统 (components/jobs/)

```go
import "github.com/xxzhwl/gaia/components/jobs"

// 定义定时任务
type CleanupJob struct{}

func (j *CleanupJob) Run() {
    // 清理逻辑
    gaia.Info("执行清理任务")
}

// 注册任务
jobs.RegisterCronJob("cleanup_job", &CleanupJob{}, "0 2 * * *") // 每天凌晨2点

// 启动定时任务服务
jobs.StartCronService()
```

### Redis 客户端 (components/redis/)

```go
import "github.com/xxzhwl/gaia/components/redis"

// 获取 Redis 客户端
client := redis.NewClient()

// 基本操作
client.Set("key", "value", 3600*time.Second)
value := client.Get("key")

// 分布式锁
lock := redis.NewLock("resource:lock", 10*time.Second)
if lock.Acquire() {
    defer lock.Release()
}

// 发布订阅
redis.Publish("channel", "message")
```

### Elasticsearch 客户端 (components/es/)

```go
import "github.com/xxzhwl/gaia/components/es"

// 创建客户端
client := es.NewClient()

// 索引文档
doc := map[string]any{"title": "测试文档", "content": "文档内容"}
client.Index("articles", "doc_id", doc)

// 搜索
query := map[string]any{
    "query": map[string]any{
        "match": map[string]any{"title": "测试"},
    },
}
results := client.Search("articles", query)
```

## ⚙️ 配置管理

### 配置文件结构

Gaia SDK 支持灵活的配置管理，配置文件通常位于 `configs/local/config.json`：

```json
{
  "SystemEnName": "YourApp",
  "SystemCnName": "你的应用",
  
  "Framework": {
    "Mysql": "user:pass@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=True&loc=Local",
    "Redis": {
      "Address": "localhost:6379",
      "Password": ""
    },
    "ES": {
      "Address": "http://localhost:9200",
      "UserName": "",
      "Password": ""
    }
  },
  
  "Server": {
    "Port": "8080",
    "Cors": {
      "Enable": true,
      "AllowOrigins": ["http://localhost:3000"]
    },
    "Logger": {
      "PrintConsole": true,
      "DetailMode": false,
      "EnablePushLog": true
    }
  },
  
  "AsyncTask": {
    "Port": "8081",
    "Mysql": "user:pass@tcp(localhost:3306)/task_db?charset=utf8mb4&parseTime=True&loc=Local"
  }
}
```

### 配置优先级

1. **环境变量**: 最高优先级，格式为 `SCHEMA_KEY`（如 `SERVER_PORT`）
2. **本地配置**: `configs/local/config.json`
3. **远程配置**: 配置中心（如 Consul）

### 配置 Schema 支持

支持多 Schema 配置，便于多环境管理：

```go
// 获取不同 Schema 的配置
serverPort := gaia.GetSafeConfString("Server.Port")        // 默认 Schema
taskPort := gaia.GetSafeConfString("AsyncTask.Port")       // AsyncTask Schema

// 创建不同 Schema 的服务
serverApp := server.NewApp("Server")        // 使用 Server Schema
taskApp := server.NewApp("AsyncTask")       // 使用 AsyncTask Schema
```

## 📚 API 参考

### 核心函数

| 函数 | 说明 |
|------|------|
| `gaia.GetSafeConfString(key)` | 安全获取字符串配置 |
| `gaia.GetSafeConfInt64(key)` | 安全获取整数配置 |
| `gaia.GetSafeConfBool(key)` | 安全获取布尔配置 |
| `gaia.LoadConfToObj(schema, obj)` | 加载配置到结构体 |
| `gaia.NewDataChecker()` | 创建数据验证器 |
| `gaia.CacheSet(key, value, ttl)` | 设置缓存 |
| `gaia.CacheGet(key)` | 获取缓存 |
| `gaia.Info(msg)`, `gaia.Error(msg)` | 日志记录 |

### 框架函数

| 函数 | 说明 |
|------|------|
| `framework.Init()` | 框架初始化 |
| `server.DefaultApp()` | 创建默认 HTTP 应用 |
| `server.NewApp(schema)` | 创建指定 Schema 的应用 |
| `server.MakeHandler(handler)` | 创建请求处理器 |
| `server.MakePlugin(middleware)` | 创建中间件插件 |

### 数据验证标签

| 标签 | 说明 | 示例 |
|------|------|------|
| `require` | 必填字段 | `require:"1"` |
| `validate` | 验证规则 | `validate:"email"` |
| `range` | 数值范围 | `validate:"range:18-100"` |
| `minlen` | 最小长度 | `validate:"minlen:6"` |
| `maxlen` | 最大长度 | `validate:"maxlen:20"` |
| `regexp` | 正则表达式 | `validate:"regexp:^\\d{11}$"` |

## 🧪 开发指南

### 添加新模块

1. **创建模块文件**：
   ```go
   // newmodule.go
   package gaia
   
   type NewModule struct {
       // 模块结构
   }
   
   func NewNewModule() *NewModule {
       return &NewModule{}
   }
   
   func (m *NewModule) DoSomething() error {
       // 实现逻辑
       return nil
   }
   ```

2. **编写单元测试**：
   ```go
   // newmodule_test.go
   func TestNewModule(t *testing.T) {
       module := NewNewModule()
       err := module.DoSomething()
       assert.NoError(t, err)
   }
   ```

3. **文档更新**：更新 README 和相关文档

### 集成外部服务

1. **创建客户端封装**：
   ```go
   package components/external
   
   type ExternalClient struct {
       baseURL string
       apiKey  string
   }
   
   func NewClient(baseURL, apiKey string) *ExternalClient {
       return &ExternalClient{baseURL, apiKey}
   }
   
   func (c *ExternalClient) CallAPI() ([]byte, error) {
       // 调用外部 API
   }
   ```

2. **配置支持**：添加配置结构，支持从配置文件加载

### 性能优化建议

1. **缓存策略**：合理使用缓存减少数据库访问
2. **连接池**：数据库和 Redis 连接池配置优化
3. **异步处理**：耗时操作使用异步任务
4. **批量操作**：减少网络往返，使用批量操作

## 🤝 贡献指南

### 开发流程

1. **Fork 仓库**：创建个人分支
2. **创建特性分支**：`git checkout -b feature/awesome-feature`
3. **提交更改**：`git commit -am 'Add awesome feature'`
4. **推送到分支**：`git push origin feature/awesome-feature`
5. **创建 Pull Request**

### 代码规范

1. **Go 代码**：遵循 Go 官方代码规范
2. **命名规范**：使用有意义的变量和函数名
3. **注释要求**：公共函数必须有注释
4. **测试覆盖**：新功能必须包含单元测试

### 提交信息格式

```
<type>: <description>

[optional body]

[optional footer]
```

类型说明：
- `feat`: 新功能
- `fix`: 错误修复
- `docs`: 文档更新
- `style`: 代码格式调整
- `refactor`: 代码重构
- `test`: 测试相关
- `chore`: 构建过程或辅助工具变动

## 📄 许可证

[待补充]

## 📞 联系方式

- **作者**: wanlizhan
- **仓库**: [github.com/xxzhwl/gaia](https://github.com/xxzhwl/gaia)
- **问题反馈**: [GitHub Issues](https://github.com/xxzhwl/gaia/issues)

## 🙏 致谢

感谢所有为 Gaia SDK 贡献代码和提出建议的开发者！