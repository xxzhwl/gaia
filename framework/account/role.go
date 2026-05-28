package account

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RoleService 提供角色创建和用户角色分配功能。
type RoleService struct {
	m *Manager
}

// CreateRoleRequest 创建角色的请求参数。
type CreateRoleRequest struct {
	TenantID    string
	Code        string
	Name        string
	Description string
}

// CreateRole 使用指定的代码、名称和描述创建新角色。
func (s *RoleService) CreateRole(ctx context.Context, req CreateRoleRequest) (*Role, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.role.create")
	defer span.End()
	role := &Role{
		ID:          newID(),
		TenantID:    s.m.tenantID(req.TenantID),
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
		Status:      "enabled",
		Version:     1,
	}
	if err := s.m.db.WithContext(ctx).Create(role).Error; err != nil {
		return nil, err
	}
	return role, nil
}

// AssignUserRole 为用户分配角色，增加 roles_version 并使缓存失效。
// 如果提供了 principal，将强制检查 step-up MFA。
func (s *RoleService) AssignUserRole(ctx context.Context, userID, roleID string, principal *Principal) error {
	ctx, span := s.m.tracer.Start(ctx, "account.role.assign_user")
	defer span.End()
	// Enforce step-up MFA if principal is provided
	if principal != nil {
		if err := s.m.auth.RequireStepUp(ctx, principal); err != nil {
			return err
		}
	}
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		var role Role
		if err := tx.Where("id = ? AND tenant_id = ? AND status = ?", roleID, user.TenantID, "enabled").First(&role).Error; err != nil {
			return err
		}
		userRole := UserRole{
			ID:        newID(),
			TenantID:  user.TenantID,
			UserID:    user.ID,
			RoleID:    role.ID,
			ScopeType: "tenant",
			ScopeID:   user.TenantID,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&userRole).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("roles_version", gorm.Expr("roles_version + 1")).Error; err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		_ = s.m.authorizer.invalidatePermissions(ctx, userID)
		s.m.auth.invalidateUserPrincipalCaches(ctx, userID)
		_ = emitOutbox(s.m.db.WithContext(ctx), EventRoleAssigned, userID, map[string]any{
			"user_id":     userID,
			"role_id":     roleID,
			"assigned_at": time.Now(),
		})
	}
	return err
}

// Authorizer 提供授权检查功能，判断主体是否拥有指定权限。
type Authorizer struct {
	m *Manager
}

// AuthzRequest 授权检查请求参数。
type AuthzRequest struct {
	Subject    *Principal
	Permission string
	// ResourceType 是可选的资源类型，用于资源级授权（如 "document"、"article"）。
	ResourceType string `json:"resource_type,omitempty"`
	// ResourceID 是可选的资源标识符，用于资源级授权上下文。
	ResourceID string `json:"resource_id,omitempty"`
	// OwnerID 是可选的资源所有者用户 ID。如果设置且与 Subject.UserID 匹配，将自动授予访问权限。
	OwnerID string `json:"owner_id,omitempty"`
	// OrgID 是可选的组织 ID。设置后将仅评估该组织及其祖先组织的作用域内权限，
	// 而非用户的所有跨组织权限。留空则使用当前行为（全部权限）。
	OrgID string `json:"org_id,omitempty"`
}

// AuthzDecision 授权检查结果。
type AuthzDecision struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
}

// Check 评估主体是否拥有请求的权限。
// 评估顺序：系统角色 → 资源所有者 → ABAC 策略拒绝 → RBAC 权限（支持 org 作用域）→ ABAC 策略允许 → 拒绝。
func (a *Authorizer) Check(ctx context.Context, req AuthzRequest) (*AuthzDecision, error) {
	ctx, span := a.m.tracer.Start(ctx, "account.authz.check")
	defer span.End()
	start := time.Now()
	defer func() {
		if m := a.m.metrics; m != nil {
			recordDuration(ctx, m.AuthzCheckDuration, start)
		}
	}()
	if req.Subject == nil {
		return &AuthzDecision{Allowed: false, Reason: "missing principal"}, nil
	}
	if req.Subject.APITokenID != "" && !apiTokenAllowsPermission(req.Subject.Scopes, req.Permission) {
		return &AuthzDecision{Allowed: false, Reason: "api token scope denied"}, nil
	}
	// Resource owner automatically has access
	if req.OwnerID != "" && req.OwnerID == req.Subject.UserID {
		return &AuthzDecision{Allowed: true, Reason: "resource owner"}, nil
	}
	if contains(req.Subject.Roles, "platform_admin") || contains(req.Subject.Roles, "tenant_owner") {
		return &AuthzDecision{Allowed: true, Reason: "system role"}, nil
	}

	// ABAC policy evaluation (deny takes precedence, allow resolves after RBAC)
	policySvc := &PolicyService{m: a.m}
	policyDecision, _ := policySvc.EvaluatePolicies(ctx, req)
	if policyDecision != nil && policyDecision.Matched && !policyDecision.Allowed {
		return &AuthzDecision{Allowed: false, Reason: policyDecision.Reason}, nil
	}

	// RBAC permission check (with optional org scoping)
	var perms []string
	var err error
	if req.OrgID != "" {
		perms, err = a.GetEffectivePermissionsForPrincipalScoped(ctx, req.Subject, req.OrgID)
	} else {
		perms, err = a.GetEffectivePermissionsForPrincipal(ctx, req.Subject)
	}
	if err != nil {
		return nil, err
	}
	if contains(perms, req.Permission) {
		return &AuthzDecision{Allowed: true, Reason: "permission matched"}, nil
	}

	// ABAC policy allow (overrides RBAC denial)
	if policyDecision != nil && policyDecision.Matched && policyDecision.Allowed {
		return &AuthzDecision{Allowed: true, Reason: policyDecision.Reason}, nil
	}

	if m := a.m.metrics; m != nil {
		m.PermissionDenied.Add(ctx, 1, metric.WithAttributes(
			attribute.String("method", "authz_check"),
		))
	}
	return &AuthzDecision{Allowed: false, Reason: "permission denied"}, nil
}

// CheckMany 在一次调用中评估多个授权请求，使用内存缓存。
// 评估顺序与 Check 一致：系统角色 → 资源所有者 → ABAC 策略拒绝 → RBAC → ABAC 策略允许。
func (a *Authorizer) CheckMany(ctx context.Context, reqs []AuthzRequest) ([]AuthzDecision, error) {
	ctx, span := a.m.tracer.Start(ctx, "account.authz.check_many")
	defer span.End()
	start := time.Now()
	defer func() {
		if m := a.m.metrics; m != nil {
			recordDuration(ctx, m.AuthzCheckDuration, start)
		}
	}()
	decisions := make([]AuthzDecision, len(reqs))
	permissionCache := make(map[string][]string)
	policySvc := &PolicyService{m: a.m}
	for i, req := range reqs {
		if req.Subject == nil {
			decisions[i] = AuthzDecision{Allowed: false, Reason: "missing principal"}
			continue
		}
		if req.Subject.APITokenID != "" && !apiTokenAllowsPermission(req.Subject.Scopes, req.Permission) {
			decisions[i] = AuthzDecision{Allowed: false, Reason: "api token scope denied"}
			continue
		}
		// Resource owner automatically has access
		if req.OwnerID != "" && req.OwnerID == req.Subject.UserID {
			decisions[i] = AuthzDecision{Allowed: true, Reason: "resource owner"}
			continue
		}
		if contains(req.Subject.Roles, "platform_admin") || contains(req.Subject.Roles, "tenant_owner") {
			decisions[i] = AuthzDecision{Allowed: true, Reason: "system role"}
			continue
		}

		// ABAC policy deny
		policyDecision, _ := policySvc.EvaluatePolicies(ctx, req)
		if policyDecision != nil && policyDecision.Matched && !policyDecision.Allowed {
			decisions[i] = AuthzDecision{Allowed: false, Reason: policyDecision.Reason}
			continue
		}

		perms, err := a.getPermissionsForRequest(ctx, req, permissionCache)
		if err != nil {
			return nil, err
		}
		if contains(perms, req.Permission) {
			decisions[i] = AuthzDecision{Allowed: true, Reason: "permission matched"}
		} else if policyDecision != nil && policyDecision.Matched && policyDecision.Allowed {
			decisions[i] = AuthzDecision{Allowed: true, Reason: policyDecision.Reason}
		} else {
			decisions[i] = AuthzDecision{Allowed: false, Reason: "permission denied"}
		}
	}
	return decisions, nil
}

// GetEffectivePermissions 返回用户的全部有效权限代码。
func (a *Authorizer) GetEffectivePermissions(ctx context.Context, userID string) ([]string, error) {
	var user User
	if err := a.m.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	return a.GetEffectivePermissionsForUser(ctx, user.ID, user.TenantID, user.RolesVersion)
}

// GetEffectivePermissionsForPrincipal 返回 Principal 的全部有效权限代码。
func (a *Authorizer) GetEffectivePermissionsForPrincipal(ctx context.Context, principal *Principal) ([]string, error) {
	if principal == nil {
		return nil, accountError(ErrInvalidArgument, "principal 不能为空")
	}
	if principal.APITokenID != "" {
		return principal.Scopes, nil
	}
	return a.GetEffectivePermissionsForUser(ctx, principal.UserID, principal.TenantID, principal.RolesVersion)
}

// GetEffectivePermissionsForPrincipalScoped 返回用户在指定组织作用域内的有效权限代码。
// 包括租户级权限和该组织（及其祖先组织）的 org 级权限，按 rolesVersion + orgID 缓存。
func (a *Authorizer) GetEffectivePermissionsForPrincipalScoped(ctx context.Context, principal *Principal, orgID string) ([]string, error) {
	if principal == nil {
		return nil, accountError(ErrInvalidArgument, "principal 不能为空")
	}
	if principal.APITokenID != "" {
		return principal.Scopes, nil
	}
	if orgID == "" {
		return a.GetEffectivePermissionsForPrincipal(ctx, principal)
	}
	scopeIDs, err := a.m.orgSvc.OrgScopeIDs(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("resolve org scope: %w", err)
	}
	key := a.permissionCacheScopedKey(principal.TenantID, principal.UserID, principal.RolesVersion, orgID)
	if a.m.cache != nil {
		if raw, ok, err := a.m.cache.Get(ctx, key); err != nil {
			return nil, err
		} else if ok {
			var cached []string
			if json.Unmarshal([]byte(raw), &cached) == nil {
				return cached, nil
			}
		}
	}
	var perms []string
	err = a.m.db.WithContext(ctx).Model(&Permission{}).
		Joins("JOIN acct_role_permissions ON acct_role_permissions.permission_id = acct_permissions.id").
		Joins("JOIN acct_user_roles ON acct_user_roles.role_id = acct_role_permissions.role_id").
		Where("acct_user_roles.user_id = ? AND acct_user_roles.tenant_id = ? AND acct_permissions.tenant_id = ? AND acct_permissions.status = ?", principal.UserID, principal.TenantID, principal.TenantID, "enabled").
		Where("(acct_user_roles.scope_type = ?) OR (acct_user_roles.scope_type = ? AND acct_user_roles.scope_id IN ?)", "tenant", "org", scopeIDs).
		Distinct().
		Pluck("acct_permissions.code", &perms).Error
	if err != nil {
		return nil, err
	}
	if a.m.cache != nil {
		if data, err := json.Marshal(perms); err == nil {
			_ = a.m.cache.Set(ctx, key, string(data), a.m.cfg.PermissionCacheTTL)
		}
	}
	return perms, nil
}

// GetEffectivePermissionsForUser 返回用户有效权限代码，按 rolesVersion 缓存。
func (a *Authorizer) GetEffectivePermissionsForUser(ctx context.Context, userID, tenantID string, rolesVersion int64) ([]string, error) {
	key := a.permissionCacheKey(tenantID, userID, rolesVersion)
	if a.m.cache != nil {
		if raw, ok, err := a.m.cache.Get(ctx, key); err != nil {
			return nil, err
		} else if ok {
			var cached []string
			if json.Unmarshal([]byte(raw), &cached) == nil {
				return cached, nil
			}
		}
	}
	var perms []string
	err := a.m.db.WithContext(ctx).Model(&Permission{}).
		Joins("JOIN acct_role_permissions ON acct_role_permissions.permission_id = acct_permissions.id").
		Joins("JOIN acct_user_roles ON acct_user_roles.role_id = acct_role_permissions.role_id").
		Where("acct_user_roles.user_id = ? AND acct_user_roles.tenant_id = ? AND acct_permissions.tenant_id = ? AND acct_permissions.status = ?", userID, tenantID, tenantID, "enabled").
		Distinct().
		Pluck("acct_permissions.code", &perms).Error
	if err != nil {
		return nil, err
	}
	if a.m.cache != nil {
		if data, err := json.Marshal(perms); err == nil {
			_ = a.m.cache.Set(ctx, key, string(data), a.m.cfg.PermissionCacheTTL)
		}
	}
	return perms, nil
}

func apiTokenAllowsPermission(scopes []string, permission string) bool {
	if len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "*" || scope == permission {
			return true
		}
		if strings.HasSuffix(scope, ":*") {
			prefix := strings.TrimSuffix(scope, "*")
			if strings.HasPrefix(permission, prefix) {
				return true
			}
		}
	}
	return false
}

// permissionCacheKey 返回用户有效权限的缓存键。
func (a *Authorizer) permissionCacheKey(tenantID, userID string, rolesVersion int64) string {
	return fmt.Sprintf("perms:%s:%s:%d", tenantID, userID, rolesVersion)
}

// permissionCacheScopedKey 返回用户在指定组织作用域内有效权限的缓存键。
func (a *Authorizer) permissionCacheScopedKey(tenantID, userID string, rolesVersion int64, orgID string) string {
	return fmt.Sprintf("perms:%s:%s:%d:org:%s", tenantID, userID, rolesVersion, orgID)
}

// WarmupPermissionCache 预热活跃用户的权限缓存。
// 查询所有已分配角色的活跃用户，批量计算权限并写入缓存。
// batchSize 控制每批处理的用户数量，默认 100。
// 适用于 Bootstrap 后或服务重启时调用，避免冷启动穿透到 DB。
func (a *Authorizer) WarmupPermissionCache(ctx context.Context, batchSize int) error {
	if a.m.cache == nil {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 100
	}

	var offset int
	for {
		var users []User
		err := a.m.db.WithContext(ctx).
			Table("acct_users").
			Select("id, tenant_id, roles_version, status").
			Where("status = ? AND id IN (SELECT DISTINCT user_id FROM acct_user_roles)", UserStatusNormal).
			Order("id").
			Offset(offset).Limit(batchSize).
			Find(&users).Error
		if err != nil {
			return fmt.Errorf("warmup query users: %w", err)
		}
		if len(users) == 0 {
			break
		}

		for _, user := range users {
			if _, err := a.GetEffectivePermissionsForUser(ctx, user.ID, user.TenantID, user.RolesVersion); err != nil {
				gaia.WarnF("[account] warmup permissions failed for user %s: %v", user.ID, err)
			}
		}

		if len(users) < batchSize {
			break
		}
		offset += batchSize
	}
	return nil
}

// invalidatePermissions 移除用户的缓存权限集。
func (a *Authorizer) invalidatePermissions(ctx context.Context, userID string) error {
	if a.m.cache == nil {
		return nil
	}
	var user User
	if err := a.m.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil
	}
	return a.m.cache.Del(ctx, a.permissionCacheKey(user.TenantID, userID, user.RolesVersion))
}

// getPermissionsForRequest 获取请求对应的权限列表，支持 org 作用域缓存。
func (a *Authorizer) getPermissionsForRequest(ctx context.Context, req AuthzRequest, cache map[string][]string) ([]string, error) {
	if req.OrgID == "" {
		key := fmt.Sprintf("%s:%s:%d", req.Subject.TenantID, req.Subject.UserID, req.Subject.RolesVersion)
		if perms, ok := cache[key]; ok {
			return perms, nil
		}
		perms, err := a.GetEffectivePermissionsForPrincipal(ctx, req.Subject)
		if err != nil {
			return nil, err
		}
		cache[key] = perms
		return perms, nil
	}
	key := fmt.Sprintf("%s:%s:%d:org:%s", req.Subject.TenantID, req.Subject.UserID, req.Subject.RolesVersion, req.OrgID)
	if perms, ok := cache[key]; ok {
		return perms, nil
	}
	perms, err := a.GetEffectivePermissionsForPrincipalScoped(ctx, req.Subject, req.OrgID)
	if err != nil {
		return nil, err
	}
	cache[key] = perms
	return perms, nil
}

func contains(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

// userHasAdminRole checks whether the user has any admin-level role that may require MFA.
func userHasAdminRole(roles []string) bool {
	adminRoles := []string{"platform_admin", "tenant_owner", "tenant_admin", "security_admin"}
	for _, r := range roles {
		for _, admin := range adminRoles {
			if r == admin {
				return true
			}
		}
	}
	return false
}
