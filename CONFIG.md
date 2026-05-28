# Gaia 框架配置项参考手册

本文档列出 Gaia 框架在源码中显式从配置中读取的所有配置键、含义、默认值，并附带完整 YAML / JSON 示例。

配置加载方式：项目启动时框架会从 `configs/{env}/*.{yaml,json,toml}` 加载；代码中通过 `gaia.GetSafeConfXxx("Key")` / `gaia.UnmarshalSafeConfig("Key", &v)` 等 API 读取。

### 命名约定

- `Framework.*` —— 框架基础组件（DB / 中间件 / 客户端）
- `Account.*` —— 内置账号体系
- `{schema}.*` —— 表示该组件支持多实例，调用方传入 schema 名（默认 schema 在每节标注）
- `Server.*` / `RpcServer.*` / `RpcClient.*` —— HTTP / RPC 服务端、客户端默认 schema

---

## 目录

1. [Gaia 框架核心](#一gaia-框架核心)
2. [数据库 / ORM](#二数据库--orm)
3. [HTTP Server 中间件](#三http-server-中间件schema-默认-server)
4. [RPC（gRPC）](#四rpcgrpc)
5. [可观测性（Tracer / Metrics）](#五可观测性)
6. [日志 / 通知 / 远程日志](#六日志--通知--远程日志)
7. [远程配置中心](#七远程配置中心nacos--k8s-configmap)
8. [Jobs / 异步任务](#八jobs--异步任务)
9. [组件（Redis / Mongo / Kafka / 存储 等）](#九组件redis--mongo--kafka--对象存储-等)
10. [Account 账号体系](#十account-账号体系)
11. [完整 YAML 示例](#十一完整-yaml-示例)
12. [完整 JSON 示例](#十二完整-json-示例)

---

## 一、Gaia 框架核心

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Debug` | bool | false | 全局调试开关；开启后 MySQL/Postgres 会打印 SQL，部分组件输出详细日志 |
| `Environment` | string | development | 部署环境标识，写入 OTel `deployment.environment` 标签，影响指标/链路分组 |
| `SystemCnName` | string | – | 系统中文名，用于初始化检查、日志/告警标头 |
| `SystemEnName` | string | – | 系统英文名，同上；同时作为默认 service 名兜底 |
| `Gaia.ProbeTimeout` | int (秒) | 5 | 组件健康探针（数据库/Redis/Kafka 等）的统一超时时间 |

---

## 二、数据库 / ORM

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Framework.Mysql` | string | – | 默认 MySQL DSN，例 `user:pass@tcp(host:3306)/db?parseTime=true&loc=Local` |
| `Framework.Postgresql` | string | – | 默认 Postgres DSN，例 `host=... user=... password=... dbname=... port=5432 sslmode=disable` |
| `Gorm.LocalLevel` | string | warn | GORM SQL 日志的本地输出最低级别：silent / error / warn / info / trace / debug |
| `Gorm.RemoteLevel` | string | warn | GORM SQL 日志的远程 ES 推送最低级别：同上 |
| `Gorm.SlowThreshold` | int64 (ms) | 200 | GORM 慢 SQL 阈值，超过会标记 WARN |

### 设计说明

- GORM 那一层自身的 `logger.Config.LogLevel` 由框架自动推导为 `looserOf(LocalLevel, RemoteLevel)`（取两者中更宽松的）。两侧都设为 `silent` 时整条 trace 直接 short-circuit，节省 CPU。
- 想"本地只看错误、远程 ES 收全量"：`LocalLevel=error` + `RemoteLevel=info`。
- 想完全关闭 DB 日志：`LocalLevel=silent` + `RemoteLevel=silent`。
- 旧字段 `Gorm.LogLevel` / `Logger.DbLocalLevel` / `Logger.DbRemoteLevel` 已废弃，不再读取。

多 DSN 场景：可调用 `gaia.NewFrameworkMysqlWithSchema("Framework.MysqlOrder")` 等读取自定义 schema。

---

## 三、HTTP Server 中间件（schema 默认 Server）

`{schema}` 默认是 `Server`；同一个进程开多个 HTTP Server 时可使用 `ServerAdmin` / `ServerInner` 等。

### 3.1 监听 / TLS

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Server.Port` | string | – | 监听端口，例 `:8080` |
| `{schema}.EnableTLS` | bool | false | 是否启用 HTTPS，影响 HSTS 默认值 |

### 3.2 CORS

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Cors.Enable` | bool | false | 启用 CORS 插件 |
| `{schema}.Cors.AllowOrigins` | []string | – | 允许的 Origin 列表，支持 `*` 与精确匹配 |
| `{schema}.Cors.AllowMethods` | []string | – | 允许的 HTTP 方法 |
| `{schema}.Cors.AllowHeaders` | []string | – | 允许的请求头 |
| `{schema}.Cors.AllowFiles` | bool | false | 是否允许 `file://` 来源（本地调试用） |
| `{schema}.Cors.AllowCredentials` | bool | true | 允许携带 Cookie / Authorization |
| `{schema}.Cors.AllowWebSockets` | bool | true | 允许 WebSocket 升级 |
| `{schema}.Cors.MaxAge` | int64 (小时) | 24 | 预检（OPTIONS）结果缓存时长 |

### 3.3 Pprof / Debug

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Pprof.Enable` | bool | false | 启用 `/debug/pprof/*` 端点 |

### 3.4 超时 / 限流 / 熔断

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Timeout.Enable` | bool | false | 启用请求级超时插件 |
| `{schema}.Timeout.Seconds` | int64 | 30 | 请求超时阈值（秒），超过返回 504 |
| `{schema}.Timeout.SlowThresholdSeconds` | int64 | – | 慢请求阈值，仅记录日志/指标，不熔断 |
| `{schema}.RateLimit.Enable` | bool | false | 启用令牌桶限流 |
| `{schema}.RateLimit.Capacity` | int64 | – | 令牌桶容量 |
| `{schema}.RateLimit.Rate` | float64 | – | 令牌生成速率（每秒） |
| `{schema}.RateLimit.IdleTTL` | int64 (秒) | – | 空闲桶回收时间 |
| `{schema}.CircuitBreaker.Enable` | bool | false | 启用熔断器（基于错误率/超时） |

### 3.5 压缩 / 安全头

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Gzip.Enable` | bool | false | 启用 gzip 响应压缩 |
| `{schema}.Gzip.MinLength` | int64 | – | 大于该字节数才压缩 |
| `{schema}.Gzip.Level` | int64 | – | 压缩级别 1-9 |
| `{schema}.Security.Enable` | bool | false | 启用安全响应头（HSTS/CSP 等）插件 |
| `{schema}.Security.HSTS` | string | 内置 | `Strict-Transport-Security` 头值，TLS 关闭时不下发 |
| `{schema}.Security.CSP` | string | 内置 | `Content-Security-Policy` 头值 |
| `{schema}.Security.PermissionsPolicy` | string | 内置 | `Permissions-Policy` 头值 |

### 3.6 健康检查 / 指标暴露

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Probe.ReadyTimeoutSec` | int64 | 5 | `/readyz` 中所有组件检查的总超时 |
| `{schema}.Metrics.ExposeOnMainPort` | bool | false | 是否在主端口暴露 `/metrics`（默认走独立端口） |
| `{schema}.Metrics.Path` | string | /metrics | 主端口暴露路径 |

### 3.7 访问日志 / 鉴权 / HTTP 客户端

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Logger.MaxBodyBytes` | int64 | – | 访问日志记录请求/响应 body 的最大字节数（0 不记录） |
| `Auth.AllowedTimeWindow` | int64 (秒) | – | 鉴权时间窗口（防重放攻击的允许时差） |
| `HttpClient.LogBody` | bool | false | 框架 httpclient 是否打印请求/响应 body |

---

## 四、RPC（gRPC）

已移除 Kitex，仅保留 gRPC。服务端见 `framework/rpcserver`，客户端见 `framework/rpcclient`，服务注册/发现见 `framework/rpcregistry`（支持 Consul / Nacos）。

### 4.1 服务端（schema 默认 RpcServer）

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Host` | string | 0.0.0.0 | 监听地址（接入注册中心时建议填可被发现的真实 IP） |
| `{schema}.Port` | string | 9090 | 监听端口 |
| `{schema}.KeepAliveTime` | int64 (秒) | 30 | gRPC keepalive 周期 |
| `{schema}.KeepAliveTimeout` | int64 (秒) | 10 | keepalive ping 超时 |
| `{schema}.MaxRecvMsgSize` | int64 (MB) | 4 | 最大接收消息 |
| `{schema}.MaxSendMsgSize` | int64 (MB) | 4 | 最大发送消息 |
| `{schema}.MaxConcurrentStreams` | int64 | 100 | 最大并发流 |
| `{schema}.TLS.Enable` | bool | false | 启用 TLS |
| `{schema}.TLS.CrtPath` | string | – | 服务端证书路径 |
| `{schema}.TLS.KeyPath` | string | – | 服务端私钥路径 |
| `{schema}.TLS.CAPath` | string | – | CA 证书路径（配置后启用 mTLS 双向认证） |

**限流（令牌桶）**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.RateLimit.Enable` | bool | false | 启用限流 |
| `{schema}.RateLimit.Capacity` | int64 | 200 | 令牌桶容量（突发上限） |
| `{schema}.RateLimit.Rate` | float64 | 100 | 令牌生成速率（个/秒） |
| `{schema}.RateLimit.PerClient` | bool | false | 按客户端 IP 维度限流（默认按 method） |
| `{schema}.RateLimit.MaxKeys` | int64 | 4096 | PerClient 时限流器 key 上限（LRU 淘汰，防内存泄漏） |

**鉴权（metadata token）**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Auth.Enable` | bool | false | 启用鉴权 |
| `{schema}.Auth.HeaderKey` | string | authorization | 读取 token 的 metadata key |
| `{schema}.Auth.Scheme` | string | Bearer | token 前缀（为空则取整个 header 值） |
| `{schema}.Auth.Tokens` | string (逗号分隔) | – | 允许的 token 列表 |
| `{schema}.Auth.SkipMethods` | string (逗号分隔) | – | 免鉴权方法全名（健康检查/反射默认免） |

也可调用 `rpcserver.SetAuthFunc(fn)` 注入自定义鉴权逻辑（优先级高于内置 token 校验）。

**访问日志（gRPC 入站写入 in_log，gRPC 客户端出站写入 out_log）**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Logger.DetailMode` | bool | false | 详细模式（输出 Method/Peer/Req/Resp/Code/Duration） |
| `{schema}.Logger.PrintConsole` | bool | true | 控制台/文件输出 |
| `{schema}.Logger.EnablePushLog` | bool | false | 推送远程日志到 ES（按 direction 写入 in_log/out_log） |
| `{schema}.Logger.LogBody` | bool | false | 是否记录 req/resp 消息体 |
| `{schema}.Logger.MaxBodyLogBytes` | int64 | 4096 | 消息体最大记录字节数 |

### 4.2 客户端（schema 默认 RpcClient）

**连接 / 负载均衡**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.DialTimeout` | int64 (秒) | 10 | 建连超时（BlockOnDial 时阻塞等待） |
| `{schema}.CallTimeout` | int64 (秒) | 0 | 单次调用默认超时（0 不限制） |
| `{schema}.LoadBalancing` | string | round_robin | 负载均衡策略：round_robin / pick_first |
| `{schema}.ResolveEvery` | int64 (秒) | 10 | registry 不支持 Watch 时的轮询间隔 |
| `{schema}.KeepAliveTime` | int64 (秒) | 30 | keepalive 周期 |
| `{schema}.KeepAliveTimeout` | int64 (秒) | 10 | keepalive ping 超时 |
| `{schema}.MaxRecvMsgSize` | int64 (MB) | 4 | 最大接收消息 |
| `{schema}.MaxSendMsgSize` | int64 (MB) | 4 | 最大发送消息 |
| `{schema}.BlockOnDial` | bool | false | Dial 时阻塞直到连接就绪 |
| `{schema}.Services.{name}.Address` | string | – | 静态直连地址（无 registry 时使用，如 `localhost:9090`） |

**重试（gRPC retryPolicy，仅建议对幂等方法开启）**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Retry.Enable` | bool | false | 启用重试 |
| `{schema}.Retry.MaxAttempts` | int64 | 3 | 最大尝试次数（含首次，gRPC 限制 2~5） |
| `{schema}.Retry.InitialBackoffMs` | int64 | 100 | 首次重试退避（毫秒） |
| `{schema}.Retry.MaxBackoffMs` | int64 | 1000 | 最大退避（毫秒） |
| `{schema}.Retry.BackoffMultiplier` | float64 | 2.0 | 退避倍数 |
| `{schema}.Retry.RetryableStatusCodes` | string (逗号分隔) | UNAVAILABLE | 可重试状态码，如 `UNAVAILABLE,ABORTED` |

**鉴权 token 注入 / TLS**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Auth.Enable` | bool | false | 启用 token 注入 |
| `{schema}.Auth.HeaderKey` | string | authorization | 注入的 metadata key |
| `{schema}.Auth.Scheme` | string | Bearer | token 前缀 |
| `{schema}.Auth.Token` | string | – | 注入的 token 值 |
| `{schema}.TLS.Enable` | bool | false | 启用 TLS |
| `{schema}.TLS.CAPath` | string | – | CA 证书（校验服务端） |
| `{schema}.TLS.CrtPath` | string | – | 客户端证书（mTLS） |
| `{schema}.TLS.KeyPath` | string | – | 客户端私钥（mTLS） |
| `{schema}.TLS.ServerName` | string | – | 校验用的服务端名称 |
| `{schema}.TLS.InsecureSkipVerify` | bool | false | 跳过服务端证书校验（仅测试） |

### 4.3 服务注册 / 发现（`{schema}.Registry`，server 与 client 通用）

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Registry.Enable` | bool | false | 启用注册/发现（false 时走静态地址直连） |
| `{schema}.Registry.Type` | string | consul | 注册中心类型：consul / nacos |

**Consul（Type=consul）**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Registry.Endpoint` | string | localhost:8500 | Consul 地址 |
| `{schema}.Registry.CheckTTL` | int64 (秒) | 10 | 健康检查间隔 |
| `{schema}.Registry.DeregAfter` | string | 30s | 健康检查失败后注销时间 |
| `{schema}.Registry.Token` | string | – | ACL Token |
| `{schema}.Registry.Datacenter` | string | – | 数据中心 |
| `{schema}.Registry.Tag` | string | gaia-rpc | 服务标签（过滤用） |

**Nacos（Type=nacos）**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `{schema}.Registry.Nacos.ServerAddrs` | string (逗号分隔) | – | Nacos 地址，与 Endpoint 二选一 |
| `{schema}.Registry.Nacos.Namespace` | string | – | 命名空间 ID |
| `{schema}.Registry.Nacos.Group` | string | DEFAULT_GROUP | 分组 |
| `{schema}.Registry.Nacos.Cluster` | string | DEFAULT | 集群名 |
| `{schema}.Registry.Nacos.Username` | string | – | 用户名 |
| `{schema}.Registry.Nacos.Password` | string | – | 密码 |
| `{schema}.Registry.Nacos.AccessKey` | string | – | 阿里云 MSE 鉴权 AccessKey |
| `{schema}.Registry.Nacos.SecretKey` | string | – | 阿里云 MSE 鉴权 SecretKey |
| `{schema}.Registry.Nacos.Endpoint` | string | – | 地址服务器端点（与 ServerAddrs 二选一） |
| `{schema}.Registry.Nacos.TimeoutMs` | uint64 | 5000 | 请求超时（毫秒） |
| `{schema}.Registry.Nacos.Ephemeral` | bool | true | 临时实例（进程退出自动剔除） |
| `{schema}.Registry.Nacos.Weight` | float64 | 10 | 实例权重 |

---

## 五、可观测性

### 5.1 Tracer（OpenTelemetry）

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Framework.Tracer.Enabled` | bool | false | 启用链路追踪 |
| `Framework.Tracer.Exporter` | string | otlphttp | 导出器：otlphttp / otlpgrpc / stdout |
| `Framework.Tracer.Endpoint` | string | exporter 默认 | OTLP collector 地址 |
| `Framework.Tracer.URLPath` | string | "" | OTLP HTTP 路径，如 `/v1/traces` |
| `Framework.Tracer.Insecure` | bool | true | 是否使用明文（关闭 TLS） |
| `Framework.Tracer.ServiceName` | string | – | 服务名（写入 resource） |
| `Framework.Tracer.SampleRate` | float64 | 1.0 | 采样率 0.0~1.0 |
| `Framework.Tracer.BatchTimeoutSec` | int64 | 1 | 批量导出周期（秒） |
| `Framework.Tracer.MaxQueueSize` | int64 | 1024 | span 缓冲队列上限 |
| `Framework.Tracer.MaxExportBatchSize` | int64 | 512 | 单批次最大 span 数 |
| `Framework.Tracer.Headers` | map[string]string | – | 导出器附加请求头（鉴权用） |

### 5.2 Metrics

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Framework.Metrics.Enabled` | bool | false | 启用指标采集 |
| `Framework.Metrics.Backend` | string | prometheus | 后端：prometheus / otlp |
| `Framework.Metrics.ServiceName` | string | – | 服务名（写入指标标签） |
| `Framework.Metrics.Prometheus.ListenAddr` | string | :9100 | Prometheus 暴露端口 |
| `Framework.Metrics.Prometheus.Path` | string | /metrics | Prometheus 路径 |
| `Framework.Metrics.OTLP.Endpoint` | string | localhost:4317 | OTLP 推送地址 |
| `Framework.Metrics.OTLP.Insecure` | bool | true | 是否明文推送 |
| `Framework.Metrics.OTLP.PushIntervalSec` | int64 | 10 | OTLP 推送周期（秒） |

---

## 六、日志 / 通知 / 远程日志

### 6.1 日志

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Logger.DisableLocalFile` | bool | false | 关闭本地文件日志 |
| `Logger.DisableRemote` | bool | false | 关闭远程日志（ES / Kafka） |
| `Logger.RemoteWatchInterval` | int64 (秒) | 10 | 框架后台拉取远程日志（如 ES）的间隔 |

### 6.2 ES（远程日志后端 / 业务直连）

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Framework.ES.Address` | []string | – | ES 节点列表 |
| `Framework.ES.UserName` | string | – | ES Basic 用户名 |
| `Framework.ES.Password` | string | – | ES Basic 密码 |

### 6.3 Kafka（远程日志写入）

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Framework.Kafka.Brokers` | string | – | Kafka broker 列表，逗号分隔 |

### 6.4 通知

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Message.Bark` | string | – | Bark 推送 URL（iOS 推送） |
| `Message.FeiShuRobot` | string | – | 飞书自定义机器人 webhook |

---

## 七、远程配置中心（Nacos / K8s ConfigMap）

自 2026-06 起，Gaia 不再内置 Consul / Etcd 作为远程配置中心 Provider；`components/consul` / `components/etcd` 仍可作为 KV 存储 / 服务发现独立使用。

### 7.1 通用入口

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `RemoteConfig.Type` | string | – | 远程配置类型：nacos / configmap / 空值（不启用） |

### 7.2 Nacos（RemoteConfig.Type=nacos）

**通用字段**

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `RemoteConfig.Nacos.ServerAddrs` | string (逗号分隔) | – | Nacos 服务地址（host:port），与 Endpoint 二选一 |
| `RemoteConfig.Nacos.Endpoint` | string | – | 阿里云 MSE / 公网 Nacos 服务发现端点 |
| `RemoteConfig.Nacos.Namespace` | string | public | 命名空间 ID |
| `RemoteConfig.Nacos.Group` | string | DEFAULT_GROUP | 所有 DataIds 共用的分组 |
| `RemoteConfig.Nacos.DataIds` | []string | – | 必填，至少一个；详见下方说明 |
| `RemoteConfig.Nacos.Format` | string | 按 DataId 后缀判定 | 默认内容格式：yaml / json；每个 DataId 优先按文件名后缀判定，无法判定时回退本字段 |
| `RemoteConfig.Nacos.Username` | string | – | 用户名 |
| `RemoteConfig.Nacos.Password` | string | – | 密码 |
| `RemoteConfig.Nacos.AccessKey` | string | – | MSE / 阿里云 Nacos 鉴权 AccessKey |
| `RemoteConfig.Nacos.SecretKey` | string | – | MSE / 阿里云 Nacos 鉴权 SecretKey |
| `RemoteConfig.Nacos.Scheme` | string | http | http / https |
| `RemoteConfig.Nacos.ContextPath` | string | – | 非默认部署路径，例如 `/nacos` |
| `RemoteConfig.Nacos.AppName` | string | – | 控制台来源识别 |
| `RemoteConfig.Nacos.LogLevel` | string | warn | SDK 日志级别 |
| `RemoteConfig.Nacos.DisableUseSnapshot` | bool | false | 远端不可达时禁止读 SDK 本地 snapshot |
| `RemoteConfig.Nacos.OpenKMS` | bool | false | 阿里云 KMS 解密；`cipher-` 前缀 dataId 自动解密 |
| `RemoteConfig.Nacos.RegionId` | string | – | KMS region |
| `RemoteConfig.Nacos.KMSVersion` | string | v1.0 | v1.0 / v3.0 |
| `RemoteConfig.Nacos.TimeoutMs` | uint64 | 5000 | 请求超时（毫秒） |
| `RemoteConfig.Nacos.LogDir` | string | – | SDK 本地日志落盘路径 |
| `RemoteConfig.Nacos.CacheDir` | string | – | SDK 本地缓存落盘路径 |
| `RemoteConfig.Nacos.CacheTTL` | int64 (秒) | 300 | BaseCenter 二级缓存 TTL |

**DataIds 数组**

`RemoteConfig.Nacos.DataIds` 是一个字符串数组，每个元素是一个 DataId 名称。数组顺序 = 优先级，后定义的覆盖先定义的。所有 DataId 共用 Nacos.Group 这一个分组（如需跨 group，需要业务层用多个 nacos.Client 实例自行编排）。

```yaml
RemoteConfig:
  Type: nacos
  Nacos:
    ServerAddrs: "127.0.0.1:8848"
    Group: DEFAULT_GROUP            # 所有 DataIds 共用此 group
    DataIds:
      - common.yaml                 # 优先级最低（base）
      - biz.yaml
      - overrides.yaml              # 优先级最高（override）
```

合并语义：顶层 deep-merge —— 子 map 递归合并、叶子值后者覆盖前者。任一 DataId 推送变更 → Nacos Client 自动重建合并视图 → Provider 触发 watcher 与缓存失效。

每个 DataId 的内容格式独立按文件名后缀判定（.json → json；.yml / .yaml → yaml），无法判定时回退到顶层 Nacos.Format，仍无效则兜底 yaml。

### 7.3 K8s ConfigMap（RemoteConfig.Type=configmap）

将 K8s ConfigMap 作为卷（或 subPath 文件）挂载到 Pod 文件系统，本 Provider 用 fsnotify 监听变更。不需要部署任何额外的配置中心组件，是部署在 K8s 上时最轻量的选项。

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `RemoteConfig.ConfigMap.Path` | string | – | 必填；单文件（subPath 挂载）或目录（默认挂载） |
| `RemoteConfig.ConfigMap.DirMode` | bool | 自动判定 | 显式声明目录模式；不填时按 Path 实际类型判定 |
| `RemoteConfig.ConfigMap.Format` | string | 按扩展名判定 | yaml / json，仅单文件模式生效 |
| `RemoteConfig.ConfigMap.DebounceMs` | int64 (毫秒) | 500 | 文件变更去抖窗口（K8s 切换 `..data` 符号链接会产生多事件） |
| `RemoteConfig.ConfigMap.CacheTTL` | int64 (秒) | 300 | BaseCenter 二级缓存 TTL |

目录模式下，每个文件对应一个顶层 key：扩展名为 yaml/yml/json 时解析为 map（key=去后缀的文件名），其余按字符串挂在 key=完整文件名 下。这与 K8s ConfigMap 的"key→value"语义一致。

---

## 八、Jobs / 异步任务

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Jobs.BanEnvList` | []string | – | 禁止运行 cron jobs 的环境列表（如 `["development","ci"]`） |

异步任务（asynctask）的存储/队列地址通过被注入的组件 schema 读取，无独立顶层键。

---

## 九、组件（Redis / Mongo / Kafka / 对象存储 等）

下列组件均支持多实例：调用 `NewXxxWithSchema(schema)` 时传入自定义 schema 名；不传则使用默认 schema。

### 9.1 Redis（默认 schema：Framework.Redis）

| 配置键 | 类型 | 作用 |
|--------|------|------|
| `{schema}.Address` | string | Redis 地址，例 `127.0.0.1:6379` |
| `{schema}.UserName` | string | ACL 用户名（Redis 6+） |
| `{schema}.Password` | string | 密码 |

### 9.2 MongoDB（schema 由调用方传入）

| 配置键 | 类型 | 作用 |
|--------|------|------|
| `{schema}.URI` | string | MongoDB URI |
| `{schema}.Database` | string | 默认数据库 |
| `{schema}.MaxPoolSize` | int64 | 连接池上限 |
| `{schema}.MinPoolSize` | int64 | 连接池下限 |
| `{schema}.ConnectTimeoutSec` | int64 | 连接超时（秒） |

### 9.3 ClickHouse（默认 schema：Framework.ClickHouse）

| 配置键 | 类型 | 作用 |
|--------|------|------|
| `{schema}.Addrs` | []string | ClickHouse 节点列表 |
| `{schema}.Database` | string | 默认 database |
| `{schema}.Username` | string | 用户名 |
| `{schema}.Password` | string | 密码 |

### 9.4 InfluxDB（默认 schema：Framework.InfluxDB）

| 配置键 | 类型 | 作用 |
|--------|------|------|
| `{schema}.URL` | string | InfluxDB 地址 |
| `{schema}.Token` | string | API Token |
| `{schema}.Org` | string | 组织名 |
| `{schema}.Bucket` | string | 默认 bucket |

### 9.5 Etcd（schema 由调用方传入）

| 配置键 | 类型 | 作用 |
|--------|------|------|
| `{schema}.Endpoints` | []string | etcd 节点 |
| `{schema}.Prefix` | string | key 前缀 |
| `{schema}.Username` | string | 用户名 |
| `{schema}.Password` | string | 密码 |
| `{schema}.DialTimeoutMs` | int64 | 拨号超时（毫秒） |
| `{schema}.RequestTimeoutMs` | int64 | 请求超时（毫秒） |

### 9.6 MQTT（schema 由调用方传入）

| 配置键 | 类型 | 作用 |
|--------|------|------|
| `{schema}.Broker` | string | MQTT broker，例 `tcp://host:1883` |
| `{schema}.ClientID` | string | 客户端 ID |
| `{schema}.Username` | string | 用户名 |
| `{schema}.Password` | string | 密码 |

### 9.7 RabbitMQ（默认 schema：Framework.RabbitMQ）

| 配置键 | 类型 | 作用 |
|--------|------|------|
| `{schema}.URL` | string | AMQP URL，例 `amqp://user:pass@host:5672/` |

### 9.8 对象存储

| 组件 | 默认 schema | 关键键 | 作用 |
|------|-------------|--------|------|
| OSS | `Framework.OSS` | `.Endpoint` `.Bucket` `.AccessKeyID` `.AccessKeySecret` `.UseInternalURL` | 阿里云 OSS |
| COS | `Framework.Cos` | `.SecretID` `.SecretKey` `.Region` `.Bucket` | 腾讯云 COS |
| MinIO | `Framework.{schema}` | `.Endpoint` `.UserName` `.Password` `.Secure` | MinIO / 自建 S3 兼容 |
| S3 | `Framework.S3` | `.Region` `.Bucket` `.AccessKeyID` `.SecretAccessKey` `.Endpoint` `.UsePathStyle` | AWS S3 |

---

## 十、Account 账号体系

### 10.1 主体

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.AppID` | string | gaia-account | 账号体系应用 ID，作为 JWT audience 默认值 |
| `Account.Mode` | string | production | 运行模式：production / development，影响审计、缓存、强校验 |
| `Account.DefaultTenantID` | string | defaultTenantID | 默认租户 ID（单租户模式使用） |
| `Account.Redis.KeyPrefix` | string | acct: | 账号体系所有 Redis key 的前缀 |
| `Account.PermissionCacheTTL` | int (分钟) | 10 | 权限决策缓存 TTL |
| `Account.PrincipalCacheMaxTTL` | int (分钟) | 5 | 用户身份缓存 TTL |
| `Account.Standalone.Profile` | string | full | 独立部署 API Profile：full / public / admin |

### 10.2 JWT（账号体系自有）

三级回退：`Account.JWT.X` → `JWT.X` → 内置默认。

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.JWT.SecretKey` | string | 回退 JWT.SecretKey | 签名密钥 |
| `Account.JWT.Issuer` | string | 回退 JWT.Issuer → gaia-account | iss |
| `Account.JWT.Audience` | string | 回退 Account.AppID | aud |
| `Account.JWT.AccessTokenExp` | int (分钟) | 15 | access token 有效期 |
| `Account.JWT.RefreshTokenExp` | int (分钟) | 43200 | refresh token 有效期（30 天） |
| `Account.JWT.EnableAccessTokenDenylistCheck` | bool | false | 是否每次校验都查 access token 黑名单 |
| `Account.JWT.DenylistCacheTTL` | int (秒) | 30 | 黑名单结果本地缓存 TTL |

### 10.3 密码策略

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.Password.MinLength` | int | 12 | 密码最小长度 |
| `Account.Password.MaxLength` | int | 128 | 密码最大长度 |
| `Account.Password.Argon2Time` | uint | 3 | Argon2 迭代次数 |
| `Account.Password.Argon2Memory` | uint | 65536 | Argon2 内存（KB） |
| `Account.Password.Argon2Threads` | uint | 2 | Argon2 并行度 |

### 10.4 验证码

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.Verification.CodeLength` | int | 6 | 验证码长度 |
| `Account.Verification.CodeTTL` | int (分钟) | 5 | 验证码有效期 |
| `Account.Verification.MaxAttempts` | int | 5 | 单个验证码最多尝试次数 |
| `Account.Verification.SendInterval` | int (秒) | 60 | 同目标两次发送最小间隔 |
| `Account.Verification.MaxPerTargetPerHour` | int | 5 | 单目标每小时最多接收次数 |
| `Account.Verification.MaxPerIPPer10Min` | int | 20 | 单 IP 每 10 分钟最多请求次数 |

### 10.5 风控

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.Risk.MaxLoginFailuresPerUser` | int | – | 单用户登录失败上限（触发锁定） |
| `Account.Risk.MaxLoginFailuresPerIP` | int | – | 单 IP 登录失败上限 |
| `Account.Risk.FailureWindow` | int (分钟) | – | 失败计数窗口 |
| `Account.Risk.LockoutDuration` | int (分钟) | – | 用户锁定时长 |
| `Account.Risk.BlockDuration` | int (分钟) | – | IP 封禁时长 |
| `Account.Risk.EnableIPReputation` | bool | false | 是否启用 IP 信誉库（外部接入） |

### 10.6 身份策略

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.Policy.RequireVerifiedPhone` | bool | false | 登录/敏感操作要求手机已验证 |
| `Account.Policy.RequireVerifiedEmail` | bool | false | 登录/敏感操作要求邮箱已验证 |

### 10.7 审计

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.Audit.AsyncWrite` | bool | true | 异步写审计日志 |
| `Account.Audit.AsyncBufferSize` | int | – | 异步缓冲区大小 |
| `Account.Audit.RetentionDays` | int | – | 在线审计保留天数 |
| `Account.Audit.ArchiveRetentionDays` | int | – | 归档审计保留天数 |

### 10.8 Passkey / WebAuthn

| 配置键 | 类型 | 默认值 | 作用 |
|--------|------|--------|------|
| `Account.Passkey.RPDisplayName` | string | Gaia Account | RP 展示名（用户在系统提示中看到） |
| `Account.Passkey.RPID` | string | localhost | RP ID，必须是站点的 eTLD+1（域名） |
| `Account.Passkey.RPOrigin` | string | http://localhost:8080 | RP Origin，必须与浏览器访问地址一致 |
| `Account.Passkey.Timeout` | int (毫秒) | 60000 | 认证/注册超时 |

### 10.9 OAuth Provider

| Provider | 端点配置键 | 凭据配置键（统一） |
|----------|-----------|-------------------|
| GitHub | `Account.OAuth.GitHub.TokenURL` / `.UserURL` / `.EmailURL` | `Account.OAuth.GitHub.ClientID` / `.ClientSecret` / `.RedirectURL` / `.Scope` |
| Google | `Account.OAuth.Google.TokenURL` / `.UserURL` / `.JWKSURL` | 同上（`.Google.*`） |
| WeChat | `Account.OAuth.WeChat.TokenURL` / `.UserURL` | 同上（`.WeChat.*`） |
| QQ | `Account.OAuth.QQ.TokenURL` / `.OpenIDURL` / `.UserURL` | 同上（`.QQ.*`） |

端点 URL 通常无需覆盖默认值；只有走自建网关或代理时才需要配置。

---

## 十一、完整 YAML 示例

以下示例覆盖所有已知配置键，按命名空间分组。生产环境请按需删减并妥善保管敏感字段。

```yaml
# ===== Gaia 框架核心 =====
Debug: false
Environment: production
SystemCnName: 用户中心
SystemEnName: user-center

Gaia:
  ProbeTimeout: 5

# ===== 数据库 / ORM =====
Framework:
  Mysql: "user:pass@tcp(127.0.0.1:3306)/userdb?parseTime=true&loc=Local&charset=utf8mb4"
  Postgresql: "host=127.0.0.1 user=postgres password=pass dbname=userdb port=5432 sslmode=disable"

  # ===== ES（远程日志 / 业务直连） =====
  ES:
    Address:
      - "http://127.0.0.1:9200"
    UserName: elastic
    Password: "changeme"

  # ===== Kafka（远程日志写入） =====
  Kafka:
    Brokers: "127.0.0.1:9092,127.0.0.1:9093"

  # ===== Redis =====
  Redis:
    Address: "127.0.0.1:6379"
    UserName: ""
    Password: ""

  # ===== ClickHouse =====
  ClickHouse:
    Addrs:
      - "127.0.0.1:9000"
    Database: default
    Username: default
    Password: ""

  # ===== InfluxDB =====
  InfluxDB:
    URL: "http://127.0.0.1:8086"
    Token: "your-influx-token"
    Org: "my-org"
    Bucket: "metrics"

  # ===== RabbitMQ =====
  RabbitMQ:
    URL: "amqp://guest:guest@127.0.0.1:5672/"

  # ===== 对象存储 =====
  OSS:
    Endpoint: "oss-cn-hangzhou.aliyuncs.com"
    Bucket: "my-bucket"
    AccessKeyID: "LTAI***"
    AccessKeySecret: "***"
    UseInternalURL: false
  Cos:
    SecretID: "AKID***"
    SecretKey: "***"
    Region: "ap-guangzhou"
    Bucket: "my-bucket-12345"
  S3:
    Region: "us-east-1"
    Bucket: "my-bucket"
    AccessKeyID: "AKIA***"
    SecretAccessKey: "***"
    Endpoint: ""
    UsePathStyle: false

  # ===== Tracer =====
  Tracer:
    Enabled: true
    Exporter: otlphttp
    Endpoint: "http://127.0.0.1:4318"
    URLPath: "/v1/traces"
    Insecure: true
    ServiceName: user-center
    SampleRate: 1.0
    BatchTimeoutSec: 1
    MaxQueueSize: 1024
    MaxExportBatchSize: 512
    Headers:
      Authorization: "Bearer xxx"

  # ===== Metrics =====
  Metrics:
    Enabled: true
    Backend: prometheus
    ServiceName: user-center
    Prometheus:
      ListenAddr: ":9100"
      Path: "/metrics"
    OTLP:
      Endpoint: "127.0.0.1:4317"
      Insecure: true
      PushIntervalSec: 10

Gorm:
  LocalLevel: warn        # 本地输出最低级别：silent/error/warn/info/trace/debug
  RemoteLevel: warn       # 远程 ES 推送最低级别
  SlowThreshold: 200      # 慢 SQL 阈值（毫秒）

# ===== HTTP Server =====
Server:
  Port: ":8080"
  EnableTLS: false
  Cors:
    Enable: true
    AllowOrigins: ["*"]
    AllowMethods: ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    AllowHeaders: ["Content-Type", "Authorization"]
    AllowFiles: false
    AllowCredentials: true
    AllowWebSockets: true
    MaxAge: 24
  Pprof:
    Enable: false
  Timeout:
    Enable: true
    Seconds: 30
    SlowThresholdSeconds: 3
  RateLimit:
    Enable: false
    Capacity: 1000
    Rate: 200.0
    IdleTTL: 300
  CircuitBreaker:
    Enable: false
  Gzip:
    Enable: true
    MinLength: 1024
    Level: 5
  Probe:
    ReadyTimeoutSec: 5
  Metrics:
    ExposeOnMainPort: false
    Path: "/metrics"
  Security:
    Enable: true
    HSTS: "max-age=31536000; includeSubDomains"
    CSP: "default-src 'self'"
    PermissionsPolicy: "geolocation=(), microphone=()"
  Logger:
    MaxBodyBytes: 4096

Auth:
  AllowedTimeWindow: 300

HttpClient:
  LogBody: false

# ===== RPC 服务端 =====
RpcServer:
  Host: "0.0.0.0"
  Port: "9090"
  TLS:
    Enable: false
  KeepAliveTime: 30
  KeepAliveTimeout: 10
  MaxRecvMsgSize: 4
  MaxSendMsgSize: 4
  MaxConcurrentStreams: 100
  RateLimit:
    Enable: false
    Capacity: 200
    Rate: 100.0
  Auth:
    Enable: true
    HeaderKey: authorization
    Scheme: Bearer
    Tokens: "token-a,token-b"
    SkipMethods: "/grpc.health.v1.Health/Check"
  Logger:
    DetailMode: true
    PrintConsole: true
    EnablePushLog: false
    LogBody: false
    MaxBodyLogBytes: 4096
  Registry:
    Enable: true
    Type: consul
    Endpoint: "127.0.0.1:8500"
    DeregAfter: "30s"

# ===== RPC 客户端 =====
RpcClient:
  DialTimeout: 10
  LoadBalancing: round_robin
  KeepAliveTime: 30
  KeepAliveTimeout: 10
  MaxRecvMsgSize: 4
  MaxSendMsgSize: 4
  Retry:
    Enable: false
    MaxAttempts: 3
  Auth:
    Enable: false
    Token: ""
  Registry:
    Enable: true
    Type: consul
    Endpoint: "127.0.0.1:8500"
  Services:
    orderService:
      Address: "127.0.0.1:9001"
    paymentService:
      Address: "127.0.0.1:9002"

# ===== 日志 =====
Logger:
  DisableLocalFile: false
  DisableRemote: false
  RemoteWatchInterval: 10

# ===== 通知 =====
Message:
  Bark: "https://api.day.app/your-key"
  FeiShuRobot: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx"

# ===== 远程配置中心 =====
# 方案 A：Nacos
RemoteConfig:
  Type: nacos
  Nacos:
    ServerAddrs: "127.0.0.1:8848"
    Namespace: ""
    Group: DEFAULT_GROUP
    DataIds:
      - "user-center.yaml"
    Format: yaml
    LogLevel: warn
    CacheTTL: 60

# 方案 B：K8s ConfigMap（卷挂载文件 + fsnotify 监听）
# RemoteConfig:
#   Type: configmap
#   ConfigMap:
#     Path: /etc/gaia/config.yaml   # 单文件模式（subPath 挂载）
#     # Path: /etc/gaia             # 目录模式（默认 ConfigMap 卷挂载）
#     DebounceMs: 500
#     CacheTTL: 60

# ===== Jobs =====
Jobs:
  BanEnvList: ["development", "ci"]

# ===== Account 账号体系 =====
Account:
  AppID: "user-center"
  Mode: production
  DefaultTenantID: "defaultTenantID"
  PermissionCacheTTL: 10
  PrincipalCacheMaxTTL: 5
  Redis:
    KeyPrefix: "acct:"
  Standalone:
    Profile: full

  JWT:
    SecretKey: "account-jwt-secret-32bytes-min"
    Issuer: "gaia-account"
    Audience: "user-center"
    AccessTokenExp: 15
    RefreshTokenExp: 43200
    EnableAccessTokenDenylistCheck: false
    DenylistCacheTTL: 30

  Password:
    MinLength: 12
    MaxLength: 128
    Argon2Time: 3
    Argon2Memory: 65536
    Argon2Threads: 2

  Verification:
    CodeLength: 6
    CodeTTL: 5
    MaxAttempts: 5
    SendInterval: 60
    MaxPerTargetPerHour: 5
    MaxPerIPPer10Min: 20

  Risk:
    MaxLoginFailuresPerUser: 5
    MaxLoginFailuresPerIP: 20
    FailureWindow: 15
    LockoutDuration: 30
    BlockDuration: 60
    EnableIPReputation: false

  Policy:
    RequireVerifiedPhone: false
    RequireVerifiedEmail: false

  Audit:
    AsyncWrite: true
    AsyncBufferSize: 1024
    RetentionDays: 90
    ArchiveRetentionDays: 365

  Passkey:
    RPDisplayName: "User Center"
    RPID: "example.com"
    RPOrigin: "https://example.com"
    Timeout: 60000

  OAuth:
    GitHub:
      ClientID: "Iv1.xxxx"
      ClientSecret: "xxxx"
      RedirectURL: "https://example.com/oauth/github/callback"
      Scope: "read:user user:email"
    Google:
      ClientID: "xxxxx.apps.googleusercontent.com"
      ClientSecret: "xxxx"
      RedirectURL: "https://example.com/oauth/google/callback"
      Scope: "openid email profile"
    WeChat:
      ClientID: "wx-app-id"
      ClientSecret: "xxx"
      RedirectURL: "https://example.com/oauth/wechat/callback"
      Scope: "snsapi_login"
    QQ:
      ClientID: "qq-app-id"
      ClientSecret: "xxx"
      RedirectURL: "https://example.com/oauth/qq/callback"
      Scope: "get_user_info"

# ===== 多实例样例 =====
MongoBiz:
  URI: "mongodb://127.0.0.1:27017"
  Database: biz
  MaxPoolSize: 100
  MinPoolSize: 10
  ConnectTimeoutSec: 5
EtcdBiz:
  Endpoints: ["127.0.0.1:2379"]
  Prefix: "/biz/"
  Username: ""
  Password: ""
  DialTimeoutMs: 3000
  RequestTimeoutMs: 2000
MqttBiz:
  Broker: "tcp://127.0.0.1:1883"
  ClientID: "gaia-app-1"
  Username: ""
  Password: ""
```

---

## 十二、完整 JSON 示例

```json
{
  "Debug": false,
  "Environment": "production",
  "SystemCnName": "用户中心",
  "SystemEnName": "user-center",

  "Gaia": {
    "ProbeTimeout": 5
  },

  "Framework": {
    "Mysql": "user:pass@tcp(127.0.0.1:3306)/userdb?parseTime=true&loc=Local&charset=utf8mb4",
    "Postgresql": "host=127.0.0.1 user=postgres password=pass dbname=userdb port=5432 sslmode=disable",
    "ES": {
      "Address": ["http://127.0.0.1:9200"],
      "UserName": "elastic",
      "Password": "changeme"
    },
    "Kafka": {
      "Brokers": "127.0.0.1:9092,127.0.0.1:9093"
    },
    "Redis": {
      "Address": "127.0.0.1:6379",
      "UserName": "",
      "Password": ""
    },
    "ClickHouse": {
      "Addrs": ["127.0.0.1:9000"],
      "Database": "default",
      "Username": "default",
      "Password": ""
    },
    "InfluxDB": {
      "URL": "http://127.0.0.1:8086",
      "Token": "your-influx-token",
      "Org": "my-org",
      "Bucket": "metrics"
    },
    "RabbitMQ": {
      "URL": "amqp://guest:guest@127.0.0.1:5672/"
    },
    "OSS": {
      "Endpoint": "oss-cn-hangzhou.aliyuncs.com",
      "Bucket": "my-bucket",
      "AccessKeyID": "LTAI***",
      "AccessKeySecret": "***",
      "UseInternalURL": false
    },
    "Cos": {
      "SecretID": "AKID***",
      "SecretKey": "***",
      "Region": "ap-guangzhou",
      "Bucket": "my-bucket-12345"
    },
    "S3": {
      "Region": "us-east-1",
      "Bucket": "my-bucket",
      "AccessKeyID": "AKIA***",
      "SecretAccessKey": "***",
      "Endpoint": "",
      "UsePathStyle": false
    },
    "Tracer": {
      "Enabled": true,
      "Exporter": "otlphttp",
      "Endpoint": "http://127.0.0.1:4318",
      "URLPath": "/v1/traces",
      "Insecure": true,
      "ServiceName": "user-center",
      "SampleRate": 1.0,
      "BatchTimeoutSec": 1,
      "MaxQueueSize": 1024,
      "MaxExportBatchSize": 512,
      "Headers": {
        "Authorization": "Bearer xxx"
      }
    },
    "Metrics": {
      "Enabled": true,
      "Backend": "prometheus",
      "ServiceName": "user-center",
      "Prometheus": {
        "ListenAddr": ":9100",
        "Path": "/metrics"
      },
      "OTLP": {
        "Endpoint": "127.0.0.1:4317",
        "Insecure": true,
        "PushIntervalSec": 10
      }
    }
  },

  "Gorm": {
    "LocalLevel": "warn",
    "RemoteLevel": "warn",
    "SlowThreshold": 200
  },

  "Server": {
    "Port": ":8080",
    "EnableTLS": false,
    "Cors": {
      "Enable": true,
      "AllowOrigins": ["*"],
      "AllowMethods": ["GET", "POST", "PUT", "DELETE", "OPTIONS"],
      "AllowHeaders": ["Content-Type", "Authorization"],
      "AllowFiles": false,
      "AllowCredentials": true,
      "AllowWebSockets": true,
      "MaxAge": 24
    },
    "Pprof": { "Enable": false },
    "Timeout": {
      "Enable": true,
      "Seconds": 30,
      "SlowThresholdSeconds": 3
    },
    "RateLimit": {
      "Enable": false,
      "Capacity": 1000,
      "Rate": 200.0,
      "IdleTTL": 300
    },
    "CircuitBreaker": { "Enable": false },
    "Gzip": {
      "Enable": true,
      "MinLength": 1024,
      "Level": 5
    },
    "Probe": { "ReadyTimeoutSec": 5 },
    "Metrics": {
      "ExposeOnMainPort": false,
      "Path": "/metrics"
    },
    "Security": {
      "Enable": true,
      "HSTS": "max-age=31536000; includeSubDomains",
      "CSP": "default-src 'self'",
      "PermissionsPolicy": "geolocation=(), microphone=()"
    },
    "Logger": { "MaxBodyBytes": 4096 }
  },

  "Auth": { "AllowedTimeWindow": 300 },
  "HttpClient": { "LogBody": false },

  "RpcServer": {
    "Host": "0.0.0.0",
    "Port": "9090",
    "TLS": { "Enable": false },
    "KeepAliveTime": 30,
    "KeepAliveTimeout": 10,
    "MaxRecvMsgSize": 4,
    "MaxSendMsgSize": 4,
    "MaxConcurrentStreams": 100,
    "RateLimit": {
      "Enable": false,
      "Capacity": 200,
      "Rate": 100.0
    },
    "Auth": {
      "Enable": true,
      "HeaderKey": "authorization",
      "Scheme": "Bearer",
      "Tokens": "token-a,token-b",
      "SkipMethods": "/grpc.health.v1.Health/Check"
    },
    "Logger": {
      "DetailMode": true,
      "PrintConsole": true,
      "EnablePushLog": false,
      "LogBody": false,
      "MaxBodyLogBytes": 4096
    },
    "Registry": {
      "Enable": true,
      "Type": "consul",
      "Endpoint": "127.0.0.1:8500",
      "DeregAfter": "30s"
    }
  },

  "RpcClient": {
    "DialTimeout": 10,
    "LoadBalancing": "round_robin",
    "KeepAliveTime": 30,
    "KeepAliveTimeout": 10,
    "MaxRecvMsgSize": 4,
    "MaxSendMsgSize": 4,
    "Retry": {
      "Enable": false,
      "MaxAttempts": 3
    },
    "Auth": {
      "Enable": false,
      "Token": ""
    },
    "Registry": {
      "Enable": true,
      "Type": "consul",
      "Endpoint": "127.0.0.1:8500"
    },
    "Services": {
      "orderService": { "Address": "127.0.0.1:9001" },
      "paymentService": { "Address": "127.0.0.1:9002" }
    }
  },

  "Logger": {
    "DisableLocalFile": false,
    "DisableRemote": false,
    "RemoteWatchInterval": 10
  },

  "Message": {
    "Bark": "https://api.day.app/your-key",
    "FeiShuRobot": "https://open.feishu.cn/open-apis/bot/v2/hook/xxx"
  },

  "RemoteConfig": {
    "Type": "nacos",
    "Nacos": {
      "ServerAddrs": "127.0.0.1:8848",
      "Namespace": "",
      "Group": "DEFAULT_GROUP",
      "DataIds": ["user-center.yaml"],
      "Format": "yaml",
      "LogLevel": "warn",
      "CacheTTL": 60
    }
  },

  "Jobs": {
    "BanEnvList": ["development", "ci"]
  },

  "Account": {
    "AppID": "user-center",
    "Mode": "production",
    "DefaultTenantID": "defaultTenantID",
    "PermissionCacheTTL": 10,
    "PrincipalCacheMaxTTL": 5,
    "Redis": { "KeyPrefix": "acct:" },
    "Standalone": { "Profile": "full" },

    "JWT": {
      "SecretKey": "account-jwt-secret-32bytes-min",
      "Issuer": "gaia-account",
      "Audience": "user-center",
      "AccessTokenExp": 15,
      "RefreshTokenExp": 43200,
      "EnableAccessTokenDenylistCheck": false,
      "DenylistCacheTTL": 30
    },

    "Password": {
      "MinLength": 12,
      "MaxLength": 128,
      "Argon2Time": 3,
      "Argon2Memory": 65536,
      "Argon2Threads": 2
    },

    "Verification": {
      "CodeLength": 6,
      "CodeTTL": 5,
      "MaxAttempts": 5,
      "SendInterval": 60,
      "MaxPerTargetPerHour": 5,
      "MaxPerIPPer10Min": 20
    },

    "Risk": {
      "MaxLoginFailuresPerUser": 5,
      "MaxLoginFailuresPerIP": 20,
      "FailureWindow": 15,
      "LockoutDuration": 30,
      "BlockDuration": 60,
      "EnableIPReputation": false
    },

    "Policy": {
      "RequireVerifiedPhone": false,
      "RequireVerifiedEmail": false
    },

    "Audit": {
      "AsyncWrite": true,
      "AsyncBufferSize": 1024,
      "RetentionDays": 90,
      "ArchiveRetentionDays": 365
    },

    "Passkey": {
      "RPDisplayName": "User Center",
      "RPID": "example.com",
      "RPOrigin": "https://example.com",
      "Timeout": 60000
    },

    "OAuth": {
      "GitHub": {
        "ClientID": "Iv1.xxxx",
        "ClientSecret": "xxxx",
        "RedirectURL": "https://example.com/oauth/github/callback",
        "Scope": "read:user user:email"
      },
      "Google": {
        "ClientID": "xxxxx.apps.googleusercontent.com",
        "ClientSecret": "xxxx",
        "RedirectURL": "https://example.com/oauth/google/callback",
        "Scope": "openid email profile"
      },
      "WeChat": {
        "ClientID": "wx-app-id",
        "ClientSecret": "xxx",
        "RedirectURL": "https://example.com/oauth/wechat/callback",
        "Scope": "snsapi_login"
      },
      "QQ": {
        "ClientID": "qq-app-id",
        "ClientSecret": "xxx",
        "RedirectURL": "https://example.com/oauth/qq/callback",
        "Scope": "get_user_info"
      }
    }
  },

  "MongoBiz": {
    "URI": "mongodb://127.0.0.1:27017",
    "Database": "biz",
    "MaxPoolSize": 100,
    "MinPoolSize": 10,
    "ConnectTimeoutSec": 5
  },
  "EtcdBiz": {
    "Endpoints": ["127.0.0.1:2379"],
    "Prefix": "/biz/",
    "Username": "",
    "Password": "",
    "DialTimeoutMs": 3000,
    "RequestTimeoutMs": 2000
  },
  "MqttBiz": {
    "Broker": "tcp://127.0.0.1:1883",
    "ClientID": "gaia-app-1",
    "Username": "",
    "Password": ""
  }
}
```

---

## 备注

1. **键名大小写**：Gaia 配置库基于 Viper，键名大小写不敏感，但建议统一使用文档中的 PascalCase 风格以便代码内查找。
2. **多实例 / 自定义 schema**：所有标注 `{schema}` 的组件都可以在配置中放在任意顶层路径下，初始化时通过 `NewXxxWithSchema("YourPath")` 引入；本文档示例中的 `MongoBiz` / `EtcdBiz` / `MqttBiz` 即为这种用法。
3. **回退链**：`Account.JWT.*` 在未设置时回退到 `JWT.*`；`Tracer.ServiceName` / `Metrics.ServiceName` 未设置时回退到 `SystemEnName`。
4. **远程配置**：配置读取优先级为环境变量 > 本地配置文件 > 远程配置中心 > 本地远程快照；本地同名 key 会覆盖远端。Nacos 会把完整远端配置持久化为本地快照，其他远端中心按已请求 key 累积写入快照。
5. **敏感字段**：`*.Password` / `*.SecretKey` / `*.AccessKeySecret` / `OAuth.*.ClientSecret` 等请放在 secret manager 或加密配置中，不要直接提交版本库。
