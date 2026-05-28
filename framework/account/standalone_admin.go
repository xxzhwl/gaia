package account

import (
	"time"

	"github.com/cloudwego/hertz/pkg/route"
	"github.com/xxzhwl/gaia/framework/server"
)

// registerAdminRoutes 注册全部管理端路由（/admin/*）。
// 所有路由要求登录，并按操作粒度做权限校验。
//
// 权限码约定（默认未在 seedDefaults 中创建，需要租户管理员手动配置或通过迁移脚本灌入）：
//   admin.user.{list,read,create,update,delete}
//   admin.role.{list,read,create,update,delete,assign}
//   admin.permission.{list,create,update,delete,assign}
//   admin.session.{read,revoke}
//   admin.audit.{read,restore,archive}
//   admin.policy.{list,create,update,delete}
//   admin.org.{create,update,delete,assign}
func (s *StandaloneService) registerAdminRoutes(r *route.RouterGroup) {
	admin := r.Group("/admin")
	admin.Use(s.m.Middleware().Authenticate())

	mw := s.m.Middleware()

	// ===== 用户管理 =====
	admin.GET("/users", mw.RequirePermission("admin.user.list"), s.handler(s.handleAdminListUsers))
	admin.POST("/users", mw.RequirePermission("admin.user.create"), s.handler(s.handleAdminCreateUser))
	admin.PUT("/users/:id/status", mw.RequirePermission("admin.user.update"), s.handler(s.handleAdminUpdateUserStatus))
	admin.POST("/users/:id/reset-mfa", mw.RequirePermission("admin.user.update"), s.handler(s.handleAdminResetUserMFA))
	admin.GET("/users/:id/sessions", mw.RequirePermission("admin.user.read"), s.handler(s.handleAdminListUserSessions))
	admin.GET("/users/:id/permissions", mw.RequirePermission("admin.user.read"), s.handler(s.handleAdminGetUserPermissions))
	admin.POST("/users/:id/roles/:role_id", mw.RequirePermission("admin.role.assign"), s.handler(s.handleAdminAssignUserRole))
	admin.DELETE("/users/:id/roles/:role_id", mw.RequirePermission("admin.role.assign"), s.handler(s.handleAdminRemoveUserRole))

	// ===== 角色管理 =====
	admin.GET("/roles", mw.RequirePermission("admin.role.list"), s.handler(s.handleAdminListRoles))
	admin.POST("/roles", mw.RequirePermission("admin.role.create"), s.handler(s.handleAdminCreateRole))
	admin.PUT("/roles/:id", mw.RequirePermission("admin.role.update"), s.handler(s.handleAdminUpdateRole))
	admin.DELETE("/roles/:id", mw.RequirePermission("admin.role.delete"), s.handler(s.handleAdminDeleteRole))
	admin.GET("/roles/:id/permissions", mw.RequirePermission("admin.role.read"), s.handler(s.handleAdminGetRolePermissions))
	admin.POST("/roles/:id/permissions/:permission_id", mw.RequirePermission("admin.permission.assign"), s.handler(s.handleAdminAssignPermissionToRole))
	admin.DELETE("/roles/:id/permissions/:permission_id", mw.RequirePermission("admin.permission.assign"), s.handler(s.handleAdminRemovePermissionFromRole))

	// ===== 权限管理 =====
	admin.GET("/permissions", mw.RequirePermission("admin.permission.list"), s.handler(s.handleAdminListPermissions))
	admin.POST("/permissions", mw.RequirePermission("admin.permission.create"), s.handler(s.handleAdminCreatePermission))
	admin.PUT("/permissions/:id", mw.RequirePermission("admin.permission.update"), s.handler(s.handleAdminUpdatePermission))
	admin.DELETE("/permissions/:id", mw.RequirePermission("admin.permission.delete"), s.handler(s.handleAdminDeletePermission))

	// ===== 会话管理 =====
	admin.DELETE("/sessions/:id", mw.RequirePermission("admin.session.revoke"), s.handler(s.handleAdminRevokeSession))

	// ===== 审计 =====
	admin.GET("/audit/events", mw.RequirePermission("admin.audit.read"), s.handler(s.handleAdminListAuditEvents))
	admin.GET("/audit/:id", mw.RequirePermission("admin.audit.read"), s.handler(s.handleAdminGetAuditLog))
	admin.POST("/audit/restore/:id", mw.RequirePermission("admin.audit.restore"), s.handler(s.handleAdminRestoreAuditLog))

	// ===== 策略 =====
	admin.GET("/policies", mw.RequirePermission("admin.policy.list"), s.handler(s.handleAdminListPolicies))
	admin.POST("/policies", mw.RequirePermission("admin.policy.create"), s.handler(s.handleAdminCreatePolicy))
	admin.PUT("/policies/:id", mw.RequirePermission("admin.policy.update"), s.handler(s.handleAdminUpdatePolicy))
	admin.DELETE("/policies/:id", mw.RequirePermission("admin.policy.delete"), s.handler(s.handleAdminDeletePolicy))

	// ===== 组织 =====
	admin.POST("/orgs", mw.RequirePermission("admin.org.create"), s.handler(s.handleAdminCreateOrg))
	admin.PUT("/orgs/:id", mw.RequirePermission("admin.org.update"), s.handler(s.handleAdminUpdateOrg))
	admin.DELETE("/orgs/:id", mw.RequirePermission("admin.org.delete"), s.handler(s.handleAdminDeleteOrg))
	admin.POST("/orgs/:id/members/:user_id/roles/:role_id", mw.RequirePermission("admin.org.assign"), s.handler(s.handleAdminAssignOrgRole))
	admin.DELETE("/orgs/:id/members/:user_id/roles/:role_id", mw.RequirePermission("admin.org.assign"), s.handler(s.handleAdminRemoveOrgRole))
}

// ============================================================
// 用户管理
// ============================================================

func (s *StandaloneService) handleAdminListUsers(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID string `json:"tenant_id"`
		Status   string `json:"status"`
		Keyword  string `json:"keyword"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
	}
	_ = req.BindJson(&body)
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return s.m.Admin().ListUsers(req.TraceContext, ListUsersRequest{
		TenantID: body.TenantID,
		Status:   body.Status,
		Keyword:  body.Keyword,
		Page:     body.Page,
		PageSize: body.PageSize,
	})
}

func (s *StandaloneService) handleAdminCreateUser(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID string `json:"tenant_id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Nickname string `json:"nickname"`
		Status   string `json:"status"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	user := &User{
		TenantID: body.TenantID,
		Username: body.Username,
		Nickname: body.Nickname,
		Status:   body.Status,
	}
	if body.Email != "" {
		user.Email = &body.Email
	}
	if body.Phone != "" {
		user.Phone = &body.Phone
	}
	if user.Status == "" {
		user.Status = UserStatusNormal
	}
	if err := s.m.Admin().CreateUser(req.TraceContext, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *StandaloneService) handleAdminUpdateUserStatus(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID string `json:"tenant_id"`
		Status   string `json:"status"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return nil, s.m.Admin().UpdateUserStatus(req.TraceContext, body.TenantID, req.GetUrlParam("id"), body.Status)
}

func (s *StandaloneService) handleAdminResetUserMFA(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	return nil, s.m.Admin().ResetUserMFA(req.TraceContext, tenantID, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleAdminListUserSessions(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	return s.m.Admin().ListUserSessions(req.TraceContext, tenantID, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleAdminGetUserPermissions(req server.Request) (any, error) {
	perms, err := s.m.Authorizer().GetEffectivePermissions(req.TraceContext, req.GetUrlParam("id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"user_id": req.GetUrlParam("id"), "permissions": perms}, nil
}

func (s *StandaloneService) handleAdminAssignUserRole(req server.Request) (any, error) {
	return nil, s.m.Admin().AssignUserRole(req.TraceContext, req.GetUrlParam("id"), req.GetUrlParam("role_id"))
}

func (s *StandaloneService) handleAdminRemoveUserRole(req server.Request) (any, error) {
	return nil, s.m.Admin().RemoveUserRole(req.TraceContext, req.GetUrlParam("id"), req.GetUrlParam("role_id"))
}

// ============================================================
// 角色管理
// ============================================================

func (s *StandaloneService) handleAdminListRoles(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	return s.m.Admin().ListRoles(req.TraceContext, tenantID)
}

func (s *StandaloneService) handleAdminCreateRole(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID    string `json:"tenant_id"`
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return s.m.Admin().CreateRole(req.TraceContext, CreateRoleRequest{
		TenantID:    body.TenantID,
		Code:        body.Code,
		Name:        body.Name,
		Description: body.Description,
	})
}

func (s *StandaloneService) handleAdminUpdateRole(req server.Request) (any, error) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Admin().UpdateRole(req.TraceContext, req.GetUrlParam("id"), body.Name, body.Description)
}

func (s *StandaloneService) handleAdminDeleteRole(req server.Request) (any, error) {
	return nil, s.m.Admin().DeleteRole(req.TraceContext, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleAdminGetRolePermissions(req server.Request) (any, error) {
	return s.m.Admin().GetRolePermissions(req.TraceContext, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleAdminAssignPermissionToRole(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	return nil, s.m.Admin().AssignPermissionToRole(req.TraceContext, tenantID, req.GetUrlParam("id"), req.GetUrlParam("permission_id"))
}

func (s *StandaloneService) handleAdminRemovePermissionFromRole(req server.Request) (any, error) {
	return nil, s.m.Admin().RemovePermissionFromRole(req.TraceContext, req.GetUrlParam("id"), req.GetUrlParam("permission_id"))
}

// ============================================================
// 权限管理
// ============================================================

func (s *StandaloneService) handleAdminListPermissions(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID     string `json:"tenant_id"`
		ResourceType string `json:"resource_type"`
		Action       string `json:"action"`
		Page         int    `json:"page"`
		PageSize     int    `json:"page_size"`
	}
	_ = req.BindJson(&body)
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return s.m.Admin().ListPermissions(req.TraceContext, PermissionListRequest{
		TenantID:     body.TenantID,
		ResourceType: body.ResourceType,
		Action:       body.Action,
		Page:         body.Page,
		PageSize:     body.PageSize,
	})
}

func (s *StandaloneService) handleAdminCreatePermission(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID     string `json:"tenant_id"`
		Code         string `json:"code"`
		Description  string `json:"description"`
		ResourceType string `json:"resource_type"`
		Action       string `json:"action"`
		Status       string `json:"status"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	if body.Status == "" {
		body.Status = "enabled"
	}
	perm := &Permission{
		TenantID:     body.TenantID,
		Code:         body.Code,
		Description:  body.Description,
		ResourceType: body.ResourceType,
		Action:       body.Action,
		Status:       body.Status,
	}
	if err := s.m.Admin().CreatePermission(req.TraceContext, perm); err != nil {
		return nil, err
	}
	return perm, nil
}

func (s *StandaloneService) handleAdminUpdatePermission(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID    string `json:"tenant_id"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return nil, s.m.Admin().UpdatePermission(req.TraceContext, body.TenantID, req.GetUrlParam("id"), body.Description, body.Status)
}

func (s *StandaloneService) handleAdminDeletePermission(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	return nil, s.m.Admin().DeletePermission(req.TraceContext, tenantID, req.GetUrlParam("id"))
}

// ============================================================
// 会话管理（管理端）
// ============================================================

func (s *StandaloneService) handleAdminRevokeSession(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	return nil, s.m.Admin().RevokeSession(req.TraceContext, tenantID, req.GetUrlParam("id"))
}

// ============================================================
// 审计
// ============================================================

func (s *StandaloneService) handleAdminGetAuditLog(req server.Request) (any, error) {
	return s.m.Audit().GetByID(req.TraceContext, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleAdminListAuditEvents(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	now := time.Now()
	return s.m.Audit().ListEvents(req.TraceContext, tenantID, now.AddDate(0, 0, -30), now)
}

func (s *StandaloneService) handleAdminRestoreAuditLog(req server.Request) (any, error) {
	return nil, s.m.Audit().RestoreFromArchive(req.TraceContext, req.GetUrlParam("id"))
}

// ============================================================
// 策略
// ============================================================

func (s *StandaloneService) handleAdminListPolicies(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	tenantID := req.GetUrlQuery("tenant_id")
	if tenantID == "" {
		tenantID = p.TenantID
	}
	return s.m.Policies().ListPolicies(req.TraceContext, tenantID)
}

func (s *StandaloneService) handleAdminCreatePolicy(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body createPolicyBody
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return s.m.Policies().CreatePolicy(req.TraceContext, body.toRequest())
}

func (s *StandaloneService) handleAdminUpdatePolicy(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body createPolicyBody
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return nil, s.m.Policies().UpdatePolicy(req.TraceContext, req.GetUrlParam("id"), body.toRequest())
}

func (s *StandaloneService) handleAdminDeletePolicy(req server.Request) (any, error) {
	return nil, s.m.Policies().DeletePolicy(req.TraceContext, req.GetUrlParam("id"))
}

type createPolicyBody struct {
	TenantID     string `json:"tenant_id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	Effect       string `json:"effect"`
	Expression   string `json:"expression"`
	ResourceType string `json:"resource_type"`
	Action       string `json:"action"`
	Priority     int    `json:"priority"`
}

func (b createPolicyBody) toRequest() CreatePolicyRequest {
	return CreatePolicyRequest{
		TenantID:     b.TenantID,
		Code:         b.Code,
		Name:         b.Name,
		Effect:       b.Effect,
		Expression:   b.Expression,
		ResourceType: b.ResourceType,
		Action:       b.Action,
		Priority:     b.Priority,
	}
}

// ============================================================
// 组织
// ============================================================

func (s *StandaloneService) handleAdminCreateOrg(req server.Request) (any, error) {
	p := contextPrincipal(req)
	if p == nil {
		return nil, accountError(ErrInvalidToken, "未认证")
	}
	var body struct {
		TenantID    string `json:"tenant_id"`
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
		ParentID    string `json:"parent_id"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	if body.TenantID == "" {
		body.TenantID = p.TenantID
	}
	return s.m.Organizations().CreateOrg(req.TraceContext, CreateOrgRequest{
		TenantID:    body.TenantID,
		Code:        body.Code,
		Name:        body.Name,
		Description: body.Description,
		ParentID:    body.ParentID,
	})
}

func (s *StandaloneService) handleAdminUpdateOrg(req server.Request) (any, error) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := req.BindJson(&body); err != nil {
		return nil, err
	}
	return nil, s.m.Organizations().UpdateOrg(req.TraceContext, req.GetUrlParam("id"), body.Name, body.Description)
}

func (s *StandaloneService) handleAdminDeleteOrg(req server.Request) (any, error) {
	return nil, s.m.Organizations().DeleteOrg(req.TraceContext, req.GetUrlParam("id"))
}

func (s *StandaloneService) handleAdminAssignOrgRole(req server.Request) (any, error) {
	return nil, s.m.Organizations().AssignOrgRole(req.TraceContext,
		req.GetUrlParam("user_id"),
		req.GetUrlParam("id"),
		req.GetUrlParam("role_id"),
	)
}

func (s *StandaloneService) handleAdminRemoveOrgRole(req server.Request) (any, error) {
	return nil, s.m.Organizations().RemoveOrgRole(req.TraceContext,
		req.GetUrlParam("user_id"),
		req.GetUrlParam("id"),
		req.GetUrlParam("role_id"),
	)
}
