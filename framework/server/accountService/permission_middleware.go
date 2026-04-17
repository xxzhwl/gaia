package accountService

import (
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/server"
	"gorm.io/gorm"
)

// PermissionMiddleware 权限中间件
type PermissionMiddleware struct {
	db *gorm.DB
}

// NewPermissionMiddleware 创建权限中间件实例
func NewPermissionMiddleware(db *gorm.DB) *PermissionMiddleware {
	return &PermissionMiddleware{db: db}
}

// RequirePermission 要求单个权限
func (pm *PermissionMiddleware) RequirePermission(permissionCode string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		claims, ok := arg.GetUserInfo()
		if !ok {
			return errwrap.Error(401, nil)
		}

		// 超级管理员拥有所有权限
		if containsRole(claims.Roles, "super_admin") {
			return nil
		}

		hasPermission, err := pm.checkUserPermission(claims.UserID, permissionCode)
		if err != nil {
			return errwrap.Error(500, err)
		}
		if !hasPermission {
			return errwrap.Error(403, nil)
		}
		return nil
	})
}

// RequirePermissions 要求全部权限（AND）
func (pm *PermissionMiddleware) RequirePermissions(permissionCodes ...string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		claims, ok := arg.GetUserInfo()
		if !ok {
			return errwrap.Error(401, nil)
		}

		// 超级管理员拥有所有权限
		if containsRole(claims.Roles, "super_admin") {
			return nil
		}

		for _, code := range permissionCodes {
			hasPermission, err := pm.checkUserPermission(claims.UserID, code)
			if err != nil {
				return errwrap.Error(500, err)
			}
			if !hasPermission {
				return errwrap.Error(403, nil)
			}
		}
		return nil
	})
}

// RequireAnyPermission 要求任一权限（OR）
func (pm *PermissionMiddleware) RequireAnyPermission(permissionCodes ...string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		claims, ok := arg.GetUserInfo()
		if !ok {
			return errwrap.Error(401, nil)
		}

		// 超级管理员拥有所有权限
		if containsRole(claims.Roles, "super_admin") {
			return nil
		}

		for _, code := range permissionCodes {
			hasPermission, err := pm.checkUserPermission(claims.UserID, code)
			if err != nil {
				return errwrap.Error(500, err)
			}
			if hasPermission {
				return nil
			}
		}
		return errwrap.Error(403, nil)
	})
}

// RequireRole 要求角色
func (pm *PermissionMiddleware) RequireRole(roleCode string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		claims, ok := arg.GetUserInfo()
		if !ok {
			return errwrap.Error(401, nil)
		}

		// 超级管理员拥有所有权限
		if containsRole(claims.Roles, "super_admin") {
			return nil
		}

		if !containsRole(claims.Roles, roleCode) {
			return errwrap.Error(403, nil)
		}
		return nil
	})
}

// RequireAnyRole 要求任一角色（OR）
func (pm *PermissionMiddleware) RequireAnyRole(roleCodes ...string) app.HandlerFunc {
	return server.MakePlugin(func(arg server.Request) error {
		claims, ok := arg.GetUserInfo()
		if !ok {
			return errwrap.Error(401, nil)
		}

		// 超级管理员拥有所有权限
		if containsRole(claims.Roles, "super_admin") {
			return nil
		}

		for _, code := range roleCodes {
			if containsRole(claims.Roles, code) {
				return nil
			}
		}
		return errwrap.Error(403, nil)
	})
}

// checkUserPermission 检查用户是否拥有指定权限
func (pm *PermissionMiddleware) checkUserPermission(userID int64, permissionCode string) (bool, error) {
	var count int64
	err := pm.db.Model(&Permission{}).
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Where("user_roles.user_id = ? AND permissions.code = ? AND permissions.status = ?", userID, permissionCode, RoleStatusEnabled).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// containsRole 检查角色列表是否包含指定角色
func containsRole(roles []string, roleCode string) bool {
	for _, r := range roles {
		if r == roleCode {
			return true
		}
	}
	return false
}
