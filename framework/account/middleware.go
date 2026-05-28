package account

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/server"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const principalContextKey = "account_principal"

// Middleware 提供与 Hertz HTTP 框架集成的认证和授权中间件。
type Middleware struct {
	m *Manager
}

// Authenticate 返回中间件处理器，解析并验证 Authorization 头中的 Bearer 令牌，
// 然后将 Principal 设置到请求上下文中。
func (m *Middleware) Authenticate() app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		start := time.Now()
		mm := m.m.Metrics()
		token := bearerToken(string(arg.C().GetHeader("Authorization")))
		if token == "" {
			if mm != nil {
				mm.AuthMiddlewareDuration.Record(arg.TraceContext, float64(time.Since(start).Microseconds())/1000.0,
					metric.WithAttributes(attribute.String("status", "failed")))
			}
			return errwrap.Error(ErrInvalidToken, errors.New("missing bearer token"))
		}
		principal, err := m.m.Auth().Validate(arg.TraceContext, token)
		if err != nil {
			if mm != nil {
				mm.AuthMiddlewareDuration.Record(arg.TraceContext, float64(time.Since(start).Microseconds())/1000.0,
					metric.WithAttributes(attribute.String("status", "failed")))
			}
			return err
		}
		arg.C().Set(principalContextKey, principal)
		if mm != nil {
			mm.AuthMiddlewareDuration.Record(arg.TraceContext, float64(time.Since(start).Microseconds())/1000.0,
				metric.WithAttributes(attribute.String("status", "success")))
		}
		return nil
	})
}

// RequirePermission 返回中间件处理器，检查主体是否拥有指定权限。
func (m *Middleware) RequirePermission(permissionCode string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		principal, ok := GetPrincipal(arg)
		if !ok {
			return errwrap.Error(ErrInvalidToken, errors.New("missing principal"))
		}
		decision, err := m.m.Authorizer().Check(arg.TraceContext, AuthzRequest{
			Subject:    principal,
			Permission: permissionCode,
		})
		if err != nil {
			return err
		}
		if !decision.Allowed {
			if mm := m.m.Metrics(); mm != nil {
				mm.PermissionDenied.Add(arg.TraceContext, 1, metric.WithAttributes(
					attribute.String("method", "require_permission"),
				))
			}
			return errwrap.Error(ErrPermissionDenied, errors.New(decision.Reason))
		}
		return nil
	})
}

// RequireAnyPermission 返回中间件处理器，检查主体是否至少拥有指定权限之一。
// 系统角色（platform_admin、tenant_owner）绕过检查。
func (m *Middleware) RequireAnyPermission(permissionCodes ...string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		principal, ok := GetPrincipal(arg)
		if !ok {
			return errwrap.Error(ErrInvalidToken, errors.New("missing principal"))
		}
		if contains(principal.Roles, "platform_admin") || contains(principal.Roles, "tenant_owner") {
			return nil
		}
		perms, err := m.m.Authorizer().GetEffectivePermissionsForPrincipal(arg.TraceContext, principal)
		if err != nil {
			return err
		}
		for _, code := range permissionCodes {
			if contains(perms, code) {
				return nil
			}
		}
		if mm := m.m.Metrics(); mm != nil {
			mm.PermissionDenied.Add(arg.TraceContext, 1, metric.WithAttributes(
				attribute.String("method", "require_any_permission"),
			))
		}
		return errwrap.Error(ErrPermissionDenied, errors.New("permission denied"))
	})
}

// RequireRole 返回中间件处理器，检查主体是否拥有指定角色。
// 系统角色（platform_admin、tenant_owner）绕过检查。
func (m *Middleware) RequireRole(roleCode string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		principal, ok := GetPrincipal(arg)
		if !ok {
			return errwrap.Error(ErrInvalidToken, errors.New("missing principal"))
		}
		if contains(principal.Roles, roleCode) || contains(principal.Roles, "platform_admin") || contains(principal.Roles, "tenant_owner") {
			return nil
		}
		if mm := m.m.Metrics(); mm != nil {
			mm.PermissionDenied.Add(arg.TraceContext, 1, metric.WithAttributes(
				attribute.String("method", "require_role"),
			))
		}
		return errwrap.Error(ErrPermissionDenied, errors.New("role denied"))
	})
}

// OptionalAuthenticate 返回中间件处理器，尝试解析 Bearer 令牌但不强制要求。
// 如果令牌有效则将 Principal 写入请求上下文；如果无效或缺失，请求仍继续处理。
func (m *Middleware) OptionalAuthenticate() app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		token := bearerToken(string(arg.C().GetHeader("Authorization")))
		if token == "" {
			return nil
		}
		principal, err := m.m.Auth().Validate(arg.TraceContext, token)
		if err != nil {
			return nil // ignore invalid token, continue without principal
		}
		arg.C().Set(principalContextKey, principal)
		return nil
	})
}

// RequireAnyRole 返回中间件处理器，检查主体是否至少拥有指定角色之一。
// 系统角色（platform_admin、tenant_owner）绕过检查。
func (m *Middleware) RequireAnyRole(roleCodes ...string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		principal, ok := GetPrincipal(arg)
		if !ok {
			return errwrap.Error(ErrInvalidToken, errors.New("missing principal"))
		}
		if contains(principal.Roles, "platform_admin") || contains(principal.Roles, "tenant_owner") {
			return nil
		}
		for _, code := range roleCodes {
			if contains(principal.Roles, code) {
				return nil
			}
		}
		if mm := m.m.Metrics(); mm != nil {
			mm.PermissionDenied.Add(arg.TraceContext, 1, metric.WithAttributes(
				attribute.String("method", "require_any_role"),
			))
		}
		return errwrap.Error(ErrPermissionDenied, errors.New("role denied"))
	})
}

// RequireAllPermissions 返回中间件处理器，检查主体是否拥有所有指定权限。
// 系统角色（platform_admin、tenant_owner）绕过检查。
func (m *Middleware) RequireAllPermissions(permissionCodes ...string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		principal, ok := GetPrincipal(arg)
		if !ok {
			return errwrap.Error(ErrInvalidToken, errors.New("missing principal"))
		}
		if contains(principal.Roles, "platform_admin") || contains(principal.Roles, "tenant_owner") {
			return nil
		}
		perms, err := m.m.Authorizer().GetEffectivePermissionsForPrincipal(arg.TraceContext, principal)
		if err != nil {
			return err
		}
		for _, code := range permissionCodes {
			if !contains(perms, code) {
				if mm := m.m.Metrics(); mm != nil {
					mm.PermissionDenied.Add(arg.TraceContext, 1, metric.WithAttributes(
						attribute.String("method", "require_all_permissions"),
					))
				}
				return errwrap.Error(ErrPermissionDenied, errors.New("permission denied"))
			}
		}
		return nil
	})
}

// AuthenticateAPIToken 返回只接受个人访问令牌的中间件。
// 适合开放给 CLI、自动化脚本或服务端集成的 API。
func (m *Middleware) AuthenticateAPIToken() app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		token := bearerToken(string(arg.C().GetHeader("Authorization")))
		if token == "" {
			return errwrap.Error(ErrInvalidToken, errors.New("missing bearer token"))
		}
		principal, err := m.m.APITokens().Validate(arg.TraceContext, token)
		if err != nil {
			return err
		}
		arg.C().Set(principalContextKey, principal)
		return nil
	})
}

// AuthenticateAnyBearer 返回同时接受登录 JWT 和个人访问令牌的中间件。
// 使用时应配合 RequirePermission / RequireAnyPermission 等权限中间件，让 PAT scopes 生效。
func (m *Middleware) AuthenticateAnyBearer() app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		token := bearerToken(string(arg.C().GetHeader("Authorization")))
		if token == "" {
			return errwrap.Error(ErrInvalidToken, errors.New("missing bearer token"))
		}
		principal, err := m.authenticateBearer(arg.TraceContext, token)
		if err != nil {
			return err
		}
		arg.C().Set(principalContextKey, principal)
		return nil
	})
}

func (m *Middleware) authenticateBearer(ctx context.Context, token string) (*Principal, error) {
	principal, err := m.m.Auth().Validate(ctx, token)
	if err == nil {
		return principal, nil
	}
	if strings.HasPrefix(token, personalAccessTokenPrefix) {
		return m.m.APITokens().Validate(ctx, token)
	}
	return nil, err
}

// GetPrincipal 从请求上下文中提取 Principal。
func GetPrincipal(req server.Request) (*Principal, bool) {
	value, ok := req.C().Get(principalContextKey)
	if !ok {
		return nil, false
	}
	principal, ok := value.(*Principal)
	return principal, ok
}

// bearerToken 从 Authorization 头部值中提取 Bearer 令牌。
func bearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
