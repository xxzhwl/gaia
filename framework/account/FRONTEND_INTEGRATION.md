# 前端（TypeScript / Web / Node / RN）接入 `framework/account` 指南

本文面向使用 TypeScript 的 **Web 前端 / 移动 H5 / Node BFF** 开发者，说明如何对接 `gaia/framework/account` 的 REST API。

服务基础地址：`{ACCOUNT_BASE_URL}/api/v1/account`，例如 `https://account.example.com/api/v1/account`。

---

## 1. 通用约定

### 1.1 通信协议

| 项 | 约定 |
|---|---|
| 请求体 | `application/json; charset=utf-8` |
| 响应体 | `application/json` |
| 鉴权 | `Authorization: Bearer <access_token>` |
| 设备识别 | `X-Device-ID: <浏览器/设备唯一ID>`（强烈推荐，用于风控+会话管理） |
| 客户端 IP | 服务端自动从 Hertz 获取（前端无需传） |
| User-Agent | 服务端自动收集 |

### 1.2 响应结构

成功：

```json
{ "code": 0, "data": { /* 业务数据 */ }, "msg": "" }
```

失败：

```json
{ "code": 401, "msg": "token expired", "data": null }
```

错误码与 HTTP 状态码（详见后端文档第 4 节）对齐：

| code | 含义 | 前端建议处理 |
|---|---|---|
| 400 | 参数错误 | 表单红字提示 |
| 401 | Token 无效/过期/吊销 | 自动调 `/auth/token/refresh`，失败则重定向登录页 |
| 403 | 权限不足 / 需 step-up | 看 `msg`：`stepup_required` 弹 MFA 框；其他显示「无权限」 |
| 409 | 用户名/邮箱/手机已被占用 | 提示并允许换一个 |
| 423 | 账号被锁定 | 显示倒计时 |
| 428 | 必须先绑手机 | 跳绑手机页 |
| 429 | 频控/风控 | 显示等待时间，禁用按钮 |

---

## 2. 推荐的 SDK 骨架（TypeScript）

### 2.1 核心 HTTP 客户端

```ts
// account-client.ts
export interface TokenPair {
  access_token: string;
  refresh_token: string;
  expires_in: number; // seconds
}

export interface ClientOptions {
  baseURL: string;
  getDeviceID: () => string;
  onUnauthorized?: () => void; // 刷新 token 也失败时跳登录
  storage?: TokenStorage;
}

export interface TokenStorage {
  load(): TokenPair | null;
  save(t: TokenPair): void;
  clear(): void;
}

export class AccountClient {
  private opts: ClientOptions;
  private refreshing: Promise<TokenPair | null> | null = null;

  constructor(opts: ClientOptions) {
    this.opts = opts;
  }

  // ============ 通用请求 ============
  async request<T>(
    method: string,
    path: string,
    body?: any,
    opts?: { auth?: boolean; query?: Record<string, any> }
  ): Promise<T> {
    const tokens = this.opts.storage?.load();
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      'X-Device-ID': this.opts.getDeviceID(),
    };
    if (opts?.auth !== false && tokens?.access_token) {
      headers['Authorization'] = `Bearer ${tokens.access_token}`;
    }

    const url = new URL(`${this.opts.baseURL}${path}`);
    if (opts?.query) {
      Object.entries(opts.query).forEach(([k, v]) => v != null && url.searchParams.set(k, String(v)));
    }

    const resp = await fetch(url.toString(), {
      method,
      headers,
      body: body == null ? undefined : JSON.stringify(body),
    });
    const json = await resp.json().catch(() => ({}));

    // 401: 尝试 refresh 一次
    if (resp.status === 401 && opts?.auth !== false && tokens?.refresh_token) {
      const refreshed = await this.refreshOnce();
      if (refreshed) {
        return this.request<T>(method, path, body, { ...opts, auth: true });
      }
      this.opts.storage?.clear();
      this.opts.onUnauthorized?.();
    }

    if (!resp.ok || (json && json.code && json.code !== 0)) {
      throw new AccountError(json.code ?? resp.status, json.msg ?? resp.statusText, json);
    }
    return (json.data ?? json) as T;
  }

  private refreshOnce(): Promise<TokenPair | null> {
    if (this.refreshing) return this.refreshing;
    const tokens = this.opts.storage?.load();
    if (!tokens?.refresh_token) return Promise.resolve(null);
    this.refreshing = this.request<TokenPair>(
      'POST',
      '/auth/token/refresh',
      { refresh_token: tokens.refresh_token },
      { auth: false }
    )
      .then((t) => {
        this.opts.storage?.save(t);
        return t;
      })
      .catch(() => null)
      .finally(() => (this.refreshing = null));
    return this.refreshing;
  }
}

export class AccountError extends Error {
  constructor(public code: number, msg: string, public raw: any) {
    super(msg);
  }
}
```

### 2.2 业务 API 封装

```ts
// account-api.ts
import { AccountClient, TokenPair } from './account-client';

export class AccountAPI {
  constructor(public client: AccountClient) {}

  // ===== 验证码 =====
  sendCode(req: { tenant_id?: string; channel: 'email' | 'sms'; target: string;
                  purpose: 'register' | 'login' | 'reset_password' | 'bind' | 'mfa' }) {
    return this.client.request<{ challenge_id: string; expires_in: number }>(
      'POST', '/verifications/send', req, { auth: false });
  }
  verifyCode(req: { tenant_id?: string; challenge_id: string; code: string;
                    purpose: string; channel: string; target: string }) {
    return this.client.request<void>('POST', '/verifications/verify', req, { auth: false });
  }

  // ===== 注册 / 登录 =====
  register(req: {
    tenant_id?: string;
    username: string; password: string;
    email?: string;
    email_verification_challenge_id?: string; email_verification_code?: string;
    phone?: string;
    phone_verification_challenge_id?: string; phone_verification_code?: string;
    nickname?: string;
  }) {
    return this.client.request<{ user_id: string; tokens: TokenPair }>(
      'POST', '/auth/register', req, { auth: false });
  }

  login(req: { tenant_id?: string; identifier: string;
               identifier_type?: 'auto' | 'username' | 'email' | 'phone';
               password: string }) {
    return this.client.request<LoginResp>('POST', '/auth/login', req, { auth: false });
  }

  loginWithCode(req: { tenant_id?: string; identifier: string;
                       identifier_type?: 'email' | 'phone';
                       challenge_id: string; code: string }) {
    return this.client.request<LoginResp>('POST', '/auth/login/code', req, { auth: false });
  }

  loginOAuth(provider: 'github' | 'google' | 'qq' | 'wechat',
             req: { tenant_id?: string; code: string; redirect_uri: string;
                    state?: string; nonce?: string; code_verifier?: string }) {
    return this.client.request<LoginResp>(
      'POST', `/auth/login/oauth/${provider}`, req, { auth: false });
  }

  bindPhoneAndLogin(req: { tenant_id?: string; identifier: string; password: string;
                           phone: string; challenge_id: string; code: string }) {
    return this.client.request<LoginResp>(
      'POST', '/auth/bind-phone-and-login', req, { auth: false });
  }

  completeMFA(req: { challenge_id: string; code: string }) {
    return this.client.request<LoginResp>('POST', '/auth/mfa/complete', req, { auth: false });
  }

  requestMFACode(channel: 'email' | 'sms') {
    return this.client.request<{ challenge_id: string }>(
      'POST', '/auth/mfa/code', { channel });
  }

  forgotPasswordStart(req: { tenant_id?: string; username?: string; identifier?: string;
                             channel: 'email' | 'sms' }) {
    return this.client.request<{ challenge_id: string }>(
      'POST', '/auth/forgot-password/start', req, { auth: false });
  }
  forgotPasswordComplete(req: { tenant_id?: string; challenge_id: string; code: string;
                                new_password: string }) {
    return this.client.request<void>(
      'POST', '/auth/forgot-password/complete', req, { auth: false });
  }

  refresh(refreshToken: string) {
    return this.client.request<TokenPair>(
      'POST', '/auth/token/refresh', { refresh_token: refreshToken }, { auth: false });
  }

  logout() {
    return this.client.request<void>('POST', '/auth/logout');
  }

  // ===== 当前用户 =====
  me() { return this.client.request<UserInfo>('GET', '/users/me'); }
  updateProfile(req: { nickname?: string; avatar_url?: string }) {
    return this.client.request<UserInfo>('PUT', '/users/me', req);
  }
  changePassword(req: { old_password: string; new_password: string }) {
    return this.client.request<void>('PUT', '/users/password', req);
  }
  deleteAccount() { return this.client.request<void>('DELETE', '/users/me'); }
  myPermissions() {
    return this.client.request<{ user_id: string; tenant_id: string;
                                  roles: string[]; permissions: string[] }>(
      'GET', '/users/me/permissions');
  }
  bindEmail(req: { email: string; challenge_id: string; code: string }) {
    return this.client.request<void>('POST', '/users/me/bind-email', req);
  }
  bindPhone(req: { phone: string; challenge_id: string; code: string }) {
    return this.client.request<void>('POST', '/users/me/bind-phone', req);
  }
  myOAuthAccounts() {
    return this.client.request<OAuthAccount[]>('GET', '/users/me/oauth-accounts');
  }
  bindOAuth(provider: string, req: { code: string; redirect_uri: string;
                                     state?: string; nonce?: string; code_verifier?: string }) {
    return this.client.request<void>('POST', `/users/me/oauth-accounts/${provider}/bind`, req);
  }
  unbindOAuth(provider: string) {
    return this.client.request<void>('DELETE', `/users/me/oauth-accounts/${provider}`);
  }
  myOrgs() { return this.client.request<Org[]>('GET', '/users/me/orgs'); }

  // ===== MFA =====
  setupTOTP() {
    return this.client.request<{ secret: string; otpauth_url: string; qr_code: string }>(
      'POST', '/mfa/totp/setup');
  }
  verifyTOTP(code: string) {
    return this.client.request<{ ok: boolean }>('POST', '/mfa/totp/verify', { code });
  }
  disableTOTP() { return this.client.request<void>('DELETE', '/mfa/totp'); }
  stepUpStart() {
    return this.client.request<{ challenge_id: string }>('POST', '/mfa/step-up/start');
  }
  stepUpComplete(req: { challenge_id: string; code: string }) {
    return this.client.request<void>('POST', '/mfa/step-up/complete', req);
  }

  // ===== 会话 =====
  listSessions() { return this.client.request<Session[]>('GET', '/sessions'); }
  revokeSession(id: string) {
    return this.client.request<void>('DELETE', `/sessions/${id}`);
  }
  revokeOtherSessions() {
    return this.client.request<{ revoked: number }>('POST', '/sessions/revoke-others');
  }

  // ===== 组织 =====
  listOrgs() { return this.client.request<Org[]>('GET', '/orgs'); }
  orgTree() { return this.client.request<OrgNode[]>('GET', '/orgs/tree'); }
  org(id: string) { return this.client.request<Org>('GET', `/orgs/${id}`); }
}

// ===== 类型定义 =====
export interface LoginResp {
  user_id?: string;
  tokens?: TokenPair;
  mfa_required?: boolean;
  mfa_challenge_id?: string;
  mfa_methods?: ('totp' | 'email' | 'sms')[];
}
export interface UserInfo {
  id: string; tenant_id: string;
  username: string; nickname: string;
  email: string; email_verified: boolean;
  phone: string; phone_verified: boolean;
  avatar_url: string;
  roles: string[];
  status: string;
}
export interface OAuthAccount { provider: string; provider_uid: string; bound_at: string }
export interface Session { id: string; device_id: string; ip: string; user_agent: string;
  created_at: string; last_seen_at: string; current?: boolean }
export interface Org { id: string; name: string; parent_id?: string }
export interface OrgNode extends Org { children: OrgNode[] }
```

### 2.3 实例化

```ts
// app-bootstrap.ts
import { AccountClient } from './account-client';
import { AccountAPI } from './account-api';

const storage = {
  load: () => {
    const raw = localStorage.getItem('acct.tokens');
    return raw ? JSON.parse(raw) : null;
  },
  save: (t) => localStorage.setItem('acct.tokens', JSON.stringify(t)),
  clear: () => localStorage.removeItem('acct.tokens'),
};

const client = new AccountClient({
  baseURL: import.meta.env.VITE_ACCOUNT_BASE_URL + '/api/v1/account',
  getDeviceID: () => getOrCreateDeviceID(), // 自己实现：localStorage 持久化的 UUID
  storage,
  onUnauthorized: () => location.assign('/login'),
});

export const account = new AccountAPI(client);
```

---

## 3. 关键流程

### 3.1 注册（带邮箱验证）

```ts
// 步骤 1：发送邮箱验证码
const { challenge_id } = await account.sendCode({
  channel: 'email', target: 'alice@example.com', purpose: 'register',
});

// 步骤 2：用户输入验证码后，提交注册
await account.register({
  username: 'alice',
  password: 'Pa$$w0rd-12345',
  email: 'alice@example.com',
  email_verification_challenge_id: challenge_id,
  email_verification_code: userInputCode,
  nickname: 'Alice',
});
```

> 如果服务端配置了 `RequireVerifiedPhone=true`，注册必须额外带 `phone_verification_*`。

### 3.2 登录（含 MFA）

```ts
const resp = await account.login({
  identifier: 'alice', password: 'Pa$$w0rd-12345',
});

if (resp.mfa_required) {
  // 弹 MFA 输入框，让用户选 TOTP / 邮箱 / 短信
  // - TOTP：用户直接输入 authenticator app 的 6 位码
  // - 邮箱/短信：先调 requestMFACode（注意：这里需要登录态？不，未登录走 /auth/mfa/code 不存在）
  //   实际上 MFA 通道选择由 challenge 决定，前端按 mfa_methods 提示选择即可
  const final = await account.completeMFA({
    challenge_id: resp.mfa_challenge_id!,
    code: userInputMFACode,
  });
  storage.save(final.tokens!);
} else {
  storage.save(resp.tokens!);
}
```

### 3.3 第三方登录（GitHub 示例）

```ts
// 1) 跳转到 GitHub 授权页
const state = crypto.randomUUID();
sessionStorage.setItem('oauth.state', state);
location.assign(
  `https://github.com/login/oauth/authorize?client_id=${CLIENT_ID}` +
  `&redirect_uri=${encodeURIComponent(REDIRECT_URI)}` +
  `&scope=read:user user:email&state=${state}`
);

// 2) 回调页 /oauth/callback?code=xxx&state=yyy
const code = new URL(location.href).searchParams.get('code')!;
const state2 = new URL(location.href).searchParams.get('state');
if (state2 !== sessionStorage.getItem('oauth.state')) throw new Error('state mismatch');

const resp = await account.loginOAuth('github', { code, redirect_uri: REDIRECT_URI });

if (resp.mfa_required) { /* 同上 */ }
else if ((resp as any).phone_binding_required) {
  // 服务端策略要求绑手机才能登录
  // 跳到绑手机页，让用户输入手机号 + 验证码后调 bindPhoneAndLogin
}
else storage.save(resp.tokens!);
```

### 3.4 忘记密码

```ts
// 步骤 1：发送重置验证码
const { challenge_id } = await account.forgotPasswordStart({
  username: 'alice', channel: 'email',
});

// 步骤 2：用户输入验证码 + 新密码
await account.forgotPasswordComplete({
  challenge_id,
  code: userInputCode,
  new_password: 'NewPa$$w0rd-67890',
});
```

### 3.5 敏感操作 + Step-up MFA

服务端会对**改密、解绑 MFA、改邮箱手机、分配高权限角色**等操作要求 step-up。前端只需要捕获 `403 + msg=stepup_required`：

```ts
async function changePasswordWithStepUp(oldPwd: string, newPwd: string) {
  try {
    await account.changePassword({ old_password: oldPwd, new_password: newPwd });
  } catch (e) {
    if (e instanceof AccountError && e.code === 403 && e.raw?.reason === 'stepup_required') {
      // 弹 MFA 输入框
      const { challenge_id } = await account.stepUpStart();
      const code = await promptUserMFACode();
      await account.stepUpComplete({ challenge_id, code });
      // 5 分钟内重试敏感操作
      await account.changePassword({ old_password: oldPwd, new_password: newPwd });
    } else {
      throw e;
    }
  }
}
```

### 3.6 启用 TOTP（Authenticator App）

```ts
// 1) 拿到 secret + otpauth_url（含 QR code 数据）
const setup = await account.setupTOTP();
showQRCode(setup.qr_code); // 用户用 Google/Microsoft Authenticator 扫码

// 2) 用户输入第一个 6 位码完成绑定
const { ok } = await account.verifyTOTP(userInputCode);
if (!ok) throw new Error('验证码错误');
```

### 3.7 设备管理（"踢掉其他设备"）

```ts
const sessions = await account.listSessions();
// 渲染列表（IP、设备、登录时间），current=true 标记当前设备
await account.revokeOtherSessions(); // 一键踢掉所有其它设备
await account.revokeSession(sessionId); // 单个吊销
```

---

## 4. 错误处理通用范式

```ts
import { AccountError } from './account-client';

async function safeCall<T>(fn: () => Promise<T>): Promise<T | null> {
  try { return await fn(); }
  catch (e) {
    if (!(e instanceof AccountError)) { showToast('网络错误'); return null; }
    switch (e.code) {
      case 400: showFieldError(e.msg); break;
      case 401: location.assign('/login'); break;
      case 403:
        if (e.raw?.reason === 'stepup_required') openStepUpDialog();
        else showToast('权限不足');
        break;
      case 409: showToast('用户名/邮箱/手机已被占用'); break;
      case 423: showToast('账号已被锁定，请稍后再试'); break;
      case 428: location.assign('/bind-phone'); break;
      case 429: showToast('操作过于频繁'); break;
      default: showToast(e.msg);
    }
    return null;
  }
}
```

---

## 5. 移动端 / Node BFF 注意事项

### 5.1 React Native

- `getDeviceID` 用 `react-native-device-info` 的 `getUniqueId()`，固定写入 Keychain。
- token 用 `react-native-keychain` 保存（比 AsyncStorage 安全）。
- 不要使用 cookie；只走 Bearer。

### 5.2 Node 作为 BFF

如果你的架构是 `Browser → Node BFF → Account Service`：

- BFF 持有用户的 access/refresh token（推荐放 httpOnly cookie + 服务端会话存储）。
- 所有调用走 Node fetch，把 `X-Device-ID` 设成 BFF 自己持久化的会话设备 ID。
- 校验 Token 时，BFF 可以**不调用 account 服务**，而是直接用 `JWKS`（`GET /oauth/certs`）拿公钥本地校验，性能最好。

```ts
import { jwtVerify, createRemoteJWKSet } from 'jose';
const JWKS = createRemoteJWKSet(new URL(`${ACCOUNT_BASE_URL}/api/v1/account/oauth/certs`));

async function verifyToken(token: string) {
  const { payload } = await jwtVerify(token, JWKS, {
    issuer: 'gaia-account', audience: 'gaia-account',
  });
  return payload; // 包含 sub(user_id), tenant_id, roles 等
}
```

### 5.3 SSR / Next.js

- 登录页走 Client Component + 表单 submit，把 token 写到 httpOnly cookie（通过 Next API Route 中转，**不要把 token 暴露给浏览器 JS**）。
- 鉴权页面在 `getServerSideProps` / Server Component 里读 cookie + 用 JWKS 校验。

---

## 6. 安全清单

- ✅ Access Token 不放 localStorage（XSS 风险）。Web 推荐 **内存 + sessionStorage** 短驻；移动端用 Keychain。
- ✅ Refresh Token **必须** httpOnly cookie 或安全存储；本文示例为简洁放 localStorage，**生产慎用**。
- ✅ 所有第三方登录回调强制校验 `state`，并使用 PKCE（`code_verifier`）。
- ✅ `X-Device-ID` 浏览器侧用 `crypto.randomUUID()` 持久化（用户清缓存即视为新设备）。
- ✅ 任何敏感操作（改密、解绑 MFA、删除账号）前要求重新输入密码或 step-up MFA。
- ✅ HTTPS only；尽早开启 HSTS。

---

## 7. 完整 REST API 路径速查

> 详见 `standalone.go` / `standalone_admin.go`，全部以 `/api/v1/account` 为前缀。

### 7.1 公共 / 终端用户

```
GET    /health
POST   /verifications/send
POST   /verifications/verify
POST   /auth/register
POST   /auth/login
POST   /auth/login/code
POST   /auth/login/oauth/:provider
POST   /auth/bind-phone-and-login
POST   /auth/mfa/complete
POST   /auth/mfa/code                    [Auth]
POST   /auth/forgot-password/start
POST   /auth/forgot-password/complete
POST   /auth/token/refresh
POST   /auth/logout                      [Auth]
GET    /users/me                         [Auth]
PUT    /users/me                         [Auth]
PUT    /users/password                   [Auth, may step-up]
DELETE /users/me                         [Auth]
GET    /users/me/permissions             [Auth]
POST   /users/me/bind-email              [Auth]
POST   /users/me/bind-phone              [Auth]
GET    /users/me/oauth-accounts          [Auth]
POST   /users/me/oauth-accounts/:provider/bind   [Auth]
DELETE /users/me/oauth-accounts/:provider        [Auth]
GET    /users/me/orgs                    [Auth]
POST   /mfa/totp/setup                   [Auth]
POST   /mfa/totp/verify                  [Auth]
DELETE /mfa/totp                         [Auth, may step-up]
POST   /mfa/step-up/start                [Auth]
POST   /mfa/step-up/complete             [Auth]
GET    /sessions                         [Auth]
DELETE /sessions/:id                     [Auth]
POST   /sessions/revoke-others           [Auth]
GET    /orgs                             [Auth]
GET    /orgs/tree                        [Auth]
GET    /orgs/:id                         [Auth]
GET    /orgs/:id/ancestors               [Auth]
GET    /orgs/:id/members                 [Auth]
POST   /passkey/register/start           [Auth]
POST   /passkey/register/complete        [Auth]
POST   /passkey/auth/start
POST   /passkey/auth/complete
GET    /passkey/credentials              [Auth]
DELETE /passkey/credentials/:id          [Auth]
GET    /audit                            [Auth]
```

### 7.2 OIDC 端点

```
GET    /.well-known/openid-configuration
GET    /oauth/certs
GET    /oauth/authorize                  [Auth]
GET    /oauth/userinfo                   [Auth]
POST   /oauth/revoke
POST   /oauth/introspect
POST   /idp/clients/token                # OIDC token 端点（含 client_credentials）
```

### 7.3 管理端 `/admin/*`

需要 `admin.<resource>.<action>` 权限码，前端管理后台在 `account.myPermissions()` 返回中可校验。完整列表见 `standalone_admin.go`。

---

完整后端配置说明见 [`BACKEND_INTEGRATION.md`](./BACKEND_INTEGRATION.md)。
