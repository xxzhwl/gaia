# Gaia 账号服务体系

基于 Gaia 框架的完整账号服务体系，提供用户管理、角色权限、Token管理、验证码、第三方登录等完整功能。

## 功能特性

- ✅ 用户注册、登录、信息管理
- ✅ 双Token JWT认证机制
- ✅ 角色和权限管理
- ✅ 验证码服务
- ✅ 第三方登录（微信、QQ、GitHub等）
- ✅ 完整的日志记录（登录日志、操作日志）
- ✅ 数据库自动迁移和初始化
- ✅ 健康检查和统计信息

## 快速开始

### 1. 初始化账号服务管理器

```go
import (
    "github.com/xxzhwl/gaia/framework/server/accountService"
)

// 方式1：使用现有数据库连接
manager, err := accountService.NewAccountManager(db)
if err != nil {
    // 处理错误
}

// 方式2：根据配置schema创建
manager, err := accountService.NewAccountManagerWithSchema("Account.Mysql")
if err != nil {
    // 处理错误
}
```

### 2. 初始化数据库

```go
// 自动创建表和初始化默认数据
if err := manager.InitDatabase(); err != nil {
    // 处理错误
}
```

### 3. 用户注册

```go
userService := manager.GetUserService()

registerReq := &accountService.RegisterRequest{
    Username: "testuser",
    Password: "password123",
    Email:    "test@example.com",
    Phone:    "13800138000",
    Nickname: "测试用户",
}

user, err := userService.Register(registerReq)
if err != nil {
    // 处理错误
}
```

### 4. 用户登录

```go
loginReq := &accountService.LoginRequest{
    Username: "testuser",
    Password: "password123",
    LoginType: "password",
    DeviceID: "device123",
}

user, tokenResp, err := userService.Login(loginReq, "127.0.0.1", "Mozilla/5.0...")
if err != nil {
    // 处理错误
}

// 返回的Token响应
fmt.Printf("Access Token: %s\n", tokenResp.AccessToken)
fmt.Printf("Refresh Token: %s\n", tokenResp.RefreshToken)
```

### 5. Token验证和刷新

```go
tokenService := manager.GetTokenService()

// 验证访问Token
claims, err := tokenService.ValidateAccessToken(accessToken)
if err != nil {
    // Token无效或过期
}

// 刷新Token
newTokenResp, err := tokenService.RefreshToken(refreshToken, "device123")
if err != nil {
    // 刷新失败
}
```

### 6. 权限检查

```go
roleService := manager.GetRoleService()

// 检查用户是否有某个权限
hasPermission, err := roleService.CheckUserPermission(userID, "system:user:view")
if err != nil {
    // 处理错误
}

if hasPermission {
    // 用户有权限
} else {
    // 用户没有权限
}
```

## 服务组件

### UserService - 用户服务
- 用户注册、登录、信息查询
- 密码修改、资料更新
- 用户状态管理

### RoleService - 角色权限服务
- 角色创建、修改、删除
- 权限管理
- 用户角色分配
- 权限树形结构管理

### TokenService - Token服务
- 双Token生成和验证
- Token刷新机制
- Token吊销管理
- 过期Token清理

### VerificationService - 验证码服务
- 验证码生成和验证
- 防刷机制
- 多种验证码类型支持

### OAuthService - 第三方登录服务
- 第三方账号绑定和解绑
- 第三方登录支持
- 多平台支持（微信、QQ、GitHub等）

### LogService - 日志服务
- 登录日志记录
- 操作日志记录
- 日志统计和分析

## 配置说明

### JWT配置
在配置文件中添加JWT相关配置：

```yaml
JWT:
  SecretKey: "your-jwt-secret-key"
  Issuer: "your-app-name"
  AccessTokenExp: 1440  # 访问Token过期时间（分钟）
  RefreshTokenExp: 43200 # 刷新Token过期时间（分钟）
```

### 数据库配置
```yaml
Account:
  Mysql: "username:password@tcp(localhost:3306)/account_system?charset=utf8mb4&parseTime=True&loc=Local"
```

## 数据结构

### 用户状态
- `0` - 禁用
- `1` - 正常
- `2` - 待验证
- `3` - 锁定

### 登录类型
- `password` - 密码登录
- `sms` - 短信登录
- `oauth` - 第三方登录

### 权限类型
- `menu` - 菜单权限
- `button` - 按钮权限
- `api` - API权限

## 错误处理

所有服务方法都使用标准的错误处理机制，返回的错误已包含适当的HTTP状态码：

```go
import "github.com/xxzhwl/gaia/errwrap"

// 获取错误码
code := errwrap.GetCode(err)
// 获取错误信息
message := err.Error()
```

## 最佳实践

### 1. 定期清理
```go
// 定期清理过期数据
go func() {
    ticker := time.NewTicker(24 * time.Hour)
    defer ticker.Stop()
    
    for range ticker.C {
        manager.Cleanup()
    }
}()
```

### 2. 健康检查
```go
// 定期健康检查
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for range ticker.C {
        if _, err := manager.HealthCheck(); err != nil {
            // 发送告警
        }
    }
}()
```

### 3. 权限验证中间件
```go
func AuthMiddleware(permissionCode string) app.HandlerFunc {
    return server.MakePlugin(func(arg server.Request) error {
        // 从JWT中获取用户信息
        claims, exists := arg.GetUserInfo()
        if !exists {
            return errwrap.Error(401, errors.New("未授权"))
        }
        
        // 检查权限
        roleService := manager.GetRoleService()
        hasPermission, err := roleService.CheckUserPermission(claims.UserID, permissionCode)
        if err != nil {
            return errwrap.Error(500, err)
        }
        
        if !hasPermission {
            return errwrap.Error(403, errors.New("权限不足"))
        }
        
        return nil
    })
}
```

## 注意事项

1. **安全性**: 生产环境务必修改默认的JWT密钥
2. **性能**: 大量用户时注意数据库索引优化
3. **监控**: 建议实现日志监控和告警机制
4. **备份**: 定期备份用户数据和日志

## 扩展开发

可以根据业务需求扩展服务功能，例如：
- 添加多因子认证
- 实现密码策略管理
- 增加用户行为分析
- 集成更多的第三方登录平台