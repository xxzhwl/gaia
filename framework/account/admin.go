package account

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PermissionService 提供权限点的管理操作。
type PermissionService struct {
	m *Manager
}

// PermissionListRequest 权限查询参数。
type PermissionListRequest struct {
	TenantID     string
	ResourceType string
	Action       string
	Page         int
	PageSize     int
}

// PermissionListResult 权限查询结果。
type PermissionListResult struct {
	Items []Permission `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
}

// List 查询权限列表，支持按资源类型和动作筛选。
func (s *PermissionService) List(ctx context.Context, req PermissionListRequest) (*PermissionListResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.permission.list")
	defer span.End()
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 200 {
		req.PageSize = 50
	}
	q := s.m.db.WithContext(ctx).Model(&Permission{}).Where("tenant_id = ?", s.m.tenantID(req.TenantID))
	if req.ResourceType != "" {
		q = q.Where("resource_type = ?", req.ResourceType)
	}
	if req.Action != "" {
		q = q.Where("action = ?", req.Action)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var items []Permission
	offset := (req.Page - 1) * req.PageSize
	if err := q.Order("resource_type ASC, code ASC").Offset(offset).Limit(req.PageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	if items == nil {
		items = []Permission{}
	}
	return &PermissionListResult{Items: items, Total: total, Page: req.Page}, nil
}

// Create 创建新的权限点。
func (s *PermissionService) Create(ctx context.Context, perm *Permission) error {
	ctx, span := s.m.tracer.Start(ctx, "account.permission.create")
	defer span.End()
	perm.ID = newID()
	perm.TenantID = s.m.tenantID(perm.TenantID)
	if perm.Status == "" {
		perm.Status = "enabled"
	}
	return s.m.db.WithContext(ctx).Create(perm).Error
}

// Update 更新权限点的描述和状态。
func (s *PermissionService) Update(ctx context.Context, tenantID, permissionID, description, status string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.permission.update")
	defer span.End()
	updates := map[string]any{}
	if description != "" {
		updates["description"] = description
	}
	if status != "" {
		updates["status"] = status
	}
	if len(updates) == 0 {
		return nil
	}
	return s.m.db.WithContext(ctx).Model(&Permission{}).
		Where("id = ? AND tenant_id = ?", permissionID, s.m.tenantID(tenantID)).
		Updates(updates).Error
}

// Delete 删除指定的权限点。
func (s *PermissionService) Delete(ctx context.Context, tenantID, permissionID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.permission.delete")
	defer span.End()
	tenantID = s.m.tenantID(tenantID)
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("permission_id = ?", permissionID).Delete(&RolePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND tenant_id = ?", permissionID, tenantID).Delete(&Permission{}).Error; err != nil {
			return err
		}
		_ = emitOutbox(tx, EventPermissionChanged, "", map[string]any{
			"permission_id": permissionID,
			"tenant_id":     tenantID,
			"action":        "deleted",
			"deleted_at":    time.Now(),
		})
		return nil
	})
}

// AssignToRole 将权限点分配给角色。
func (s *PermissionService) AssignToRole(ctx context.Context, tenantID, roleID, permissionID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.permission.assign_role")
	defer span.End()
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rp := RolePermission{ID: newID(), RoleID: roleID, PermissionID: permissionID}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rp).Error; err != nil {
			return err
		}
		_ = tx.Model(&Role{}).Where("id = ?", roleID).Update("version", gorm.Expr("version + 1")).Error
		return nil
	})
}

// GetRolePermissions 查询角色已分配的全部权限。
func (s *PermissionService) GetRolePermissions(ctx context.Context, roleID string) ([]Permission, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.permission.get_role_permissions")
	defer span.End()
	var perms []Permission
	err := s.m.db.WithContext(ctx).Model(&Permission{}).
		Joins("JOIN acct_role_permissions ON acct_role_permissions.permission_id = acct_permissions.id").
		Where("acct_role_permissions.role_id = ?", roleID).
		Find(&perms).Error
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []Permission{}
	}
	return perms, nil
}

// RemoveFromRole 从角色移除权限点。
func (s *PermissionService) RemoveFromRole(ctx context.Context, roleID, permissionID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.permission.remove_role")
	defer span.End()
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ? AND permission_id = ?", roleID, permissionID).Delete(&RolePermission{}).Error; err != nil {
			return err
		}
		_ = tx.Model(&Role{}).Where("id = ?", roleID).Update("version", gorm.Expr("version + 1")).Error
		return nil
	})
}

// AdminService 提供管理端操作（用户管理、角色管理、会话管理等）。
// 所有方法本身不做权限检查，由调用方通过中间件控制。
type AdminService struct {
	m *Manager
}

// ListUsersRequest 用户列表查询参数。
type ListUsersRequest struct {
	TenantID string
	Status   string
	Keyword  string // 搜索 username/email/phone
	Page     int
	PageSize int
}

// ListUsersResult 用户列表查询结果。
type ListUsersResult struct {
	Items []UserInfo `json:"items"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
}

// CreateUserWithPasswordRequest 管理员创建带密码用户的请求参数。
type CreateUserWithPasswordRequest struct {
	TenantID  string
	Username  string
	Password  string
	Nickname  string
	Email     string
	Phone     string
	Status    string
	AvatarURL string
}

// ListUsers 查询用户列表，支持按状态和关键字搜索。
func (s *AdminService) ListUsers(ctx context.Context, req ListUsersRequest) (*ListUsersResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.list_users")
	defer span.End()
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 200 {
		req.PageSize = 50
	}
	q := s.m.db.WithContext(ctx).Model(&User{}).Where("tenant_id = ?", s.m.tenantID(req.TenantID))
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	if req.Keyword != "" {
		kw := "%" + req.Keyword + "%"
		q = q.Where("username LIKE ? OR email LIKE ? OR phone LIKE ?", kw, kw, kw)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var users []User
	offset := (req.Page - 1) * req.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&users).Error; err != nil {
		return nil, err
	}
	items := make([]UserInfo, 0, len(users))
	if len(users) == 0 {
		return &ListUsersResult{Items: items, Total: total, Page: req.Page}, nil
	}

	// 批量查询角色
	userIDs := make([]string, len(users))
	for i, u := range users {
		userIDs[i] = u.ID
	}
	type userRole struct {
		UserID string
		Code   string
	}
	var userRoles []userRole
	_ = s.m.db.WithContext(ctx).Model(&Role{}).
		Select("acct_user_roles.user_id, acct_roles.code").
		Joins("JOIN acct_user_roles ON acct_user_roles.role_id = acct_roles.id").
		Where("acct_user_roles.user_id IN ? AND acct_roles.status = ?", userIDs, "enabled").
		Scan(&userRoles)
	rolesMap := make(map[string][]string, len(users))
	for _, ur := range userRoles {
		rolesMap[ur.UserID] = append(rolesMap[ur.UserID], ur.Code)
	}

	// 批量查询权限
	type userPerm struct {
		UserID string
		Code   string
	}
	var userPerms []userPerm
	_ = s.m.db.WithContext(ctx).Model(&Permission{}).
		Select("acct_user_roles.user_id, acct_permissions.code").
		Joins("JOIN acct_role_permissions ON acct_role_permissions.permission_id = acct_permissions.id").
		Joins("JOIN acct_user_roles ON acct_user_roles.role_id = acct_role_permissions.role_id").
		Where("acct_user_roles.user_id IN ? AND acct_permissions.status = ? AND acct_permissions.tenant_id = ?", userIDs, "enabled", s.m.tenantID(req.TenantID)).
		Distinct().
		Scan(&userPerms)
	permsMap := make(map[string][]string, len(users))
	for _, up := range userPerms {
		permsMap[up.UserID] = append(permsMap[up.UserID], up.Code)
	}

	for _, u := range users {
		roles := rolesMap[u.ID]
		perms := permsMap[u.ID]
		if roles == nil {
			roles = []string{}
		}
		if perms == nil {
			perms = []string{}
		}
		items = append(items, *u.toInfo(roles, perms))
	}
	return &ListUsersResult{Items: items, Total: total, Page: req.Page}, nil
}

// CreateUser 由管理员创建用户（初始无密码，需要用户自行重置）。
func (s *AdminService) CreateUser(ctx context.Context, user *User) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.create_user")
	defer span.End()
	user.ID = newID()
	user.TenantID = s.m.tenantID(user.TenantID)
	user.AuthVersion = 1
	user.RolesVersion = 1
	user.ProfileVersion = 1
	if user.Status == "" {
		user.Status = UserStatusNormal
	}
	return s.m.db.WithContext(ctx).Create(user).Error
}

// CreateUserWithPassword 由管理员创建带初始密码的用户，同时创建密码凭证。
func (s *AdminService) CreateUserWithPassword(ctx context.Context, req CreateUserWithPasswordRequest) (*UserInfo, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.create_user_with_password")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)
	req.Username = strings.TrimSpace(req.Username)
	req.Email = normalizeEmail(req.Email)
	req.Phone = normalizePhone(req.Phone)
	if err := validateIdentifier(RegisterRequest{
		Username: req.Username,
		Email:    req.Email,
		Phone:    req.Phone,
	}); err != nil {
		return nil, accountError(ErrInvalidArgument, err.Error())
	}
	if err := validatePassword(req.Password, s.m.cfg.Password); err != nil {
		return nil, err
	}
	if req.Status == "" {
		req.Status = UserStatusNormal
	}

	var created *UserInfo
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&User{}).Where("tenant_id = ? AND username = ?", tenantID, req.Username).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return accountError(ErrIdentifierExists, "用户名已存在")
		}
		if req.Email != "" {
			if err := tx.Model(&User{}).Where("tenant_id = ? AND email = ?", tenantID, req.Email).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return accountError(ErrIdentifierExists, "邮箱已存在")
			}
		}
		if req.Phone != "" {
			if err := tx.Model(&User{}).Where("tenant_id = ? AND phone = ?", tenantID, req.Phone).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return accountError(ErrIdentifierExists, "手机号已存在")
			}
		}

		passHash, err := hashPassword(req.Password, s.m.cfg.Password)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		user := User{
			ID:             newID(),
			TenantID:       tenantID,
			Username:       req.Username,
			Email:          nullableString(req.Email),
			Phone:          nullableString(req.Phone),
			Nickname:       req.Nickname,
			AvatarURL:      req.AvatarURL,
			Status:         req.Status,
			AuthVersion:    1,
			RolesVersion:   1,
			ProfileVersion: 1,
		}
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		cred := Credential{
			ID:         newID(),
			TenantID:   tenantID,
			UserID:     user.ID,
			Type:       CredentialPassword,
			Identifier: user.Username,
			SecretHash: passHash,
			SecretMeta: "{}",
			Enabled:    true,
		}
		if err := tx.Create(&cred).Error; err != nil {
			return fmt.Errorf("create credential: %w", err)
		}
		if err := s.m.auth.assignDefaultRole(ctx, tx, &user); err != nil {
			return err
		}
		roles, err := s.m.auth.loadRoleCodes(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		created = user.toInfo(roles, []string{})
		_ = emitOutbox(tx, EventUserRegistered, user.ID, map[string]any{
			"user_id":       user.ID,
			"tenant_id":     tenantID,
			"username":      user.Username,
			"email":         user.Email,
			"phone":         user.Phone,
			"created_by":    "admin",
			"registered_at": time.Now(),
		})
		return nil
	})
	if err != nil {
		s.m.audit(ctx, tenantID, "", "admin_create_user", "failed", err.Error(), "", "")
		return nil, err
	}
	s.m.audit(ctx, tenantID, created.ID, "admin_create_user", "success", "", "", "")
	return created, nil
}

// UpdateUserStatus 更新用户状态（禁用/启用/锁定/解锁）。
func (s *AdminService) UpdateUserStatus(ctx context.Context, tenantID, userID, status string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.update_user_status")
	defer span.End()
	return s.m.db.WithContext(ctx).Model(&User{}).
		Where("id = ? AND tenant_id = ?", userID, s.m.tenantID(tenantID)).
		Update("status", status).Error
}

// ResetUserMFA 重置用户的 MFA 凭证。
func (s *AdminService) ResetUserMFA(ctx context.Context, tenantID, userID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.reset_mfa")
	defer span.End()
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Credential{}).Where("user_id = ? AND type IN ?", userID, []string{CredentialTOTP, CredentialRecoveryCode}).
			Update("enabled", false).Error; err != nil {
			return err
		}
		s.m.audit(ctx, tenantID, userID, "admin_reset_mfa", "success", "", "", "")
		return nil
	})
}

// ListRoles 查询角色列表。
func (s *AdminService) ListRoles(ctx context.Context, tenantID string) ([]Role, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.list_roles")
	defer span.End()
	var roles []Role
	if err := s.m.db.WithContext(ctx).Where("tenant_id = ?", s.m.tenantID(tenantID)).
		Order("code ASC").Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// CreateRole 创建新角色（委托 RoleService）。
func (s *AdminService) CreateRole(ctx context.Context, req CreateRoleRequest) (*Role, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.create_role")
	defer span.End()
	return s.m.roles.CreateRole(ctx, req)
}

// UpdateRole 更新角色信息。
func (s *AdminService) UpdateRole(ctx context.Context, roleID, name, description string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.update_role")
	defer span.End()
	updates := map[string]any{}
	if name != "" {
		updates["name"] = name
	}
	if description != "" {
		updates["description"] = description
	}
	if len(updates) == 0 {
		return nil
	}
	return s.m.db.WithContext(ctx).Model(&Role{}).Where("id = ?", roleID).Updates(updates).Error
}

// DeleteRole 删除角色及其关联。
func (s *AdminService) DeleteRole(ctx context.Context, roleID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.delete_role")
	defer span.End()
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&RolePermission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("role_id = ?", roleID).Delete(&UserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", roleID).Delete(&Role{}).Error; err != nil {
			return err
		}
		return nil
	})
}

// AssignUserRole 为用户分配角色（委托 RoleService）。
func (s *AdminService) AssignUserRole(ctx context.Context, userID, roleID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.assign_role")
	defer span.End()
	return s.m.roles.AssignUserRole(ctx, userID, roleID, nil)
}

// RemoveUserRole 移除用户的角色。
func (s *AdminService) RemoveUserRole(ctx context.Context, userID, roleID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.remove_role")
	defer span.End()
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND role_id = ?", userID, roleID).Delete(&UserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("roles_version", gorm.Expr("roles_version + 1")).Error; err != nil {
			return err
		}
		return nil
	})
}

// ListUserSessions 查询用户的活跃会话。
func (s *AdminService) ListUserSessions(ctx context.Context, tenantID, userID string) ([]SessionInfo, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.list_sessions")
	defer span.End()
	return s.m.sessionSvc.List(ctx, tenantID, userID, "")
}

// RevokeSession 强制撤销指定会话。
func (s *AdminService) RevokeSession(ctx context.Context, tenantID, sessionID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.revoke_session")
	defer span.End()
	return s.m.sessionSvc.RevokeByID(ctx, tenantID, sessionID)
}

// ListPermissions 查询权限列表（委托 PermissionService）。
func (s *AdminService) ListPermissions(ctx context.Context, req PermissionListRequest) (*PermissionListResult, error) {
	return s.m.Permissions().List(ctx, req)
}

// CreatePermission 创建权限点。
func (s *AdminService) CreatePermission(ctx context.Context, perm *Permission) error {
	return s.m.Permissions().Create(ctx, perm)
}

// UpdatePermission 更新权限点。
func (s *AdminService) UpdatePermission(ctx context.Context, tenantID, permissionID, description, status string) error {
	return s.m.Permissions().Update(ctx, tenantID, permissionID, description, status)
}

// DeletePermission 删除权限点。
func (s *AdminService) DeletePermission(ctx context.Context, tenantID, permissionID string) error {
	return s.m.Permissions().Delete(ctx, tenantID, permissionID)
}

// AssignPermissionToRole 将权限点分配给角色。
func (s *AdminService) AssignPermissionToRole(ctx context.Context, tenantID, roleID, permissionID string) error {
	return s.m.Permissions().AssignToRole(ctx, tenantID, roleID, permissionID)
}

// RemovePermissionFromRole 从角色移除权限点。
func (s *AdminService) RemovePermissionFromRole(ctx context.Context, roleID, permissionID string) error {
	return s.m.Permissions().RemoveFromRole(ctx, roleID, permissionID)
}

// GetRolePermissions 查询角色已分配的全部权限。
func (s *AdminService) GetRolePermissions(ctx context.Context, roleID string) ([]Permission, error) {
	return s.m.Permissions().GetRolePermissions(ctx, roleID)
}

// GetUserPermissions 查询用户的有效权限列表与角色。
func (s *AdminService) GetUserPermissions(ctx context.Context, userID string) ([]string, []Role, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.admin.get_user_permissions")
	defer span.End()
	perms, err := s.m.Authorizer().GetEffectivePermissions(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	var userRoleIDs []string
	if err := s.m.db.WithContext(ctx).Model(&UserRole{}).
		Where("user_id = ?", userID).Pluck("role_id", &userRoleIDs).Error; err != nil {
		return nil, nil, err
	}
	var roles []Role
	if len(userRoleIDs) > 0 {
		if err := s.m.db.WithContext(ctx).Where("id IN ?", userRoleIDs).
			Find(&roles).Error; err != nil {
			return nil, nil, err
		}
	}
	if perms == nil {
		perms = []string{}
	}
	if roles == nil {
		roles = []Role{}
	}
	return perms, roles, nil
}
