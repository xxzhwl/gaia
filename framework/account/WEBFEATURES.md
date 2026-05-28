# Web 端账户体系功能场景清单

> 配套后端：`gaia/framework/account`
> 接口契约：见 [`BACKEND_INTEGRATION.md`](./BACKEND_INTEGRATION.md)
> SDK 接入：见 [`FRONTEND_INTEGRATION.md`](./FRONTEND_INTEGRATION.md)
> 适用场景：浏览器 SPA / SSR / H5 / Node BFF

本文档不讲接口字段，只讲**Web 端要做什么产品场景**。分两大块：

- **C 端用户场景**（Part A）：登录、账户、安全、隐私
- **B 端管理场景**（Part B）：用户管理、角色权限、审计、租户、配置

---

## 0. 通用基础设施（用户端 / 管理端共用）

这部分是地基，无论做哪个端都必须先做扎实。

### 0.1 Token 与会话

| 项 | 推荐方案 | 说明 |
|---|---|---|
| `access_token` 存储 | **JS 内存（闭包/Pinia/Redux）** | 不进 localStorage / sessionStorage，规避 XSS |
| `refresh_token` 存储 | **httpOnly + Secure + SameSite=Lax cookie** | 由网关或 BFF 写入，前端 JS 拿不到 |
| `device_id` | 启动时生成 UUID v4 + IndexedDB 持久化；FingerprintJS 增强 | 风控用 |
| 自动续期 | 401 拦截器内调 `/auth/token/refresh` | 见下 |
| 并发请求收敛 | 全局 `refreshing: Promise<TokenPair> \| null` | N 个 401 只能发 1 个 refresh |
| 登出 | 调 `/auth/logout` → 清内存 token → 清 cookie → 跳登录 | |
| 跨 Tab 同步 | `BroadcastChannel('auth')` 或 `storage` 事件 | A Tab 登出 B Tab 同步登出 |

### 0.2 路由守卫

```
beforeEach((to) => {
  if (to.meta.requiresAuth && !isAuthed())  → /login?next=<原路径>
  if (to.meta.permissions  && !hasAll(perms)) → /403
  if (to.meta.requiresMFA  && !mfaEnabled)  → /settings/security/mfa?force=1
})
```

未登录跳转**必须保留 `next`**，登录后回原页面。

### 0.3 错误处理与文案

- 错误码 → 多语言文案映射表，**禁止把后端 raw msg 直接显示**
- 401 / 403 / 404 / 423 / 428 / 429 各有对应 UI（不要全弹"操作失败"）
- 网络异常和业务错误要分开处理，弱网下不能误把"网络挂了"当成"被踢了"

### 0.4 国际化、主题、无障碍

- i18n：登录注册页通常是无障碍审计高发地，提早接入
- 暗黑模式：登录页常被忘
- a11y：input label、aria-describedby、键盘 Tab 流转、错误提示用 `role="alert"`

### 0.5 埋点

- 登录漏斗：`page_view → input → submit → captcha → success/fail`
- MFA 流失分析：`stepup_triggered → request → modal_open → input → success/fail`
- 注销转化：每一步流失率
- 异常账号：被踢、被冻结、被强制改密的次数

---

# Part A · C 端用户场景

## A1. 入口：注册 / 登录 / 找回

### A1.1 注册

| 子场景 | 关键点 |
|---|---|
| 邮箱/手机号注册 | 实时唯一性校验（带 debounce）；密码强度计；勾选协议 |
| 验证码发送 | 60s 倒计时；图形验证码兜底；短信/邮件 channel 切换 |
| 协议确认 | 用户协议 + 隐私协议必须勾选；版本号变更要重新确认 |
| 注册后引导 | 头像、昵称、偏好设置；可跳过 |
| 风控失败兜底 | 触发频控时 UI 显示等待时间 + 引导改用其他方式 |

### A1.2 登录

| 方式 | 说明 |
|---|---|
| 密码登录 | 错误提示要"模糊"，不暴露"用户不存在 vs 密码错误"，避免账号枚举 |
| 验证码登录 | 手机号 + 短信 OTP；同样 60s 倒计时 + 图形码兜底 |
| 第三方登录 | Google / GitHub / 微信 / Apple；走 OAuth 重定向；回调页处理 `state` 防 CSRF |
| **Passkey / WebAuthn** | Web 独有杀器；调 `navigator.credentials.get()` |
| Magic Link | 邮箱里点链接登录，常见于 SaaS |
| 扫码登录（被扫端）| Web 显示二维码 → 轮询 `/auth/qrcode/status` → 扫码确认后拿 token |
| "记住我 30 天" | 控制 refresh_token TTL；checkbox 默认不勾选 |

### A1.3 找回密码

- 邮箱/手机收验证码 → 重置；密码修改后**所有 session 自动作废**
- 防爆破：连续失败 N 次后冷却
- 重置 token 一次性、有效期短（≤30 分钟）

### A1.4 OAuth 回调页

- 回调 URL 严格白名单
- 校验 `state` 参数防 CSRF
- 失败要给用户友好错误页（"授权被取消" / "授权失败请重试"）

---

## A2. 会话保活

| 场景 | 实现 |
|---|---|
| 静默续期 | access_token 401 → 自动 refresh → 重放原请求 |
| Refresh 失败 | 弹"登录已过期"提示 → 跳 `/login?next=...` |
| 多 Tab 同步 | BroadcastChannel：登出/续期/切租户事件广播 |
| 标签页可见性 | `visibilitychange` 触发 → 切回前台时 ping `/auth/me` 确认登录态 |
| 被踢下线 | 收到 `revoked` 错误 → 提示"账号已在其他设备登录" |
| auth_version 变更 | 改密/改 MFA 后 server 把版本号 +1，下个请求 401 → 强制重登 |
| 离线/弱网 | 区分"网络问题"（不登出）vs "401"（登出） |

---

## A3. 账户中心

### A3.1 个人资料

- 头像：上传 + 裁剪 + 占位图 + 默认头像生成（首字母彩底）
- 昵称、性别、生日、地区、个性签名
- 用户名/UID 不可改（仅显示）
- 编辑后乐观更新 + 失败回滚

### A3.2 联系方式

| 操作 | 安全要求 |
|---|---|
| 绑定/换绑邮箱 | **双向验证**：旧邮箱告警通知 + 新邮箱验证码 |
| 绑定/换绑手机号 | 同上：旧号短信告警 + 新号验证码 |
| 解绑 | 至少保留一种联系方式可用于找回 |

### A3.3 登录方式管理

- 已绑定的第三方账号列表（Google / 微信 …）
- 绑定/解绑保护：**至少保留一种登录方式**，避免锁死
- OAuth-only 用户首次设置密码（无需输旧密码）

### A3.4 注销账号

长流程，分步反悔：

```
风险提示页 → 输入密码 → step-up MFA → 选择注销原因 → 提交
↓
进入冻结期（7~30 天）→ 期间任意时刻可"取消注销"恢复
↓
冻结期满 → 真正物理删除（异步任务）
```

---

## A4. 安全中心（Step-up 重灾区）

### A4.1 登录历史与活跃会话

| 功能 | 细节 |
|---|---|
| 登录历史 | 时间、IP、地理位置、设备型号、UA、是否成功 |
| 活跃 session 列表 | 标"当前会话"；显示设备名/最后活跃时间 |
| 踢出单个 session | 调 `/auth/sessions/:id/revoke`（写操作 → 触发 step-up） |
| 一键退出其他设备 | 保留当前 session，吊销其他全部 |
| "这是不是我" | 异地登录邮件中的反向确认入口 |

### A4.2 MFA 管理

```
[启用 TOTP]
  → 弹二维码 + 手动 secret
  → 用户用 Authenticator 扫码
  → 输入 6 位验证 → 后端 verify
  → 强制下载备份码（"用过即作废"）
  → 启用成功

[停用 TOTP]
  → step-up 拦截 → 输 TOTP 验证 → 停用

[备份码]
  → 重新生成（旧码立即失效，需 step-up）
  → 一键打印 / 复制 / 下载 .txt
```

### A4.3 Passkey 管理（Web 杀手级）

- 注册多个 passkey：Mac、iPhone、安全密钥（YubiKey）
- 每个 passkey 命名（"Mac mini 公司"、"iPhone 15"）
- 列表显示：名称、平台、上次使用时间
- 删除某个 passkey 需 step-up

### A4.4 Step-up 弹窗

全局拦截器统一处理，**业务页面零感知**：

```ts
axios.interceptors.response.use(undefined, async (err) => {
  if (err.response?.status === 403 &&
      err.response.data?.msg === 'stepup_required') {
    const { challenge_id } = await api.post('/auth/stepup/request')
    const code = await mfaModal.show()           // 全局共享 Modal
    await api.post('/auth/stepup/complete', { challenge_id, code })
    return axios(err.config)                      // 自动重放
  }
  throw err
})
```

弹窗 UX 要点：
- 6 个独立 input、自动跳格、整串粘贴自动分发
- `inputmode="numeric"` 在移动 H5 唤起数字键盘
- 失败提示带剩余次数（"3/5"）
- 5 次用尽 → "请重新发起操作"
- 兜底入口：备份码、客服找回

### A4.5 风控通知

站内信 + 邮件：

- 异地登录、新设备登录
- 改密成功、解绑 MFA
- API Token 创建/吊销
- 高危操作执行（注销、转账、批量导出）

---

## A5. 权限在 UI 上的体现

| 场景 | 实现 |
|---|---|
| 路由级 | 进 `/admin` 前检查 permissions 包含 `admin.*` 否则 → 403 |
| 菜单级 | 根据 permissions 过滤菜单项 |
| 按钮级 | `<Permission code="user.delete">` 包裹；无权时**隐藏**或**禁用 + tooltip** |
| 多租户 | 顶部下拉切换 tenant，切换后**清空所有数据缓存**重新拉取 |
| 角色徽章 | Profile 页展示当前角色 chip |

---

## A6. 隐私与合规

| 功能 | 触发场景 |
|---|---|
| Cookie 同意横幅 | GDPR 必备，首次访问展示 |
| 用户协议版本升级 | 重新弹确认；旧版本签名失效 |
| 数据导出 | "下载我的全部数据"按钮 → 后端异步生成包 → 邮件发链接（step-up 保护） |
| 关闭个性化推荐 | 监管要求（国内） |
| 第三方 SDK 清单 | 设置页可查 |

---

## A7. 异常态全清单

| 状态 | UI 处理 |
|---|---|
| 未登录访问受保护页 | 跳登录 + 保留 `next` |
| Token 过期 | 静默 refresh，不打扰用户 |
| Refresh 也过期 | 提示"登录已过期"+ 跳登录 |
| 被踢下线（revoked） | "账号已在其他设备登录"或"已被强制下线" |
| 账号被冻结 | 专属页：冻结原因 + 申诉入口 |
| 账号被注销 | 跳注销完成页，禁止再登录 |
| 首次登录强制改密 | 拦截到改密页，改前所有路由 redirect |
| 强制启用 MFA | 拦到 MFA 绑定页 |
| 网络异常 | 不要登出，加重试 + 离线提示 |
| 服务端 5xx | 通用错误页，不暴露堆栈 |

---

# Part B · 管理端场景（B 端 / 后台）

管理端的特点：**操作敏感、影响他人、需要审计**。Step-up 触发频率远高于用户端。

## B1. 管理员登录的特殊点

| 项 | 要求 |
|---|---|
| 登录入口 | 通常和 C 端分离：`/admin/login`；可走独立域名 |
| 强制 MFA | 管理员账号**必须启用 TOTP / Passkey** 才能进入后台 |
| IP 白名单（可选）| 仅允许办公网/VPN 段访问 |
| 短 session TTL | 推荐 30~60 分钟，远低于 C 端的 7~30 天 |
| 登录后强制 step-up | 进入"危险操作"模块（删用户、改权限）必须二次验证 |
| 操作时长心跳 | 长时间无操作 → 锁屏，需重新输密码或 MFA 唤醒 |

---

## B2. 用户管理

### B2.1 用户列表

- 多条件搜索：UID、邮箱、手机号、昵称、注册时间、状态、角色、租户
- 状态筛选：正常 / 冻结 / 待激活 / 已注销 / 锁定
- 批量操作：批量冻结、批量解锁、批量推送通知
- 导出（CSV / Excel）→ **必须 step-up + 写审计**

### B2.2 用户详情

| Tab | 内容 |
|---|---|
| 基本信息 | 资料、状态、注册时间、最后登录 |
| 联系方式 | 邮箱、手机、第三方绑定 |
| 角色与权限 | 当前角色 + 直接授权的 permissions + 来源（哪个角色给的） |
| 登录历史 | 完整登录记录（时间、IP、UA、成功/失败、风控原因）|
| 活跃会话 | 当前在哪些设备登录，可强制踢 |
| 安全配置 | TOTP / Passkey / 备份码状态 |
| 操作日志 | 该用户产生的审计事件 |
| 风控记录 | 触发过的风控规则、是否被锁定 |

### B2.3 管理员对用户的危险操作

每一项都必须 **step-up + 审计**：

| 操作 | 说明 |
|---|---|
| 冻结 / 解冻账号 | 设置冻结原因、自动到期时间 |
| 强制改密 | 用户下次登录必须改密码 |
| 强制启用 MFA | 标记 must_setup_mfa |
| 重置 MFA | 客服场景：用户丢手机时帮忙重置 TOTP |
| 重置密码 | 生成一次性链接发给用户 |
| 强制下线（吊销所有 session） | 一键 |
| **Impersonate（代登录）** | 高敏感：必须独立审计、有时间窗口、操作期间所有动作打 `impersonator` 标签 |
| 修改角色 | 加/减角色，立即生效（auth_version+1） |
| 修改邮箱/手机 | 客服场景，紧跟用户身份核验流程 |
| 删除/恢复账号 | 跳过用户冻结期 |

### B2.4 用户行为画像（运营辅助）

- 注册渠道、登录频次、最近一次活跃
- 安全得分（是否启用 MFA / 是否绑定多种联系方式）
- 风控等级

---

## B3. 角色与权限管理（RBAC）

### B3.1 角色

| 功能 | 说明 |
|---|---|
| 角色列表 | 内置角色（admin / user）+ 自定义角色 |
| 创建/编辑角色 | 名称、code、描述、归属租户 |
| 角色绑定权限 | 树形 / 分组多选 permissions |
| 角色绑定用户 | 反向视图：这个角色当前哪些用户在用 |
| 角色克隆 | 基于已有角色快速创建变种 |
| 删除角色 | 检查是否还有用户在用 → 警告 |

### B3.2 权限点

| 功能 | 说明 |
|---|---|
| 权限点目录 | 后端注册的 permission code 列表 |
| 分组展示 | 按模块分组（用户管理 / 内容 / 账单 …） |
| 搜索 | 按 code、描述模糊搜索 |
| 权限点对应资源 | 提示该权限控制哪些 API / 菜单 |

### B3.3 用户授权

- 给单个用户直接授角色（或撤销）
- 给单个用户加直接权限（旁路角色）
- **变更生效**：下次请求 401 强制重新拿 principal（auth_version+1）

### B3.4 权限模拟（Permission Preview）

排查"为什么这个用户没权限"：

```
选择用户 → 显示其所有 effective permissions
       → 点某权限 → 反向追溯：来自哪个角色 / 直接授权 / 拒绝原因
```

---

## B4. 多租户管理

| 功能 | 说明 |
|---|---|
| 租户列表 | 名称、code、状态、用户数、创建时间 |
| 创建/编辑租户 | 名称、code、配额、过期时间 |
| 租户切换 | 超管可在租户间切换查看；切换后清缓存 |
| 租户管理员设置 | 指定哪些用户是该租户的管理员 |
| 租户配额 | 用户数上限、存储上限、API 调用上限 |
| 租户冻结/删除 | 整租户级停用；删除前导出数据 |

---

## B5. 审计日志（合规核心）

| 维度 | 内容 |
|---|---|
| 列表筛选 | 时间范围、操作者、目标用户、模块、动作类型、IP、是否成功 |
| 全文搜索 | 资源 ID、自定义标签 |
| 时间线视图 | 单个用户的所有事件按时间排列，便于事故复盘 |
| 详情 | 请求体（脱敏）、响应、IP、UA、trace_id |
| 关联跳转 | 点击 trace_id 跳到 APM/日志系统 |
| 导出 | 按筛选条件导出（step-up 保护） |
| 不可删除 | 审计记录只能查不能改 |
| 长期归档 | 冷热分离，3 年以上转 S3 |
| 告警规则 | 配置"管理员一小时内修改超过 10 个用户密码"等告警 |

---

## B6. 风控与告警

### B6.1 风控规则配置

| 规则 | 配置项 |
|---|---|
| 登录失败锁定 | N 次失败锁 M 分钟 |
| 异地登录 | 国家/省份变化触发告警 / 强制 step-up |
| 频控 | 单 IP/单账号每分钟最大请求数 |
| 设备指纹黑名单 | 拉黑某 device_id |
| 邮箱/手机号黑名单 | 注册阻断 |
| IP 黑/白名单 | 段级配置 |
| TOTP 暴力检测 | 单 challenge 失败次数上限 |

### B6.2 风控事件中心

- 实时事件流
- 命中规则展示
- 一键采取行动（锁号、强制下线、加黑）
- 误判申诉处理

### B6.3 告警通道

- 邮件、企业 IM（飞书/钉钉/企微）、Webhook、PagerDuty
- 告警分级（INFO / WARN / CRITICAL）

---

## B7. 系统配置

| 模块 | 配置项 |
|---|---|
| 通用 | 站点名、Logo、外发邮件域名 |
| 注册 | 是否开放注册、是否需要邀请码、白名单邮箱后缀 |
| 登录 | 启用哪些登录方式、密码策略、session TTL |
| MFA | 是否强制、TOTP 时间漂移容忍、备份码数量 |
| Step-up | 默认窗口、按 action 自定义窗口 |
| OAuth | 第三方 client_id/secret、重定向 URL 白名单 |
| 邮件/短信模板 | 多语言模板编辑器 |
| 频控 | 各接口的 QPS / 总量配额 |
| 数据保留 | 审计/日志/会话归档周期 |

---

## B8. 客服模块

面向一线客服（权限受限的运营子账号）：

| 功能 | 说明 |
|---|---|
| 用户搜索（受限）| 仅按 UID / 邮箱精确查；不能批量导出 |
| 帮用户重置 MFA | 客服流程：身份核验工单 → 客服执行 → 用户邮件确认 |
| 帮用户重置密码 | 同上，发一次性链接 |
| 解封账号 | 仅能解封"客服误封"工单关联的账号 |
| 工单系统 | 接入 CRM；每个客服动作绑定工单号写审计 |
| 不能做的事 | 改角色、改租户配置、impersonate（除非高级客服 + 主管审批）|

---

## B9. 数据看板（Dashboard）

| 模块 | 指标 |
|---|---|
| 用户增长 | 日/周/月新增、流失、活跃 |
| 登录趋势 | 登录成功率、失败原因分布、登录方式占比 |
| 安全态势 | 启用 MFA 比例、Passkey 渗透、近 24h 风控触发 |
| 审计热力 | 高频操作排行、异常时间段 |
| 系统健康 | 接口 P50/P99、错误率、Redis 命中率（接 metrics 即可） |

---

# Part C · Web 独有的能力（写在最后）

这些是 App 端做不到、Web 必须或最适合做的：

| 能力 | 说明 |
|---|---|
| **Passkey / WebAuthn** | 浏览器原生 API，硬件密钥/系统 passkey 体验最佳 |
| **多 Tab 状态同步** | BroadcastChannel / storage event |
| **CSRF 防护** | cookie 模式必须；管理后台尤其重要 |
| **iframe 嵌入策略** | X-Frame-Options / CSP frame-ancestors |
| **浏览器后退/前进** | 路由栈管理，避免返回时跳回登录页 |
| **扫码登录（被扫端）** | 配合 App 当扫码端 |
| **打印支持** | 备份码、API Token 一键打印（管理端常用） |
| **拖拽上传** | 头像、批量导入用户用 |
| **复杂表格** | 后台用户列表/审计列表的虚拟滚动、列冻结、列设置 |

---

## 路由 / 页面清单（Web）

### C 端用户

```
/login                    多种登录方式聚合
/register                 注册
/forgot-password          找回密码
/reset-password?token=    重置密码
/oauth/callback/:provider 第三方回调
/login/qrcode             扫码登录
/welcome                  新用户引导

/me                       公开主页
/settings/profile         编辑资料
/settings/account         账号信息（邮箱/手机/第三方）
/settings/security
  ├ /password             改密
  ├ /mfa                  MFA 管理
  ├ /passkeys             Passkey 管理
  ├ /sessions             活跃会话
  ├ /history              登录历史
  ├ /api-tokens           API Token（ToB）
  └ /authorized-apps      OAuth 授权管理
/settings/notifications   通知偏好
/settings/privacy         隐私 / 数据导出 / 注销
/settings/preferences     语言、时区、主题

/403  /404  /500          异常页
/account/frozen           冻结页
/account/closed           注销完成页
```

### 管理端

```
/admin/login                  独立登录入口

/admin                        看板首页
/admin/users                  用户列表
/admin/users/:id              用户详情（多 Tab）
/admin/users/:id/impersonate  代登录确认页

/admin/roles                  角色列表
/admin/roles/:id              角色详情
/admin/permissions            权限点目录

/admin/tenants                租户列表
/admin/tenants/:id            租户详情

/admin/audit                  审计日志
/admin/audit/:id              事件详情
/admin/risk/events            风控事件流
/admin/risk/rules             风控规则配置

/admin/settings/general       通用配置
/admin/settings/auth          登录与密码策略
/admin/settings/mfa           MFA 策略
/admin/settings/oauth         第三方登录配置
/admin/settings/templates     邮件/短信模板

/admin/support/tickets        客服工单
```

---

## 实施分期建议

| 阶段 | C 端 | 管理端 |
|---|---|---|
| **P0（MVP，1~2 周）** | 注册/登录/找回密码/退登/Profile/路由守卫/token 拦截器 | 管理员登录/用户列表/用户详情查看 |
| **P1（1 周）** | 改密/改邮箱手机/登录历史/活跃会话/第三方登录 1 个 | 冻结/解冻、强制下线、强制改密、审计日志查询 |
| **P2（1~2 周）** | MFA 绑定/解绑/备份码、Step-up 全局拦截、风控通知 | 角色/权限管理、Impersonate、Step-up 全覆盖 |
| **P3（1 周）** | API Token、多 Tab 同步、暗黑模式、a11y | 多租户、风控规则配置、数据看板 |
| **P4** | Passkey、设备指纹、合规模块、A/B、扫码登录 | 客服模块、告警通道、配置中心 |

---

## 一句话总结

**Web 用户端的核心命题是「让用户进得来、留得住、信得过、走得了」；Web 管理端的核心命题是「每一次危险操作都可溯源、可拦截、可申诉」。前者是 UX 工程，后者是合规工程，两者共用同一套后端 API，但在产品形态、Step-up 触发频率、Session TTL、UI 复杂度、审计粒度上完全不同档次。**
