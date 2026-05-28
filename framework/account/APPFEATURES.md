# App 端账户体系功能场景清单

> 配套后端：`gaia/framework/account`
> 接口契约：见 [`BACKEND_INTEGRATION.md`](./BACKEND_INTEGRATION.md)
> 适用场景：iOS（Swift / SwiftUI）/ Android（Kotlin / Compose）/ 跨平台（RN / Flutter / Wails / Tauri Mobile）

本文档不讲接口字段，只讲**App 端要做什么产品场景**，并区分：

- **C 端用户 App**（Part A）：To C 产品的常规账号体验
- **B 端管理 App**（Part B）：管理员 / 客服移动端的特殊需求

App 与 Web 最大的差异在于：**有 OS 级原生能力（生物识别、推送、Keychain、相机、通讯录、位置）+ 无浏览器（无 cookie、无 Tab、无 BroadcastChannel）+ 有商店审核合规要求**。

---

## 0. 通用基础设施（用户端 / 管理端共用）

### 0.1 Token 与会话存储

| 平台 | 推荐方案 |
|---|---|
| **iOS** | `Keychain Services`，attribute = `kSecAttrAccessibleWhenUnlockedThisDeviceOnly` + `kSecAttrSynchronizable=false` |
| **Android** | `EncryptedSharedPreferences`（Jetpack Security）或 `Android Keystore` 包装的 AES |
| **RN** | `react-native-keychain` / `expo-secure-store` |
| **Flutter** | `flutter_secure_storage` |

**绝对禁止**：
- iOS：`UserDefaults` / Plist 文件
- Android：`SharedPreferences`（未加密）/ 数据库明文
- 跨端：明文文件、AsyncStorage（RN）、SharedPreferences

### 0.2 设备唯一标识

| 平台 | 推荐 |
|---|---|
| iOS | `identifierForVendor`（IDFV）+ Keychain 持久 UUID 兜底（卸载重装不变） |
| Android | `Settings.Secure.ANDROID_ID` + 自生成 UUID 持久化（应对厂商差异） |
| 跨端 | 启动时生成 UUID v4 写入安全存储，永远不变直到用户手动清数据 |

不要用 IDFA / OAID（这些是广告 ID，会被用户重置且违反隐私规范）。

### 0.3 启动流程（冷启动）

```
App 启动
  ↓
Splash 显示（≤ 1.5s）
  ↓
读 Keychain 取 refresh_token
  ↓
有 → 调 /auth/token/refresh 静默续期
  │     ├─ 成功 → 跳首页
  │     └─ 失败（401/403）→ 跳登录页
  └─ 无 → 跳登录页 / 引导页
```

**关键点**：
- Splash 期间不要让用户看到"已登录的首页 → 闪到登录页"，必须在拿到 token 校验结果后再决定路由
- 若网络超时 → 进入"离线模式"显示缓存数据，而不是直接登出

### 0.4 应用生命周期

| 事件 | 行为 |
|---|---|
| 切到后台 | 记录时间戳；可选：显示遮罩防止 App Switcher 截图泄露 |
| 切回前台 | 距离上次活跃 > N 分钟 → 触发"应用锁"（生物识别/PIN） |
| 网络恢复 | 重试失败请求 |
| 内存警告 | 清理非关键缓存 |

### 0.5 网络层

- 401 拦截器 → 自动 refresh → 重放原请求
- 并发收敛：N 个 401 只发 1 个 refresh（Mutex / Semaphore）
- 弱网友好：超时阶梯（5s → 10s → 30s）+ 离线队列
- HTTPS Pinning（金融/政企必备）：固定证书或公钥；要预留替换通道
- 请求重试：仅对幂等请求（GET / 带幂等 key 的 POST）

### 0.6 推送通知（差异化能力）

| 用途 | 说明 |
|---|---|
| 异地登录告警 | 推送到旧设备：「您的账号刚在 xxx 登录，是不是您？」 |
| 新设备登录确认 | 类似 Apple ID 双向确认 |
| 改密成功通知 | 让用户立刻知道是不是被劫持 |
| 操作 Step-up 二次确认 | App 里点"批准"代替输 TOTP（Push-based MFA） |
| 通知里直接处理 | iOS 通知 Action / Android Notification Action |

### 0.7 多账号管理

App 强需求（Web 弱需求）：

- 切换账号不要求退登再登（保留多份 token）
- "添加账号"入口在设置页
- 切换时清缓存、清 WebView Cookie
- 标记"当前账号"

---

# Part A · C 端用户 App 场景

## A1. 入口：注册 / 登录 / 找回

### A1.1 注册

| 子场景 | App 特点 |
|---|---|
| 手机号注册 | 优先使用，点验证码自动获取焦点 |
| 邮箱注册 | 海外为主；调系统邮箱客户端便于查码 |
| **运营商一键登录** | 中国移动/联通/电信 SDK；体验最佳，秒注册 |
| 短信验证码 | 自动读取（iOS 一次性密码 AutoFill / Android SMS Retriever API） |
| 协议确认 | 必须勾选；监管要求"显著"展示，不能默认勾选 |
| 防作弊 | 设备风控 SDK（友盟/极验）+ 后端二次校验 |

### A1.2 登录

| 方式 | App 实现 |
|---|---|
| 密码登录 | 错误模糊提示 |
| 验证码登录 | 自动填充验证码 |
| **生物识别登录** | 杀手级 → 见 A1.3 |
| 第三方登录 | 走系统 SDK：Google Sign-In / Sign in with Apple / WeChat OpenSDK / QQ |
| **Sign in with Apple** | iOS App Store 强制：只要有第三方登录就必须提供 |
| 扫码登录（扫码端） | 调相机扫 Web 端展示的二维码 → 调 `/auth/qrcode/confirm` |
| 切换历史账号 | 列出已登录过的账号头像 + 一键登录（多账号场景） |

### A1.3 生物识别登录（Touch ID / Face ID / 指纹）

```
首次登录成功后
  → 弹窗询问"是否启用 Face ID 登录"
  → 用户同意
  → 把 refresh_token 用 SecAccessControl(.userPresence) 加密存 Keychain
  → 下次启动 → 系统弹生物识别 UI → 解锁后才能用 token

iOS：
  - LAContext.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics)
  - SecAccessControl + .biometryCurrentSet（指纹/面容增删后失效，安全）

Android：
  - BiometricPrompt API
  - 配合 KeyStore + setUserAuthenticationRequired(true)
```

**注意**：
- 5 次失败 → 退化到密码登录
- 用户改了系统生物特征（重新录指纹）→ 加密 key 失效，需要用密码重新登录后重新启用
- Settings 里要有"关闭生物识别登录"开关

### A1.4 找回密码

- 短信/邮箱验证码 → 重置
- 重置后**所有设备 session 自动作废**（包括当前设备），需要重新登录
- 防爆破：连续失败 N 次后冷却

---

## A2. 会话保活

| 场景 | 实现 |
|---|---|
| 启动续期 | 见 0.3 |
| 401 拦截 → refresh | 全局拦截器；并发收敛 |
| Refresh 失败 | 跳登录页，保留"上次访问的页面" |
| auth_version 变更 | 改密/改 MFA 后强制重登（包括其他设备）|
| 被踢下线 | 显示「账号在其他设备登录」|
| 后台保活 | iOS：BackgroundTasks API 续期；Android：WorkManager；尽量避免后台轮询 |

---

## A3. 账户中心

### A3.1 个人资料

- 头像：相册选 + 拍照 + 裁剪（Crop SDK / 系统 PHPickerViewController）
- 昵称、性别、生日、地区
- 用户名/UID 不可改
- 改资料前先弹密码或生物识别（敏感字段）

### A3.2 联系方式

| 操作 | App 特点 |
|---|---|
| 绑定/换绑手机 | 优先一键登录获取手机号；旧号短信告警 |
| 绑定/换绑邮箱 | 调系统邮件 App 查验证码 |
| 第三方账号 | 列出已绑定的微信/Apple/Google；解绑保护 |

### A3.3 注销账号（监管强要求）

国内 App 必须提供"显著"的注销入口：

```
我的 → 设置 → 账号与安全 → 注销账号

风险提示页 → 输密码 → step-up MFA → 选原因 → 提交
↓
进入冻结期（7~30 天）
↓
冻结期内任意时刻可恢复
↓
冻结期满 → 物理删除
```

---

## A4. 安全中心（Step-up 重灾区）

### A4.1 登录历史与活跃设备

- 列表显示：设备名、型号、地理位置、最后活跃时间
- 标"当前设备"
- 一键踢出其他设备
- 踢出操作触发 step-up

### A4.2 MFA 管理

| 子场景 | 说明 |
|---|---|
| 启用 TOTP | 显示二维码 + secret；引导用户安装 Authenticator |
| **App 自身做 Authenticator** | 高级玩法：本 App 内置 TOTP 生成器（仿 Google Authenticator）|
| 输 6 位验证 | 数字键盘 + 自动跳格 + 整串粘贴 |
| 备份码 | 显示一次 + 复制 + 截图（用户自己存手机相册） |
| 停用 MFA | step-up 拦截 |
| **Push-based MFA** | 用户在 PC 登录 → 手机收到推送 → 点"批准" 即完成 step-up（无需输 6 位） |

### A4.3 应用锁（App-only 概念）

App 独有的本地安全层：

```
设置 → 安全 → 应用锁
  ├─ 关闭
  ├─ 启动时验证（每次冷启动必须）
  ├─ 切回前台 N 分钟后验证
  └─ 仅敏感页面验证（钱包、隐私设置）

验证方式：
  ├─ 仅 PIN
  ├─ 仅生物识别
  └─ 生物识别 + PIN 兜底
```

注意：**应用锁 ≠ 登录态**，是登录之上额外的本地保护层。

### A4.4 Step-up 弹窗

App 端要做的额外功能：

| 场景 | 体验 |
|---|---|
| 已启用生物识别 | 弹生物识别 UI 而不是输 6 位（前提：后端允许 biometric 作为 step-up 因素） |
| 仅 TOTP | 6 位数字 input；自动唤起数字键盘 |
| 短信兜底 | 没 Authenticator 时支持发短信 |
| 备份码 | 提供"用备份码"入口 |
| 推送批准 | 在另一台已信任设备上点"批准"也能完成 step-up |

### A4.5 风控通知

| 通道 | 用途 |
|---|---|
| 推送通知 | 异地登录、新设备、改密、解绑 MFA |
| App 内通知中心 | 历史告警可查 |
| 邮件 | 兜底（推送可能被关） |
| 系统通知 Action | "这是不是我"按钮直接处理（不进 App） |

---

## A5. 权限在 UI 上的体现

App 内权限相对简单（C 端通常无复杂 RBAC），但要做：

- 路由守卫：进 VIP 区前查权限
- 按钮级灰显
- 多角色账号（创作者/普通用户/学生）切换身份

---

## A6. 隐私与合规（App 强要求）

### A6.1 商店审核合规

| 平台 | 要求 |
|---|---|
| **App Store** | 1. 必须有"删除账号"入口（4.5）；2. 有第三方登录必须有 Sign in with Apple；3. 隐私清单 PrivacyInfo.xcprivacy；4. ATT 弹窗（追踪权限）|
| **Google Play** | 1. 数据安全表单；2. 删除账号网页入口（同时支持站外提交）；3. 敏感权限说明 |
| **国内市场** | 1. 隐私政策弹窗（首次启动）；2. 个人信息清单；3. SDK 清单；4. 未成年人保护；5. 防沉迷（游戏）|

### A6.2 系统权限申请

每个权限都要写清"用来做什么"：

| 权限 | 文案示例 | 何时申请 |
|---|---|---|
| 相机 | "用于拍摄头像和扫描二维码登录" | 第一次点击使用时 |
| 相册 | "用于选择头像图片" | 第一次点击时 |
| 通讯录 | "用于查找已注册的好友" | 用户主动选择"找好友"时 |
| 位置 | "用于异地登录风控判断" | 仅登录时模糊位置（不要全程） |
| 推送 | "用于安全告警和重要通知" | 第一次登录后 |
| 麦克风 | 不申请就别申请 | |

**关键原则**：**按需申请**，不要在启动时一把梭。

### A6.3 隐私设置页

- 关闭个性化推荐
- 关闭精准定位
- 关闭通讯录上传
- 数据导出（生成包后邮件发链接，step-up 保护）
- 第三方 SDK 清单（监管要求）

---

## A7. 异常态全清单

| 状态 | App UI |
|---|---|
| 未登录访问受保护页 | 跳登录 + 保留"返回意图" |
| Token 过期 | 静默 refresh |
| Refresh 也过期 | 弹"登录已过期"+ 跳登录 |
| 被踢下线 | 弹窗 + 跳登录 |
| 账号被冻结 | 专属页（不能退到首页）|
| 账号被注销 | 跳注销完成页 |
| 强制改密 | 拦到改密页 |
| 强制启用 MFA | 拦到 MFA 绑定页 |
| 网络异常 | 不要登出，显示"网络异常"+ 重试按钮 |
| 5xx | 通用错误页 |
| **服务端要求强制升级** | 拦截器收到 426 → 拦到强制升级页（App-only）|

---

# Part B · 管理端 App 场景

> 这是个**有意识的小众场景**：B 端管理 App 通常用 Web 后台覆盖；做 App 版本只针对"高优先级、需要随时响应"的能力。
>
> 不要把整个 Web 管理后台搬到 App 上 —— 那是反模式。

## B1. 管理 App 的设计原则

| 原则 | 说明 |
|---|---|
| **只做"应急 + 巡检"场景** | 看告警、批准请求、紧急封号；复杂配置回 Web 做 |
| **最高安全等级** | 强制 MFA + 应用锁 + 短 session（15~30 分钟）+ HTTPS Pinning |
| **离线弱可用** | 看历史数据 OK，操作必须在线 |
| **每个写操作必 step-up** | App 上的误触代价更高 |
| **生物识别替代 step-up** | 操作前 Face ID 一下，UX 比输 6 位顺滑 |

## B2. 管理 App 的核心场景

### B2.1 告警与事件中心

| 功能 | 说明 |
|---|---|
| 推送实时告警 | 风控触发、批量异常登录、系统异常 |
| 告警列表 | 按级别 / 时间 / 模块筛选 |
| 告警详情 | 触发规则、命中用户、相关日志 |
| 一键处置 | 锁号、踢下线、加黑名单、忽略 |
| 告警分派 | 分给某个值班员 |

### B2.2 用户操作（紧急）

| 操作 | 说明 |
|---|---|
| 用户搜索 | UID / 邮箱 / 手机 精确查找 |
| 紧急冻结账号 | 收到客诉/欺诈举报立刻处理 |
| 强制下线 | 一键吊销该用户所有 session |
| 重置 MFA | 客服紧急场景 |
| 解锁账号 | 客服误封解封 |

### B2.3 审批流（关键功能）

管理 App 的杀手锏：**走在路上批审批**。

| 场景 | 说明 |
|---|---|
| 高危操作审批 | 删除租户、批量改密、跨权限边界操作要双人审批 |
| 客服权限申请 | 一线客服申请临时高权限 |
| 数据导出审批 | PII 导出必须主管批 |
| 生效时间窗 | 批准后 15 分钟内有效，过期重申 |
| 通知方式 | 推送 + IM；点通知直接进审批详情 |

### B2.4 巡检看板

| 模块 | 内容 |
|---|---|
| 关键指标 | DAU / 登录成功率 / 风控触发数 |
| 告警热度 | 24h 告警数趋势 |
| 系统健康 | 服务在线率、接口 P99 |
| 当班人员 | 谁在值班、值班记录 |

### B2.5 运行手册（Runbook）

App 内置 SOP：

- 各类告警的处置流程图
- 一键拨打值班电话 / 跳企业 IM 群
- 历史相似事件参考
- 联系上级一键发消息

## B3. 管理 App 不该做的事

| 反模式 | 原因 |
|---|---|
| 复杂的角色/权限配置 | UI 难以承载，回 Web 做 |
| 系统配置编辑 | 长表单 + 副作用大，移动端容易误操作 |
| 大表格导出 | 数据量大、屏幕小、网络不可控 |
| 长流程的运营操作 | 比如配置营销活动 |

---

# Part C · App 独有的能力（写在最后）

| 能力 | 说明 |
|---|---|
| **生物识别登录 / 应用锁** | iOS Face ID / Touch ID；Android BiometricPrompt |
| **运营商一键登录** | 国内特色，体验最佳 |
| **Push-based MFA** | 用推送代替 TOTP 6 位 |
| **推送告警通知** | 异地登录、改密、风控触发 |
| **扫码登录（扫码端）** | 配合 Web 端扫码 |
| **App Switcher 隐私遮罩** | 切到后台时把内容打码避免预览泄露 |
| **越狱/Root 检测** | 高安全场景拒绝运行 |
| **HTTPS Pinning** | 防中间人 |
| **离线模式** | 缓存只读数据 |
| **多账号切换** | 不退登并存 |
| **设备指纹强化** | iOS DeviceCheck / Android SafetyNet/Play Integrity |
| **强制升级** | 服务端 426 → 弹窗强制更新 |
| **NFC / 蓝牙做硬件 MFA** | 高端场景 |

---

## 路由 / 页面清单

### C 端用户 App

```
启动闪屏
  ↓
[已登录] → 主 Tab 容器
[未登录] → 引导/登录

登录注册栈：
  /login            多方式聚合（密码 / 验证码 / 一键登录 / 第三方 / 生物识别）
  /register         注册
  /forgot           找回密码
  /oauth/callback   第三方回调（Universal Link / App Link）

主 Tab 容器：
  首页 / 业务 / 消息 / 我的

我的 → 设置：
  /settings/profile         个人资料
  /settings/account         账号与联系方式
  /settings/security
    ├ /password             改密
    ├ /mfa                  TOTP 管理
    ├ /authenticator        内置 Authenticator（可选）
    ├ /biometric            生物识别开关
    ├ /app-lock             应用锁
    ├ /sessions             活跃设备
    └ /history              登录历史
  /settings/notifications   推送偏好
  /settings/privacy         隐私 / 数据导出 / 注销
  /settings/about           关于 / 隐私政策 / 用户协议 / 第三方 SDK 清单
  /settings/accounts        多账号切换

异常页：
  /frozen / /closed / /force-update
```

### 管理 App

```
/login                 强制 MFA + 生物识别 + IP 校验

主 Tab：
  告警  审批  用户  我的

告警栈：
  /alerts              告警列表
  /alerts/:id          告警详情 + 处置面板
  /alerts/dashboard    巡检看板

审批栈：
  /approvals           待办列表
  /approvals/:id       详情 + 批准/拒绝（生物识别替代 step-up）

用户栈：
  /users/search        紧急搜索
  /users/:id           关键信息 + 应急操作（冻结/踢下线/重置 MFA）

我的：
  /me/runbook          运行手册
  /me/duty             值班记录
  /me/audit-self       我自己的操作审计
  /me/security         应用锁、生物识别、登出
```

---

## 实施分期建议

| 阶段 | C 端 App | 管理 App |
|---|---|---|
| **P0（MVP）** | 注册/登录（密码+验证码）/ 找回密码 / Profile / 启动续期 / Token 拦截器 | （通常先用 Web 后台覆盖，不做 App）|
| **P1** | 一键登录 / 第三方登录 / 改密 / 改邮箱手机 / 登录历史 / 活跃设备 | 推送告警 + 告警列表（只读）|
| **P2** | MFA / Step-up（含生物识别替代）/ 风控推送 / 注销账号 | 紧急操作（冻结/踢下线/重置 MFA）+ 应用锁 + 强制 MFA |
| **P3** | 应用锁 / 多账号 / Push-based MFA / 离线模式 | 审批流 / 巡检看板 / Runbook |
| **P4** | 内置 Authenticator / 越狱检测 / Pinning / 强制升级 | 告警分派 / 离线告警缓存 |

---

## 一句话总结

**C 端 App 的核心命题是「用 OS 原生能力（生物识别 / 推送 / 系统 SDK）把 Web 上需要 5 步的安全操作压成 1 步，同时满足 App Store / Google Play / 国内监管的强制合规」；管理端 App 的核心命题是「让值班的人能在路上 30 秒内完成最紧急的应急操作 + 审批，绝不堆功能」。两者都建立在同一套后端 API 之上，但产品形态、安全等级、信息密度、合规要求与 Web 完全不同。**

> 决策提示：80% 的项目应该先把 Web 用户端 + Web 管理端做扎实，再做 C 端 App，最后再考虑要不要做管理 App。**管理 App 是少数高频值班团队（金融风控、SRE）的奢侈品，不是必需品。**
