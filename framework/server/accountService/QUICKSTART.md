# Gaia 账户体系快速接入指南

本文档帮助上层应用快速接入 Gaia 框架的账户服务体系。

## 一、接入步骤

### 步骤 1：配置数据库和 JWT

在 `config.json` 中添加配置：

```json
{
  "Account": {
    "Mysql": "root:password@tcp(localhost:3306)/account_db?charset=utf8mb4&parseTime=True&loc=Local"
  },
  "JWT": {
    "SecretKey": "your-secret-key-min-32-characters-long",
    "Issuer": "your-app-name",
    "AccessTokenExp": 1440,
    "RefreshTokenExp": 43200
  }
}
```

### 步骤 2：初始化账户服务

在应用启动时初始化：

```go
package main

import (
    "github.com/xxzhwl/gaia/framework/server/accountService"
)

var AccountManager *accountService.AccountManager

func init() {
    // 方式1：使用配置中的 Schema
    manager, err := accountService.NewAccountManagerWithSchema("Account.Mysql")
    if err != nil {
        panic("账户服务初始化失败: " + err.Error())
    }
    
    // 方式2：使用现有数据库连接
    // db := gaia.NewFrameworkMysql()
    // manager, err := accountService.NewAccountManager(db.GetGormDb())
    
    AccountManager = manager
    
    // 自动迁移数据库（首次运行）
    if err := manager.InitDatabase(); err != nil {
        panic("数据库初始化失败: " + err.Error())
    }
}
```

### 步骤 3：创建 API 控制器

创建 `controller/account_controller.go`：

```go
package controller

import (
    "errors"
    
    "github.com/cloudwego/hertz/pkg/app"
    "github.com/xxzhwl/gaia"
    "github.com/xxzhwl/gaia/errwrap"
    "github.com/xxzhwl/gaia/framework/server"
    "github.com/xxzhwl/gaia/framework/server/accountService"
)

type AccountCtrl struct{}

// ========== 用户相关接口 ==========

// RegisterRequest 注册请求
type RegisterRequest struct {
    Username string `json:"username" memo:"用户名" require:"1"`
    Password string `json:"password" memo:"密码" require:"1"`
    Email    string `json:"email" memo:"邮箱" validator:"Mail"`
    Phone    string `json:"phone" memo:"手机号"`
    Nickname string `json:"nickname" memo:"昵称"`
}

// Register 用户注册
func (a *AccountCtrl) Register() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        req := &RegisterRequest{}
        if err := arg.BindJsonWithChecker(req); err != nil {
            return nil, err
        }
        
        registerReq := accountService.RegisterRequest{
            Username: req.Username,
            Password: req.Password,
            Email:    req.Email,
            Phone:    req.Phone,
            Nickname: req.Nickname,
        }
        
        user, err := AccountManager.GetUserService().Register(registerReq)
        if err != nil {
            return nil, err
        }
        
        return map[string]any{
            "id":       user.ID,
            "uuid":     user.UUID,
            "username": user.Username,
        }, nil
    })
}

// LoginRequest 登录请求
type LoginRequest struct {
    Username string `json:"username" memo:"用户名/邮箱/手机号" require:"1"`
    Password string `json:"password" memo:"密码" require:"1"`
    DeviceID string `json:"device_id" memo:"设备ID"`
}

// Login 用户登录
func (a *AccountCtrl) Login() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        req := &LoginRequest{}
        if err := arg.BindJsonWithChecker(req); err != nil {
            return nil, err
        }
        
        // 获取客户端信息
        ip := arg.GetClientIP()
        userAgent := string(arg.GetUserAgent())
        
        loginReq := accountService.LoginRequest{
            Username:  req.Username,
            Password:  req.Password,
            LoginType: accountService.LoginTypePassword,
            DeviceID:  req.DeviceID,
        }
        
        user, tokenResp, err := AccountManager.GetUserService().Login(loginReq, ip, userAgent)
        if err != nil {
            return nil, err
        }
        
        return map[string]any{
            "user": map[string]any{
                "id":       user.ID,
                "uuid":     user.UUID,
                "username": user.Username,
                "nickname": user.Nickname,
            },
            "token": map[string]any{
                "access_token":  tokenResp.AccessToken,
                "refresh_token": tokenResp.RefreshToken,
                "expires_at":    tokenResp.ExpiresAt,
                "token_type":    tokenResp.TokenType,
            },
        }, nil
    })
}

// RefreshTokenRequest 刷新Token请求
type RefreshTokenRequest struct {
    RefreshToken string `json:"refresh_token" memo:"刷新Token" require:"1"`
    DeviceID     string `json:"device_id" memo:"设备ID"`
}

// RefreshToken 刷新访问Token
func (a *AccountCtrl) RefreshToken() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        req := &RefreshTokenRequest{}
        if err := arg.BindJsonWithChecker(req); err != nil {
            return nil, err
        }
        
        tokenResp, err := AccountManager.GetTokenService().RefreshToken(
            req.RefreshToken, 
            req.DeviceID,
        )
        if err != nil {
            return nil, err
        }
        
        return map[string]any{
            "access_token":  tokenResp.AccessToken,
            "refresh_token": tokenResp.RefreshToken,
            "expires_at":    tokenResp.ExpiresAt,
            "token_type":    tokenResp.TokenType,
        }, nil
    })
}

// GetUserInfo 获取当前用户信息
func (a *AccountCtrl) GetUserInfo() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        // 从 JWT 获取用户ID
        claims, exists := arg.GetUserInfo()
        if !exists {
            return nil, errwrap.Error(401, errors.New("未登录"))
        }
        
        userInfo, err := AccountManager.GetUserService().GetUserByID(claims.UserID)
        if err != nil {
            return nil, err
        }
        
        return userInfo, nil
    })
}

// UpdateProfileRequest 更新资料请求
type UpdateProfileRequest struct {
    Nickname string `json:"nickname" memo:"昵称"`
    Email    string `json:"email" memo:"邮箱" validator:"Mail"`
    Phone    string `json:"phone" memo:"手机号"`
}

// UpdateProfile 更新用户资料
func (a *AccountCtrl) UpdateProfile() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        req := &UpdateProfileRequest{}
        if err := arg.BindJsonWithChecker(req); err != nil {
            return nil, err
        }
        
        claims, exists := arg.GetUserInfo()
        if !exists {
            return nil, errwrap.Error(401, errors.New("未登录"))
        }
        
        updates := map[string]any{}
        if req.Nickname != "" {
            updates["nickname"] = req.Nickname
        }
        if req.Email != "" {
            updates["email"] = req.Email
        }
        if req.Phone != "" {
            updates["phone"] = req.Phone
        }
        
        if err := AccountManager.GetUserService().UpdateUserProfile(claims.UserID, updates); err != nil {
            return nil, err
        }
        
        return map[string]any{"message": "更新成功"}, nil
    })
}

// ChangePasswordRequest 修改密码请求
type ChangePasswordRequest struct {
    OldPassword string `json:"old_password" memo:"旧密码" require:"1"`
    NewPassword string `json:"new_password" memo:"新密码" require:"1"`
}

// ChangePassword 修改密码
func (a *AccountCtrl) ChangePassword() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        req := &ChangePasswordRequest{}
        if err := arg.BindJsonWithChecker(req); err != nil {
            return nil, err
        }
        
        claims, exists := arg.GetUserInfo()
        if !exists {
            return nil, errwrap.Error(401, errors.New("未登录"))
        }
        
        if err := AccountManager.GetUserService().ChangePassword(
            claims.UserID, 
            req.OldPassword, 
            req.NewPassword,
        ); err != nil {
            return nil, err
        }
        
        return map[string]any{"message": "密码修改成功"}, nil
    })
}

// Logout 用户登出
func (a *AccountCtrl) Logout() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        claims, exists := arg.GetUserInfo()
        if !exists {
            return nil, errwrap.Error(401, errors.New("未登录"))
        }
        
        // 吊销用户所有Token
        if err := AccountManager.GetTokenService().RevokeUserTokens(claims.UserID); err != nil {
            return nil, err
        }
        
        return map[string]any{"message": "登出成功"}, nil
    })
}

// ========== 角色权限相关接口（需管理员权限） ==========

// CreateRoleRequest 创建角色请求
type CreateRoleRequest struct {
    Name        string `json:"name" memo:"角色名称" require:"1"`
    Code        string `json:"code" memo:"角色代码" require:"1"`
    Description string `json:"description" memo:"描述"`
}

// CreateRole 创建角色
func (a *AccountCtrl) CreateRole() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        req := &CreateRoleRequest{}
        if err := arg.BindJsonWithChecker(req); err != nil {
            return nil, err
        }
        
        createReq := &accountService.CreateRoleRequest{
            Name:        req.Name,
            Code:        req.Code,
            Description: req.Description,
        }
        
        role, err := AccountManager.GetRoleService().CreateRole(createReq)
        if err != nil {
            return nil, err
        }
        
        return role, nil
    })
}

// GetRoleList 获取角色列表
func (a *AccountCtrl) GetRoleList() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        page := arg.GetUrlQueryInt("page", 1)
        pageSize := arg.GetUrlQueryInt("page_size", 20)
        
        roles, total, err := AccountManager.GetRoleService().GetRoleList(page, pageSize)
        if err != nil {
            return nil, err
        }
        
        return map[string]any{
            "list":  roles,
            "total": total,
        }, nil
    })
}

// GetPermissionTree 获取权限树
func (a *AccountCtrl) GetPermissionTree() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        tree, err := AccountManager.GetRoleService().GetPermissionTree()
        if err != nil {
            return nil, err
        }
        
        return tree, nil
    })
}

// AssignUserRoleRequest 分配角色请求
type AssignUserRoleRequest struct {
    UserID int64 `json:"user_id" memo:"用户ID" require:"1"`
    RoleID int64 `json:"role_id" memo:"角色ID" require:"1"`
}

// AssignUserRole 为用户分配角色
func (a *AccountCtrl) AssignUserRole() app.HandlerFunc {
    return server.MakeHandler(func(arg server.Request) (any, error) {
        req := &AssignUserRoleRequest{}
        if err := arg.BindJsonWithChecker(req); err != nil {
            return nil, err
        }
        
        if err := AccountManager.GetRoleService().AssignUserRole(req.UserID, req.RoleID); err != nil {
            return nil, err
        }
        
        return map[string]any{"message": "分配成功"}, nil
    })
}
```

### 步骤 4：注册路由

在路由文件中注册：

```go
package router

import (
    "github.com/cloudwego/hertz/pkg/app/server"
    "your-app/controller"
    gaiaServer "github.com/xxzhwl/gaia/framework/server"
)

func RegisterRoutes(h *server.Hertz) {
    accountCtrl := &controller.AccountCtrl{}
    
    // 公开接口（无需登录）
    public := h.Group("/api/v1/account")
    {
        public.POST("/register", accountCtrl.Register())
        public.POST("/login", accountCtrl.Login())
        public.POST("/refresh", accountCtrl.RefreshToken())
    }
    
    // 需要登录的接口
    auth := h.Group("/api/v1/account", gaiaServer.AuthMiddleware())
    {
        auth.GET("/user/info", accountCtrl.GetUserInfo())
        auth.PUT("/user/profile", accountCtrl.UpdateProfile())
        auth.PUT("/user/password", accountCtrl.ChangePassword())
        auth.POST("/logout", accountCtrl.Logout())
    }
    
    // 管理员接口（需要权限检查）
    admin := h.Group("/api/v1/admin", 
        gaiaServer.AuthMiddleware(),
        PermissionMiddleware("system:manage"),
    )
    {
        admin.POST("/roles", accountCtrl.CreateRole())
        admin.GET("/roles", accountCtrl.GetRoleList())
        admin.GET("/permissions", accountCtrl.GetPermissionTree())
        admin.POST("/user/role", accountCtrl.AssignUserRole())
    }
}
```

### 步骤 5：创建权限验证中间件

创建 `middleware/permission.go`：

```go
package middleware

import (
    "errors"
    
    "github.com/cloudwego/hertz/pkg/app"
    "github.com/xxzhwl/gaia/errwrap"
    "github.com/xxzhwl/gaia/framework/server"
    "your-app/controller"
)

// PermissionMiddleware 权限验证中间件
func PermissionMiddleware(permissionCode string) app.HandlerFunc {
    return server.MakePlugin(func(arg server.Request) error {
        // 获取当前用户信息
        claims, exists := arg.GetUserInfo()
        if !exists {
            return errwrap.Error(401, errors.New("未登录"))
        }
        
        // 检查权限
        hasPermission, err := controller.AccountManager.GetRoleService().CheckUserPermission(
            claims.UserID, 
            permissionCode,
        )
        if err != nil {
            return errwrap.Error(500, err)
        }
        
        if !hasPermission {
            return errwrap.Error(403, errors.New("权限不足"))
        }
        
        return nil
    })
}

// RoleMiddleware 角色验证中间件
func RoleMiddleware(roleCode string) app.HandlerFunc {
    return server.MakePlugin(func(arg server.Request) error {
        claims, exists := arg.GetUserInfo()
        if !exists {
            return errwrap.Error(401, errors.New("未登录"))
        }
        
        // 获取用户角色
        roles, err := controller.AccountManager.GetRoleService().GetUserRoles(claims.UserID)
        if err != nil {
            return errwrap.Error(500, err)
        }
        
        // 检查是否包含指定角色
        for _, role := range roles {
            if role.Code == roleCode {
                return nil
            }
        }
        
        return errwrap.Error(403, errors.New("需要 "+roleCode+" 角色"))
    })
}
```

### 步骤 6：后台定时任务（可选）

创建 `task/cleanup.go`：

```go
package task

import (
    "time"
    
    "your-app/controller"
    "github.com/xxzhwl/gaia"
)

// StartCleanupTask 启动清理任务
func StartCleanupTask() {
    go func() {
        ticker := time.NewTicker(24 * time.Hour)
        defer ticker.Stop()
        
        for range ticker.C {
            gaia.Info("开始执行账户数据清理任务")
            
            // 清理过期Token
            count, err := controller.AccountManager.GetTokenService().CleanExpiredTokens()
            if err != nil {
                gaia.ErrorF("清理过期Token失败: %v", err)
            } else {
                gaia.InfoF("清理 %d 个过期Token", count)
            }
            
            // 清理过期验证码
            count, err = controller.AccountManager.GetVerificationService().CleanExpiredCodes()
            if err != nil {
                gaia.ErrorF("清理过期验证码失败: %v", err)
            } else {
                gaia.InfoF("清理 %d 个过期验证码", count)
            }
            
            // 执行整体清理
            if err := controller.AccountManager.Cleanup(); err != nil {
                gaia.ErrorF("账户清理失败: %v", err)
            }
        }
    }()
}
```

在 `main.go` 中启动：

```go
func main() {
    // ... 其他初始化 ...
    
    // 启动定时清理任务
    task.StartCleanupTask()
    
    // ... 启动服务 ...
}
```

---

## 二、API 接口列表

### 公开接口

| 接口 | 方法 | 描述 |
|------|------|------|
| `/api/v1/account/register` | POST | 用户注册 |
| `/api/v1/account/login` | POST | 用户登录 |
| `/api/v1/account/refresh` | POST | 刷新Token |

### 需要登录

| 接口 | 方法 | 描述 |
|------|------|------|
| `/api/v1/account/user/info` | GET | 获取用户信息 |
| `/api/v1/account/user/profile` | PUT | 更新资料 |
| `/api/v1/account/user/password` | PUT | 修改密码 |
| `/api/v1/account/logout` | POST | 登出 |

### 管理员接口

| 接口 | 方法 | 描述 |
|------|------|------|
| `/api/v1/admin/roles` | POST/GET | 创建/获取角色 |
| `/api/v1/admin/permissions` | GET | 获取权限树 |
| `/api/v1/admin/user/role` | POST | 分配角色 |

---

## 三、前端接入示例

### 登录流程

```javascript
// 1. 登录获取 Token
const login = async (username, password) => {
  const res = await fetch('/api/v1/account/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password })
  });
  const data = await res.json();
  
  // 保存 Token
  localStorage.setItem('access_token', data.token.access_token);
  localStorage.setItem('refresh_token', data.token.refresh_token);
  
  return data.user;
};

// 2. 请求拦截器添加 Token
const request = async (url, options = {}) => {
  const token = localStorage.getItem('access_token');
  
  const res = await fetch(url, {
    ...options,
    headers: {
      ...options.headers,
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    }
  });
  
  // Token 过期，刷新
  if (res.status === 401) {
    const refreshed = await refreshToken();
    if (refreshed) {
      return request(url, options); // 重试
    } else {
      // 刷新失败，跳转登录
      window.location.href = '/login';
    }
  }
  
  return res;
};

// 3. 刷新 Token
const refreshToken = async () => {
  const refreshToken = localStorage.getItem('refresh_token');
  
  const res = await fetch('/api/v1/account/refresh', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken })
  });
  
  if (res.ok) {
    const data = await res.json();
    localStorage.setItem('access_token', data.access_token);
    localStorage.setItem('refresh_token', data.refresh_token);
    return true;
  }
  
  return false;
};
```

---

## 四、常见问题

### Q1: 如何修改默认密码加密方式？

在 `user_service.go` 中替换 `hashPassword` 和 `verifyPassword` 函数：

```go
// 使用 bcrypt 替代 SHA256
import "golang.org/x/crypto/bcrypt"

func hashPassword(password, salt string) string {
    hashed, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    return string(hashed)
}

func verifyPassword(password, salt, hash string) bool {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
```

### Q2: 如何添加更多用户字段？

1. 在 `models.go` 的 `User` 结构体添加字段
2. 执行数据库迁移
3. 在 `RegisterRequest` 和 `UserInfoResponse` 中添加对应字段

### Q3: 如何实现多租户？

在 `User` 模型添加 `TenantID` 字段，在所有查询中增加租户过滤：

```go
// 查询示例
db.Where("tenant_id = ?", tenantID).First(&user)
```

---

## 五、安全建议

1. **生产环境务必修改 JWT SecretKey**
2. **启用 HTTPS**
3. **添加密码复杂度验证**
4. **实现登录失败锁定机制**
5. **定期清理过期日志数据**
6. **敏感操作添加二次验证**
