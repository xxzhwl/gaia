# 后端服务接入 `framework/account` 指南

本文面向 **上游 Go 业务服务**（基于 `gaia` 框架）的开发者，说明如何把 `gaia/framework/account` 集成到你自己的服务中。

`framework/account` 提供两种集成方式：

| 模式 | 适用场景 | 启动方式 |
|---|---|---|
| **A. 独立服务（Standalone）** | 把账号服务作为独立微服务部署，业务通过 HTTP/JWT 远程调用 | `account.NewStandaloneService()` 内置 REST API |
| **B. 嵌入式（SDK）** | 单体服务，账号能力直接和业务跑在同一个进程内 | `account.New(cfg)` 拿到 `*Manager`，直接调方法 |

绝大多数项目推荐模式 A：账号是公共能力，独立部署可以方便地多业务复用 + 集中升级。模式 B 适合中小型应用或需要扩展底层逻辑的场景。

---

## 1. 模式 A：独立账号服务

### 1.1 启动一个独立服务

```go
package main

import (
    "context"
    "log"

    "github.com/xxzhwl/gaia"
    "github.com/xxzhwl/gaia/framework/account"
)

func main() {
    // 1) 初始化 gaia 框架（按你们项目惯例加载配置文件 / KV 配置）
    if err := gaia.InitFramework(); err != nil {
        log.Fatal(err)
    }

    // 2) 用默认框架配置（自动读取 Account.* 配置 + 默认 MySQL/Redis）
    svc, err := account.NewStandaloneService()
    if err != nil {
        log.Fatal(err)
    }

    // 3) 阻塞运行，自动注册路由 + 跑后台任务
    if err := svc.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

启动后服务监听 `:8080`，所有路由都挂在 `/api/v1/account/*`，会自动跑：

- `Bootstrap`：自动建表（migrate）
- 清理任务（过期 token / session / 验证码 / 审计归档）
- Outbox / RiskService / KeyRotation / AuditFlush 等后台 worker

### 1.2 自定义配置

如果需要自定义端口、TTL、风控等：

```go
cfg := account.FrameworkConfig(db, redisClient)        // 从 gaia 配置构建
cfg.JWT.SecretKey = "<至少32字节>"
cfg.AccessTokenTTL = 15 * time.Minute
cfg.RefreshTokenTTL = 30 * 24 * time.Hour
cfg.AccountPolicy.RequireVerifiedPhone = true
cfg.OAuthProviders = map[string]account.OAuthProvider{
    "github": account.NewGitHubProvider(clientID, clientSecret),
    "wechat": account.NewWeChatProvider(appID, appSecret),
}
cfg.NotifyProvider = myNotifier{} // 实现 Send(channel, target, code) error

svc, err := account.NewStandaloneServiceWithConfig(cfg, account.StandaloneConfig{
    ListenAddr:      "9000",
    CleanupInterval: 5 * time.Minute,
})
```

### 1.3 关键配置项（`Account.*` 配置键）

| 配置键 | 默认 | 说明 |
|---|---|---|
| `Account.JWT.SecretKey` | 必填 | 生产环境必须 ≥32 字节，建议用非对称 KeySet |
| `Account.JWT.AccessTokenExp` | 15（分钟） | Access Token 有效期 |
| `Account.JWT.RefreshTokenExp` | 43200（30天） | Refresh Token 有效期 |
| `Account.Password.MinLength` | 12 | 密码最小长度 |
| `Account.DefaultTenantID` | `default` | 单租户场景使用此固定 tenant |
| `Account.Policy.RequireVerifiedPhone` | false | 注册必须绑定已验证手机 |
| `Account.Risk.MaxLoginFailuresPerUser` | 10 | 用户级失败上限 |
| `Account.Audit.RetentionDays` | 90 | 审计日志保留天数 |
| `Account.Audit.ArchiveRetentionDays` | 0 | 归档天数（0=不归档） |

### 1.4 业务服务作为「资源服务器」校验 Token

业务服务（A 模式下账号服务的下游）只需要持有同一份 `JWT.SecretKey`（或公钥）就能本地校验 Bearer Token，**不必每次远程调用账号服务**：

```go
// 业务服务启动时，初始化只读 Manager（仅做 Token 校验）
mgr, err := account.NewFramework() // 复用同一份 Account.* 配置
if err != nil { log.Fatal(err) }

// 业务服务的 Hertz/RPC 中间件
func authMiddleware(mgr *account.Manager) app.HandlerFunc {
    return mgr.Middleware().Authenticate()
}

// 路由示例
h := server.NewApp()
h.Use(authMiddleware(mgr))
h.GET("/api/v1/biz/orders", listOrdersHandler)
```

在业务 handler 中拿到登录用户：

```go
func listOrdersHandler(c *app.RequestContext) {
    p := account.GetPrincipal(server.WrapRequest(c)) // *account.Principal
    userID, tenantID := p.UserID, p.TenantID
    // ...
}
```

中间件家族：

| 方法 | 作用 |
|---|---|
| `Authenticate()` | 强校验 Bearer Token，失败 401 |
| `OptionalAuthenticate()` | 有 Token 就解析，没有不报错（公开页可选个性化） |
| `RequirePermission("order.read")` | 必须含某权限码，失败 403 |
| `RequireAnyPermission("a","b")` | 满足任一即可 |
| `RequireAllPermissions("a","b")` | 必须全部满足 |
| `RequireRole("tenant_admin")` | 必须含某角色 |
| `RequireAnyRole("admin","ops")` | 含任一角色 |

> ⚠️ 业务服务**不要重复跑 `Bootstrap`**，那只属于账号服务。业务侧只需要 `New(cfg)` 拿到 Manager 用中间件即可。

---

## 2. 模式 B：嵌入式 SDK 调用

如果你的服务需要**自定义业务逻辑**（比如登录后送积分、注册时灌默认数据），推荐直接调用 SDK 方法而不是远程 HTTP：

```go
mgr, err := account.New(cfg)
if err != nil { log.Fatal(err) }
if err := mgr.Bootstrap(ctx); err != nil { log.Fatal(err) }
defer mgr.Close()

// 自定义注册：在 SDK 注册成功后，给新用户灌业务数据
result, err := mgr.Auth().Register(ctx, account.RegisterRequest{
    TenantID:  "default",
    Username:  "alice",
    Password:  "Pa$$w0rd-12345",
    Email:     "alice@example.com",
})
if err != nil { return err }

// 业务侧扩展
go grantNewUserCoupon(result.User.ID)
go createBizProfile(result.User.ID)
```

### 2.1 主要服务入口

`Manager` 提供以下子服务：

```go
mgr.Auth()           // *AuthService          登录/注册/刷新/登出/MFA/Step-up
mgr.Users()          // *UserService          用户基本资料/改密/绑定邮箱手机/重置密码
mgr.Sessions()       // *SessionService       会话查询/吊销
mgr.Verification()   // *VerificationService  验证码 send/verify
mgr.OAuth()          // *OAuthService         第三方登录/绑定/解绑
mgr.IdP()            // *IdpService           OIDC IdP（authorize/token/jwks）
mgr.Roles()          // *RoleService          角色及成员
mgr.Permissions()    // *PermissionService    权限 CRUD
mgr.Authorizer()     // *Authorizer           权限求值（含 Policy/ABAC）
mgr.Organizations()  // *OrgService           组织树
mgr.Audit()          // *AuditService         审计查询
mgr.Admin()          // *AdminService         管理端：用户/角色/会话强制吊销
mgr.Middleware()     // *Middleware           Hertz 中间件
```

### 2.2 常见调用模板

```go
// 登录
loginResp, err := mgr.Auth().Login(ctx, account.LoginRequest{
    TenantID:       "default",
    Identifier:     "alice",          // 用户名 / email / phone 之一
    IdentifierType: "auto",           // 或 username / email / phone
    Password:       "Pa$$w0rd-12345",
    DeviceID:       "device-uuid",
    IP:             "1.2.3.4",
    UserAgent:      ua,
})
// 如果 loginResp.MFARequired = true，需要再调 mgr.Auth().CompleteMFA(...)

// 校验 Token（无需走 HTTP）
p, err := mgr.Auth().Validate(ctx, accessToken)

// 检查权限（编程式）
ok, err := mgr.Authorizer().HasPermission(ctx, p, "order.write")
```

### 2.3 自定义短信/邮件通道

实现 `NotifyProvider` 接口：

```go
type myNotifier struct{}

func (n myNotifier) Send(ctx context.Context, channel, target, code string) error {
    // channel = "email" | "sms"
    return mySMSProvider.Send(target, fmt.Sprintf("您的验证码是 %s", code))
}

cfg.NotifyProvider = myNotifier{}
```

### 2.4 事件订阅（异步扩展）

```go
cfg.EventSubscribers = map[string]account.EventSubscriber{
    "biz-on-register": func(ctx context.Context, ev account.Event) {
        if ev.Type == account.EventUserRegistered {
            grantNewUserCoupon(ev.UserID)
        }
    },
}
```

支持事件：`user.registered` / `user.login` / `user.logout` / `mfa.enabled` / `password.changed` / `oauth.bound` / 等。

---

## 3. 多租户设计与最佳实践

`framework/account` 全套支持多租户。一句话定位：**所有数据按 `tenant_id` 物理隔离 + 配置/策略可按租户差异化 + 单租户场景透明无感**。

### 3.1 它解决了什么

所有核心表都带 `tenant_id` 列与索引（`models.go`）：

| 表 | 唯一索引 | 隔离效果 |
|---|---|---|
| `users` | `(tenant_id, username)` / `(tenant_id, email)` / `(tenant_id, phone)` | 同一邮箱/手机号**可在不同租户重复**注册，互不感知 |
| `credentials` | `(tenant_id, identifier)` | 凭证按租户独立 |
| `sessions` / `refresh_tokens` | 都带 `tenant_id` 索引 | 会话查询天然按租户过滤 |
| `roles` | `(tenant_id, code)` | 每租户可有自己语义的 `admin / editor` |
| `permissions` | `(tenant_id, code)` | 权限码按租户独立，互不污染 |
| `user_roles` | `(tenant_id, user_id, role_id)` | 同一用户在不同租户可有不同角色 |
| `audit_logs` | `(tenant_id, created_at)` | 审计按租户分区查询/导出 |

> 所以你可以一套部署服务 N 个客户/团队/工作区，无需为每个客户拉一套库；SaaS 场景下 `acme.com` 与 `globex.com` 的员工可以用同一邮箱注册，互不干扰。

### 3.2 单租户场景：默认透明

```go
// manager.go
func (m *Manager) tenantID(tenantID string) string {
    if tenantID == "" {
        return m.cfg.DefaultTenantID  // 配置 Account.DefaultTenantID，默认 "default"
    }
    return tenantID
}
```

不传 `TenantID` → 自动落到 `default`。**单租户业务可以完全无视租户字段**，但底层隔离能力随时可启用，无需迁移。

### 3.3 多租户场景：业务侧约定

```go
// 注册/登录显式带 tenant_id（来自请求 Header / 子域名 / 路径前缀均可）
result, err := mgr.Auth().Login(ctx, account.LoginRequest{
    TenantID:   c.GetHeader("X-Tenant-ID"),
    Identifier: req.Identifier,
    Password:   req.Password,
    ...
})

// 后续业务接口从 Principal 拿租户上下文
p := account.GetPrincipal(server.WrapRequest(c))
db.Where("tenant_id = ?", p.TenantID).Find(&orders)  // 业务表也按租户过滤
```

下发的 JWT 里自带 `tenant_id`，下游业务无需额外查表：

```go
// auth.go: signAccessToken
claims := accessClaims{
    UserID:    user.ID,
    TenantID:  user.TenantID,  // ← 直接进 JWT
    SessionID: sessionID,
    ...
}
```

### 3.4 `TenantValidator`：租户级别的"封号/欠费/到期"开关

每次 token 校验时都会调用（`auth.go`）：

```go
cfg.TenantValidator = func(ctx context.Context, tenantID string) error {
    info, err := saasPortal.GetTenant(ctx, tenantID)
    if err != nil { return err }
    if info.Status == "suspended" {
        return errwrap.Newf(account.ErrPermissionDenied, "租户已停用")
    }
    if info.ExpireAt.Before(time.Now()) {
        return errwrap.Newf(account.ErrPermissionDenied, "租户已到期")
    }
    return nil
}
```

效果：

- 租户欠费 / 试用到期 → 立刻让该租户**所有用户的 token 全部失效**，无需逐个踢会话。
- 租户被封禁 → 整个租户瞬间下线。
- 配合 `RolesVersion` / `AuthVersion` 可做租户级强制重新登录。

### 3.5 角色与权限的"租户自治"

- 平台预置角色/权限种子由 `Manager.Bootstrap` 写入 `tenant_id = 'default'`（`is_system=1`）。
- 租户管理员可以在自己 `tenant_id` 下建 `editor / reviewer / hr-admin`，互不冲突。
- 同名 `admin` 在 A、B 两租户可绑定完全不同的权限集。

### 3.6 第三方应用接入也按租户隔离

`s.m.IdP().ListClients(ctx, tenantID)` 严格按租户过滤；A 租户的"工单系统"和 B 租户的"BI 系统"互不可见——这套账号中心**自身就能当 SaaS 平台的统一登录中心**。

### 3.7 跨租户禁忌清单（业务方易错点）

| 禁忌 | 正确做法 |
|---|---|
| 用 `WHERE user_id = ?` 直接查 | 永远 `WHERE tenant_id = ? AND user_id = ?` |
| 业务表只存 `user_id` 不存 `tenant_id` | 业务表也带 `tenant_id`，避免越租访问导致数据泄漏 |
| 在 token 里只信任 `user_id` | 同时把 `Principal.TenantID` 作为隔离字段一起用 |
| 跨租户共享 Redis Key 不带前缀 | 业务缓存 Key 带 `tenant:<id>:` 前缀 |
| 后台运营接口忘记校验调用方租户 | 在 `RequireRole("platform_admin")` 之外，禁止跨租户更新 |

### 3.8 租户 vs 组织（Organization）的边界

- **租户（Tenant）**：用于隔离**客户/工作区/BU**，是物理隔离层（数据集互不可见）。
- **组织（Organization，`org.go`）**：用于在**同一租户内**建模"部门/小组"的树状结构。

不要把这两层混在一起。例：「腾讯」是一个租户，"腾讯/PCG/QQ"是租户内的组织树。

---

## 4. 会话并发策略 / 多端登录控制

### 4.1 默认行为：多端共存，互不干扰

每次登录都会**新建一条独立的 `sessions` 行**，旧端不会被自动顶掉：

```go
// auth.go: issueTokens（密码登录、注册、验证码登录、OAuth 都走它）
if sessionID == "" {
    sessionID = newID()
}
session := Session{
    ID:            sessionID,
    DeviceID:      deviceID,
    IP:            ip,
    UserAgentHash: hashUserAgent(userAgent),
    Status:        SessionActive,
    ExpiresAt:     sessionExpiresAt,
}
tx.Create(&session)
```

JWT 里的 `sid` 区分每个端，鉴权按 `sid` 独立校验：A 端被踢只让 A 的 token 失效，B 端不受影响。

`config.go` 里**没有**任何并发会话限制开关（grep 不到 `MaxSessions / SingleDevice / KickOther`）。框架不内置策略，**完全交给业务方决定**。

### 4.2 框架提供的会话操作能力

`mgr.Sessions()` 暴露：

| 方法 | 用途 |
|---|---|
| `List(ctx, tenantID, userID, currentSessionID)` | 列出该用户所有 active 会话；标记当前端 |
| `Revoke(ctx, tenantID, userID, sessionID)` | 用户主动踢下某一端（自校验归属） |
| `RevokeOther(ctx, tenantID, userID, currentSessionID)` | 一键踢下当前端以外的所有端 |
| `RevokeByID(ctx, tenantID, sessionID)` | 管理员强制下线（不校验 userID，配合 `Admin()`） |

每次 Revoke 都会：① 把 `sessions.status` 置为 `revoked`；② 把对应 `RefreshToken` 置为 `revoked`；③ 调用 `auth.invalidatePrincipalCache(sid)` 跨实例立刻生效（走 Redis）。

### 4.3 策略 A：单端登录（强一致 / 银行类强安全）

登录成功后追加一行：

```go
result, err := mgr.Auth().Login(ctx, req)
if err != nil { return err }
// 单端登录：把刚才创建的 session 之外的所有 active 会话踢掉
_, _ = mgr.Sessions().RevokeOther(ctx, req.TenantID, result.User.ID, result.SessionID)
return result
```

> 当前 `AuthResult` 直接返回的字段中包含 access token，可在业务封装层从 token 解出 `sid`，或在你的 LoginHandler 包一层把 session.id 取出后调用。如需要，可向 `AuthResult` 追加 `SessionID string` 字段（侵入式改动需评估）。

### 4.4 策略 B：限制最大并发会话数 N（柔性）

```go
sessions, _ := mgr.Sessions().List(ctx, req.TenantID, userID, "")
if len(sessions) >= maxN {
    sort.Slice(sessions, func(i, j int) bool {
        return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
    })
    // 踢掉最早登录的那条，腾出名额
    _ = mgr.Sessions().Revoke(ctx, req.TenantID, userID, sessions[0].ID)
}
result, _ := mgr.Auth().Login(ctx, req)
```

### 4.5 策略 C：按设备类型分组（移动 1 端 + Web 1 端）

前端按设备指纹生成稳定的 `DeviceID` 并随登录请求上送，登录前查同类设备并踢旧的：

```go
var rows []account.Session
db.Where("tenant_id = ? AND user_id = ? AND status = 'active' AND device_id LIKE ?",
    tenantID, userID, devicePrefix+"%").Find(&rows)
for _, s := range rows {
    _ = mgr.Sessions().RevokeByID(ctx, tenantID, s.ID)
}
result, _ := mgr.Auth().Login(ctx, req)
```

> 推荐做法：在 `DeviceID` 里编码设备类型前缀，例如 `web-xxxxx` / `ios-xxxxx` / `android-xxxxx`；或扩展业务侧的设备登记表。

### 4.6 用户自助"我的设备"页面

直接复用 `Sessions().List` + `Sessions().Revoke`：

```go
// GET /me/sessions
sessions, _ := mgr.Sessions().List(ctx, p.TenantID, p.UserID, p.SessionID)
// 返回前端，前端展示 [设备名/IP/上次活跃时间/是否当前端]

// POST /me/sessions/{id}/revoke
_ = mgr.Sessions().Revoke(ctx, p.TenantID, p.UserID, sid)

// POST /me/sessions/revoke-others
n, _ := mgr.Sessions().RevokeOther(ctx, p.TenantID, p.UserID, p.SessionID)
```

`standalone.go` 已经把上面三个能力暴露成 REST 接口（`/api/v1/account/sessions`、`DELETE /sessions/{id}`、`POST /sessions/revoke-others`），无需重复实现。

### 4.7 风险信号自动下线（推荐启用）

`risk.go` 在以下信号触发时会把会话状态置为 `risk_blocked`：

- 同一 `user_id` 多 IP 高速切换
- 异地登录 / 不可信设备
- 短时密码错误次数超限

业务侧通过 `Account.Risk.*` 调阈值即可，无需改代码。

---

## 5. 调用链全景图（单租户场景）

> 本节以 `DefaultTenantID = "default"` 的"零感知多租户"模式展开。所有调用 `m.tenantID("")` 自动注入 `default`，业务方无需关心租户字段。每条命令都遵循同一骨架：开 OTel span → tenantID 透明 → `db.Transaction` → `emitOutbox` 业务事件 → `audit` 落审计 → `metrics` 打点 → 失败回写风控/审计。

### 5.1 总体分层

```
HTTP/REST (standalone.go)              ← 模式 A：独立账号服务暴露的 HTTP 入口
       │                                 模式 B：业务直接调用 SDK，跳过这一层
       ▼
Manager.Auth() / Users() / Sessions() / Verification() / IdP() ...   (manager.go)
       │
       ▼
service 层 (auth.go / user.go / session.go / verification.go / risk.go ...)
       │
       ▼
─────────────────────────────────────────────────────────────────
GORM 事务 (Transaction)              Redis 缓存 (Principal/限流/锁)
acct_users / acct_credentials        acct_audit_logs (异步 channel 写入)
acct_sessions / acct_refresh_tokens  acct_outbox → 业务消费 (账号事件)
acct_roles / acct_permissions        OTel Tracer / Metrics
acct_oauth_accounts / acct_mfa_*     account.user.* / .password_changed 等 topic
─────────────────────────────────────────────────────────────────
```

### 5.2 注册（POST `/auth/register`）

入口：`standalone.handleRegister` (`standalone.go:209`) → `Auth().Register()` (`auth.go:123`)

```
HTTP POST /api/v1/account/auth/register
  └─ handleRegister
       └─ Auth().Register(ctx, req)
            ├─ tenantID = m.tenantID("") = "default"
            ├─ validateIdentifier / validatePassword（policy.go 强度规则）
            └─ db.Transaction:
                ├─ Count(users) WHERE tenant_id+username/email/phone   ← 唯一性
                ├─ hashPassword(argon2id)
                ├─ [可选] verification.verifyTx(ChallengeID,Code)       ← 邮箱/短信验证码
                ├─ INSERT acct_users (auth_version=1, roles_version=1)
                ├─ INSERT acct_credentials (type=password)
                ├─ assignDefaultRole → INSERT acct_user_roles (role=user)
                ├─ emitOutbox("account.user.registered")
                └─ issueTokens(user, roles, "", "", "")                 ← §5.8
       ← 成功：AuthResult{User, AccessToken, RefreshToken}
       ← 失败：audit("register","failed") + metrics.RegisterTotal
```

要点：

- 手机/邮箱可选；当 `AccountPolicy.RequireVerifiedPhone=true` 且未通过短信验证 → `User.status=pending`，**不签发 token**，只返回 `PhoneBindingRequired=true`，前端引导走 §5.3 中"绑定手机并登录"。
- 注册成功默认即登录（写一条 `sessions` 行），前端拿到的是已登录态。

### 5.3 密码登录（POST `/auth/login`）

入口：`handleLogin` → `Auth().Login()` (`auth.go:285`)

```
HTTP POST /api/v1/account/auth/login
  └─ Auth().Login(ctx, req)
       └─ db.Transaction:
            ├─ SELECT acct_users WHERE tenant_id + (username|email|phone)
            ├─ 状态闸门：disabled/deleted → 401；locked 已过期则解锁，未过期 → 401
            ├─ Risk.Assess(tenantID, userID, ip)         ← risk.go (Redis 计数)
            │     decision ∈ allow / challenge / block / lock / delay
            ├─ SELECT acct_credentials FOR UPDATE
            ├─ verifyPassword(argon2id) → 失败：tx 外 RecordFailure + audit
            ├─ [自愈] needsPasswordUpgrade → 用新参数重新 hash 入库
            ├─ phoneBindingRequired(user) → 返回 PhoneBindingRequired=true（不签 token）
            ├─ MFA 闸门：
            │   - HasTOTP → CreateMFAChallenge → 返回 MFARequired=true
            │   - risk=challenge && 有邮箱/手机 → Verification.Send + MFAChallenge
            │   - risk=challenge && 无可用通道 → 401
            ├─ loadRoleCodes(user) → roles[]
            ├─ Admin MFA 强校验（RequireAdminMFA && 有 admin 角色 && 无 TOTP → 401）
            ├─ issueTokens(user, roles, ...)             ← §5.8
            ├─ emitOutbox("account.user.logged_in", method=password)
            └─ UPDATE last_login_at / last_login_ip
       ├─ Risk.RecordSuccess / RecordFailure
       ├─ audit("login", success|failed) + metrics.LoginTotal/LoginDuration
       └─ Authorizer.GetEffectivePermissions(userID)     ← 异步暖缓存
```

> 仅当 `MFARequired=false && PhoneBindingRequired=false` 时才落 success 审计；MFA 完成由 `CompleteMFA` 补审计，避免双计。

### 5.4 验证码登录 / OAuth 登录

```
（前置）POST /verifications/send → Verification.Send 写一行 acct_verification_challenges
                                  → outbox 投递发送任务（短信/邮件 worker 拉取）

POST /auth/login/code  → Auth().LoginWithVerificationCode  (auth.go:632)
  └─ verification.verifyTx → SELECT/UPSERT user → MFA/PhoneBinding 闸门 → issueTokens

POST /auth/login/oauth/:provider → Auth().LoginByOAuth      (oauth.go)
  └─ provider.Exchange(code) → openid+profile
  └─ SELECT acct_oauth_accounts WHERE provider+open_id
       ├─ 命中 → 走老用户登录
       └─ 未命中 → 自动建账号 / 或返回 AccountBindRequired=true（按策略）
  └─ issueTokens + emitOutbox(method=oauth_<provider>)
```

### 5.5 Token 刷新（POST `/auth/token/refresh`）

入口：`handleRefresh` → `Auth().Refresh()` (`auth.go:743`)，**用一次轮换一次**：

```
POST /auth/token/refresh {refresh_token}
  └─ hash = sha256(refresh_token)
  └─ db.Transaction:
       ├─ SELECT acct_refresh_tokens FOR UPDATE WHERE token_hash=?
       ├─ 状态闸门：
       │   ├─ status != active → 重放！UPDATE 整 family_id 全部 revoked + Sessions 全部 revoked
       │   │   metrics.TokenReplay+1，返回 401
       │   └─ expires_at 过期 → status=expired，返回 401
       ├─ SELECT acct_sessions FOR UPDATE id=session_id AND status=active
       ├─ session 过期 / 用户不可用 / phoneBindingRequired → 对应错误
       ├─ UPDATE old.status = used + used_at
       ├─ loadRoleCodes
       └─ issueTokens(user, roles, deviceID, ip, ua,
                      sid=session.id, familyID=session.family_id, prevHash=oldHash)
                                                                     ← 同一会话内的链
```

安全模型：每条 refresh token 通过 `family_id` 串成链，旧 token 一旦再次出现 → **整族失活**。配合 §5.9 的 `invalidatePrincipalCache` 跨实例立即下线。

### 5.6 受保护接口的鉴权链（最高频热路径）

所有业务接口经过 `Middleware.Authenticate()` 都走这条：

```
HTTP <Authorization: Bearer xxx>
  └─ Middleware.Authenticate (middleware.go:24)
       └─ Auth().Validate(ctx, token)                    (auth.go:880)
            ├─ parseAccessToken(jwt) → claims (HS/RS 验签 + exp)
            ├─ [可选] TenantValidator(claims.tenant_id)  ← 租户停用一键全员失效
            ├─ [可选] AccessToken Denylist (denylist.go) ← jti 黑名单
            ├─ Redis getCachedPrincipal(claims) → 命中直接返回 ★ 0 次 DB IO
            ├─ 一次 JOIN 兜底：
            │     SELECT users.status,auth_version,roles_version,phone_verified_at,
            │            sessions.status,sessions.expires_at
            │     FROM acct_users JOIN acct_sessions ON sid=claims.sid
            │     WHERE user_id+tenant_id
            ├─ 闸门：session=active && 未过期 && user=normal &&
            │       claims.auth_version==users.auth_version &&
            │       claims.roles_version==users.roles_version &&
            │       (RequireVerifiedPhone? phone_verified_at!=nil : true)
            ├─ Redis cachePrincipal(claims, principal)
            └─ return Principal{UserID, TenantID, sid, Roles, ...}
       └─ ctx.Set(principalContextKey, principal)
       └─ 继续业务 Handler
```

业务 Handler 取出后可继续：

- `RequirePermission("post.write")` → `Authorizer.Check`（`platform_admin/tenant_owner` 直通；否则取角色权限并集判定）
- `RequireRole("editor")` → 直接看 `Principal.Roles`

> **改密、踢端、改角色、租户停用 → 全部通过 `invalidatePrincipalCache(sid)` 让 Redis 立即失效**，跨实例零延迟生效；下一次请求在 5.6 的"版本一致"或"session active"闸门会被 401 拦下，无需依赖客户端配合。

### 5.7 退出当前端 / 全端退出

```
POST /auth/logout                       （Authenticate 已注入 Principal）
  └─ p = GetPrincipal(req)
  └─ if ?all=true → Auth().LogoutAll(p.UserID)
  └─ else Auth().Logout({AccessToken, SessionID=p.sid})    (auth.go:815)
       └─ 解析 sid（优先 SessionID，其次 access_token，再次 refresh_token）
       └─ db.Transaction:
            ├─ UPDATE acct_sessions SET status=revoked, revoked_at=now WHERE id=sid
            └─ UPDATE acct_refresh_tokens SET status=revoked WHERE session_id=sid
       └─ emitOutbox("account.user.logged_out")
       └─ invalidatePrincipalCache(sid)

POST /auth/logout?all=true → Auth().LogoutAll(userID)      (auth.go:855)
  └─ pluck sessions.id WHERE user_id
  └─ db.Transaction:
       ├─ UPDATE acct_sessions SET revoked WHERE user_id AND active
       └─ JOIN UPDATE acct_refresh_tokens SET revoked WHERE session.user_id
  └─ invalidatePrincipalCaches(sids)

POST /sessions/revoke-others → Sessions().RevokeOther       (session.go)
  └─ 同 LogoutAll，但保留 currentSid 不动 —— "保留当前端，踢掉其他端"
```

### 5.8 `issueTokens`：所有"成功登录/续签"的统一出口

```
issueTokens(tx, user, roles, deviceID, ip, ua, sid, familyID, prevHash)  (auth.go:1042)
  ├─ sessionID = sid orelse newID()       ; familyID = familyID orelse newID()
  ├─ if prevHash == "":                   ← 首次登录才创建 session
  │     INSERT acct_sessions { id=sid, tenant_id, user_id, family_id,
  │                            device_id, ip, user_agent_hash,
  │                            status=active, expires_at=now+RefreshTokenTTL }
  ├─ refreshTokenPlain = randomString()
  ├─ INSERT acct_refresh_tokens {
  │      id, session_id=sid, family_id,
  │      token_hash=sha256(refreshTokenPlain),
  │      previous_hash=prevHash,                      ← 链式追溯
  │      status=active, expires_at=now+RefreshTokenTTL }
  ├─ accessToken,exp = signAccessToken(user, roles, sid)   ← JWT，TTL=AccessTokenTTL
  └─ return AuthResult{User, AccessToken, RefreshToken=refreshTokenPlain（仅此一次明文）, ExpiresAt, "Bearer"}
```

JWT claims：`user_id, tenant_id, sid, auth_version, roles_version, phone_verified, roles, jti, exp, iat`。`auth_version / roles_version` 是改密/改权后的"软失效"开关。

### 5.9 注销账号（DELETE `/users/me`）

入口：`handleDeleteAccount` → `Users().DeleteAccount()` (`user.go:167`)

```
DELETE /api/v1/account/users/me   （Authenticate）
  └─ p = Principal
  └─ Users().DeleteAccount({UserID:p.UserID})
       └─ pluck sessions.id WHERE user_id
       └─ db.Transaction:
            ├─ UPDATE acct_credentials SET enabled=false
            ├─ UPDATE acct_sessions SET revoked
            ├─ UPDATE acct_refresh_tokens SET revoked
            ├─ UPDATE acct_users SET status=deleted, deleted_at=now    ← 软删
            └─ DELETE acct_user_roles
       └─ emitOutbox("account.user.deleted")
       └─ invalidatePrincipalCaches(sids)
```

> 软删除：行保留以满足审计/合规；所有凭证关闭、所有会话下线、所有角色解绑。同邮箱/手机号需要走重新激活流程才能复用。

### 5.10 改密 / 强制下线场景

```
PATCH /users/me/password → Users().ChangePassword            (user.go:112)
  └─ [若 RequireStepUp] 强制 step-up MFA
  └─ db.Transaction:
       ├─ verifyPassword(old) / 否则 401
       ├─ hashPassword(new) + UPDATE credentials
       ├─ UPDATE users SET auth_version = auth_version+1     ★ 让所有旧 access token 失效
       ├─ UPDATE acct_sessions SET revoked WHERE user_id
       └─ JOIN UPDATE acct_refresh_tokens SET revoked
  └─ invalidatePrincipalCaches(sids)
  └─ emitOutbox("account.user.password_changed")
```

`auth_version+1` 后，所有旧 token 在 §5.6"版本一致"闸门被 401 拦下；管理员侧的 `Admin().Disable / Lock / ResetPassword` 走同样的范式。

### 5.11 异步出口：Outbox / Audit / Metrics

每个写操作都伴随三类副作用，**全部写在同一事务里以保证一致性**：

| 出口 | 写入位置 | 目的 |
|---|---|---|
| `emitOutbox(topic, key, payload)` | `acct_outbox` 表 | 业务事件解耦：搜索/报表/通知/IM 由外部消费者拉取 |
| `audit(...)`（异步 channel） | `acct_audit_logs` | 安全审计、合规导出、风控分析 |
| `metrics.*Total / *Duration` | OTel SDK | 监控告警：登录失败率、token 验证 QPS、风控阻断量 |

`OutboxWorker` 周期性扫表 → 投递到 Kafka/MQ → 标记已投递；失败重试。这套机制保证"账号事件 → 业务订阅"链路最终一致，不丢消息。

### 5.12 一张表速查：HTTP 路径 → SDK 入口 → 关键写表 / 缓存动作

| HTTP | Handler | SDK 入口 | 关键 DB 写入 | Outbox / 缓存 |
|---|---|---|---|---|
| `POST /auth/register` | `handleRegister` | `Auth().Register` | acct_users + acct_credentials + acct_user_roles + acct_sessions + acct_refresh_tokens | `account.user.registered` |
| `POST /auth/login` | `handleLogin` | `Auth().Login` | acct_sessions + acct_refresh_tokens（+ user.last_login_*） | `account.user.logged_in` + cachePrincipal |
| `POST /auth/login/code` | `handleLoginWithCode` | `Auth().LoginWithVerificationCode` | 同上 + 验证码消费 | 同上 |
| `POST /auth/login/oauth/:p` | `handleOAuthLogin` | `Auth().LoginByOAuth` | 同上 + acct_oauth_accounts | 同上 (`method=oauth_<p>`) |
| `POST /auth/token/refresh` | `handleRefresh` | `Auth().Refresh` | acct_refresh_tokens (旧 used / 新 active) | — |
| `POST /auth/logout` | `handleLogout` | `Auth().Logout` | sessions/refresh_tokens revoke | `account.user.logged_out` + invalidatePrincipalCache |
| `POST /auth/logout?all` | `handleLogout` | `Auth().LogoutAll` | 全部 sessions/refresh_tokens revoke | invalidatePrincipalCaches |
| `GET /users/me` | `handleGetCurrentUser` | `Users().GetCurrent` | 只读 | — |
| `PATCH /users/me/password` | `handleChangePassword` | `Users().ChangePassword` | credentials + users.auth_version+1 + sessions/refresh_tokens revoke | `account.user.password_changed` + invalidatePrincipalCaches |
| `DELETE /users/me` | `handleDeleteAccount` | `Users().DeleteAccount` | credentials disabled + sessions/refresh_tokens revoke + users.deleted + user_roles 清空 | `account.user.deleted` + invalidatePrincipalCaches |
| `GET /sessions` | `handleListSessions` | `Sessions().List` | 只读 | — |
| `DELETE /sessions/:id` | `handleRevokeSession` | `Sessions().Revoke` | session+refresh_tokens revoke | invalidatePrincipalCache |
| `POST /sessions/revoke-others` | `handleRevokeOtherSessions` | `Sessions().RevokeOther` | 同上（除当前 sid） | invalidatePrincipalCaches |
| `任意受保护接口` | — | `Middleware.Authenticate → Auth().Validate` | 命中缓存 0 IO；未命中一次 JOIN | getCachedPrincipal/cachePrincipal |

---

## 6. 错误码约定

所有 SDK 错误通过 `errwrap`（namespace=`account`）抛出，`code` 与 HTTP 状态对齐：

| 常量 | code | 含义 |
|---|---|---|
| `ErrInvalidArgument` | 400 | 参数非法 |
| `ErrInvalidCredential` | 401 | 用户名/密码错误 |
| `ErrInvalidToken` | 401 | Token 无效 |
| `ErrExpiredToken` | 401 | Token 过期 |
| `ErrRevokedToken` | 401 | Token 已吊销 |
| `ErrPermissionDenied` | 403 | 权限不足 / Step-up 触发 |
| `ErrIdentifierExists` | 409 | 用户名/邮箱/手机已存在 |
| `ErrAccountLocked` | 423 | 账号被风控锁定 |
| `ErrPhoneBindingRequired` | 428 | 必须先绑手机 |
| `ErrRateLimited` / `ErrRiskBlocked` | 429 | 频控/风控 |
| `ErrInternal` | 500 | 内部错误 |

业务侧统一处理：

```go
import "github.com/xxzhwl/gaia/errwrap"

if err != nil {
    if e, ok := errwrap.As(err); ok {
        switch e.Code {
        case account.ErrPermissionDenied:
            // 提示用户权限不足或要求 step-up
        case account.ErrAccountLocked:
            // 提示稍后重试
        }
    }
    return err
}
```

---

## 7. 标准 OIDC 接入（账号服务作为 IdP）

如果你要让**第三方应用**（小程序/合作方系统）通过账号服务登录：

1. 管理端用 `POST /api/v1/account/idp/clients` 注册一个 client，得到 `client_id` / `client_secret`。
2. 第三方走标准 OIDC：
   - 浏览器重定向到 `GET /api/v1/account/oauth/authorize?client_id=...&redirect_uri=...&response_type=code&scope=openid+profile&state=xxx&code_challenge=...`
   - 拿到 code 后服务端 `POST /api/v1/account/idp/clients/token`（grant_type=authorization_code）换 token
   - 用 access_token 调 `GET /api/v1/account/oauth/userinfo`
   - 自动发现：`GET /api/v1/account/.well-known/openid-configuration`
   - 公钥：`GET /api/v1/account/oauth/certs`

服务到服务调用（machine-to-machine）使用 `grant_type=client_credentials`。

---

## 8. 部署清单

```
1. MySQL：8.0+，UTF8MB4，数据库需要 CREATE/ALTER 权限（用于 AutoMigrate）
2. Redis：6+，建议独立 logical DB
3. JWT.SecretKey：≥32 字节随机串（生产环境）
   推荐用 KeySet（非对称 RS256），便于公钥分发与轮换
4. NotifyProvider：接入企业短信/邮件网关
5. 风控阈值：根据业务调整 Account.Risk.*
6. 审计：建议开启 Account.Audit.AsyncWrite=true，并配置 ArchiveRetentionDays
7. 监控：Manager 已暴露 metrics（详见 metrics.go），接入 Prometheus
```

### 8.1 路由组按部署形态切分（Profile / Module）

同一份二进制可以通过 `StandaloneConfig.Profile` 切出**对外服务**和**内部管理服务**两套部署，无需重新编译：

| Profile | 启用模块 | 适用部署 |
|---|---|---|
| `full`（默认） | 全部 12 个模块 | 单实例小规模 / 体验环境 |
| `public` | health, auth, verification, user, mfa, session, org, oidc, passkey, audit | 面向 C 端 / 前端的对外 ingress（**不含 admin、idp 客户端管理**） |
| `admin` | health, auth, user, session, admin | 仅内网管理控制台（**不含注册/验证码/oidc/passkey 等公开接口**） |

模块粒度（`RouteModule`）：`health / auth / verification / user / mfa / session / org / idp / oidc / passkey / audit / admin`。

**配置驱动（推荐）**：在 `Account.Standalone.Profile` 里写 `public` 或 `admin`，运维改 yaml 即可切换，无需重启二进制类型：

```yaml
Account:
  Standalone:
    Profile: public        # 对外 C 端进程
```

```yaml
Account:
  Standalone:
    Profile: admin         # 内网管理端进程（同一份二进制）
```

**代码驱动 + 模块覆盖**：当预设 profile 不能完全覆盖时，可在其上做加减：

```go
svc, _ := account.NewStandaloneServiceWithConfig(cfg, account.StandaloneConfig{
    ListenAddr: "8080",
    Profile:    account.ProfilePublic,
    // 在 public 基础上补一个 idp 客户端注册端点（OIDC IdP 模式）
    EnabledModules:  []account.RouteModule{account.ModuleIdP},
    // 同时屏蔽组织只读接口
    DisabledModules: []account.RouteModule{account.ModuleOrg},
})
```

最终启用集合 = `profileBaseModules(Profile) ∪ EnabledModules \ DisabledModules`，启动日志会打印一行 `[account] standalone routes registered: profile=public modules=[...]` 方便核对。

**典型部署拓扑**：

```
                    ┌──────────────────────────────────┐
   外网 / CDN ───→  │  account-public  (Profile=public) │  → /api/v1/account/auth/login...
                    │  3 实例                            │
                    └──────────────────────────────────┘
                                                                共享同一套 MySQL + Redis
                    ┌──────────────────────────────────┐
   内网堡垒机 ───→  │  account-admin   (Profile=admin)  │  → /api/v1/account/admin/users...
                    │  1 实例（受跳板机访问控制）        │
                    └──────────────────────────────────┘
```

两类进程消费同一套 `Account.*` 配置和数据，仅 `Account.Standalone.Profile` 不同，因此不会出现"配置漂移"。所有路由组的 SDK 入口、DB 写入、缓存动作、outbox 事件完全一致——见 §5 调用链全景图。

---

## 9. 升级与扩展路线

- **Policy（ABAC）**：用 `mgr.Policy()` 写形如 `resource.owner == subject.id` 的策略，超出 RBAC 静态权限码的能力时启用。
- **Passkey/WebAuthn**：标准接口已就绪，前端启用 `navigator.credentials` 后即可走 `/passkey/*` 路由替代密码。
- **MFA Step-up**：敏感操作（改密、解绑 MFA、分配高权限角色）会自动要求二次验证，前端捕获 `ErrPermissionDenied + reason=stepup_required` 后弹 MFA 框，详见前端文档。

  Step-up 实现要点（v1.x 重构后）：

  - **按 sid 维度**：成功标记写入 `acct_sessions.mfa_satisfied_at`（每个浏览器/设备独立）。
    Safari step-up 不会让 Chrome 上的同账号会话获得敏感操作权限。
  - **不依赖外部缓存**：标记持久化在 DB 上，Redis 不可用也不会静默放行。
  - **窗口可配**：`Config.StepUpWindow`（或 `Account.StepUpWindow` 配置项，单位分钟，默认 5）。
    转账等高风险动作建议业务侧自行额外检查并把窗口设为 0。
  - **会话失效自动作废**：session 一旦被改密/退出/风控吊销，`mfa_satisfied_at` 跟随整行作废，无需单独清理。
  - **失败计数**：TOTP 错误时挑战 `attempts` 自增，达到 `MaxAttempts=5` 后挑战作废，挡掉 6 位数暴力。

  ```go
  // 业务侧标准接入模式
  func (h *Handler) DoSensitive(ctx context.Context, req X) error {
      p := authn.MustPrincipal(ctx)
      if err := h.acct.Auth().RequireStepUp(ctx, p); err != nil {
          return err // → ErrPermissionDenied，前端会弹 step-up 框
      }
      return h.bizSensitive(ctx, req)
  }
  ```

  关联指标：
  - `acct.mfa.stepup.granted` — step-up 验证通过数
  - `acct.mfa.stepup.denied{reason}` — 拦截敏感操作数（`expired_or_missing` / `session_not_found` / `session_inactive` / `no_sid`）
  - `acct.mfa.totp.replay` — TOTP 一次性码重放检测数
- **Outbox**：业务事件通过 outbox 表保证至少一次投递，可对接消息队列做下游通知。

---

完整 REST API 路由清单见 `standalone.go` / `standalone_admin.go` 中的 `registerRoutes` / `registerAdminRoutes`，前端对接示例见 [`FRONTEND_INTEGRATION.md`](./FRONTEND_INTEGRATION.md)。
