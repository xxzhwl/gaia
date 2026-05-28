package account

import (
	"context"
	"fmt"

	"github.com/xxzhwl/gaia"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Manager 是账户模块的核心管理器，持有所有子服务和配置。
type Manager struct {
	cfg          Config
	db           *gorm.DB
	cache        Cache
	auth         *AuthService
	users        *UserService
	roles        *RoleService
	authorizer   *Authorizer
	middleware   *Middleware
	verification *VerificationService
	risk         *RiskService
	mfa          *MFAService
	oauth        *OAuthService
	auditSvc     *AuditService
	sessionSvc   *SessionService
	orgSvc       *OrgService
	idpSvc       *IdpService
	passkeySvc   *PasskeyService
	apiTokenSvc  *APITokenService
	consentSvc   *ConsentService
	health       *HealthService
	metrics      *AccountMetrics
	tracer       trace.Tracer
	auditCh      chan *AuditLog
	auditDone    chan struct{}
}

func newManager(cfg Config) *Manager {
	bufSize := cfg.Audit.AsyncBufferSize
	if bufSize <= 0 {
		bufSize = 100
	}
	m := &Manager{cfg: cfg, db: cfg.DB, cache: cfg.Cache, auditCh: make(chan *AuditLog, bufSize)}
	m.auth = &AuthService{m: m}
	m.users = &UserService{m: m}
	m.roles = &RoleService{m: m}
	m.authorizer = &Authorizer{m: m}
	m.middleware = &Middleware{m: m}
	m.verification = &VerificationService{m: m}
	m.risk = &RiskService{m: m}
	m.mfa = &MFAService{m: m}
	m.oauth = &OAuthService{m: m}
	m.auditSvc = &AuditService{m: m}
	m.sessionSvc = &SessionService{m: m}
	m.orgSvc = &OrgService{m: m}
	m.idpSvc = &IdpService{m: m}
	m.passkeySvc = &PasskeyService{m: m}
	m.apiTokenSvc = &APITokenService{m: m}
	m.consentSvc = &ConsentService{m: m}
	m.health = &HealthService{m: m}
	m.metrics = initAccountMetrics()
	m.tracer = otel.Tracer("github.com/xxzhwl/gaia/framework/account")
	return m
}

// Bootstrap 运行所有账户表的自动迁移并填充默认角色和权限。
func (m *Manager) Bootstrap(ctx context.Context) error {
	if err := m.db.WithContext(ctx).AutoMigrate(
		&User{},
		&Credential{},
		&Session{},
		&RefreshToken{},
		&Role{},
		&Permission{},
		&UserRole{},
		&RolePermission{},
		&AuditLog{},
		&AuditLogArchive{},
		&VerificationChallenge{},
		&AccessTokenDenylist{},
		&MFAChallenge{},
		&OAuthAccount{},
		&Organization{},
		&IdpClient{},
		&AuthorizationCode{},
		&PasskeyCredential{},
		&OutboxEvent{},
		&Policy{},
		&PersonalAccessToken{},
		&AuthorizedApp{},
		&UserConsent{},
	); err != nil {
		return fmt.Errorf("account migrate tables: %w", err)
	}
	return m.seedDefaults(ctx)
}

// Close 释放底层资源，包括：
//   - 关闭并刷新异步审计写入器（优雅停止）
//   - 释放底层缓存连接（如果实现了 Closer 接口）
func (m *Manager) Close(ctx context.Context) error {
	// 关闭异步审计写入器，等待其刷新所有未处理日志
	if m.cfg.Audit.AsyncWrite {
		close(m.auditCh)
		select {
		case <-m.auditDone:
		case <-ctx.Done():
		}
	}
	if closer, ok := m.cache.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// StartAuditWriter 启动后台协程，处理异步审计日志写入。
// 仅在 Config.Audit.AsyncWrite 为 true 时需要调用。
// 可通过 Close 方法优雅停止。
func (m *Manager) StartAuditWriter(ctx context.Context) {
	if m.auditDone != nil {
		return // 已经在运行
	}
	m.auditDone = make(chan struct{})
	go func() {
		defer close(m.auditDone)
		for entry := range m.auditCh {
			if err := m.db.WithContext(context.Background()).Create(entry).Error; err != nil {
				gaia.WarnF("[account] async audit write failed: event=%s user_id=%s err=%v", entry.Event, entry.UserID, err)
			}
		}
	}()
	gaia.InfoF("[account] async audit writer started (buffer=%d)", cap(m.auditCh))
}

// Auth 返回 AuthService，用于认证操作。
func (m *Manager) Auth() *AuthService {
	return m.auth
}

// Users 返回 UserService，用于用户资料管理。
func (m *Manager) Users() *UserService {
	return m.users
}

// Roles 返回 RoleService，用于角色管理。
func (m *Manager) Roles() *RoleService {
	return m.roles
}

// Authorizer 返回 Authorizer，用于权限检查。
func (m *Manager) Authorizer() *Authorizer {
	return m.authorizer
}

// Middleware 返回 Middleware，用于 HTTP 处理器集成。
func (m *Manager) Middleware() *Middleware {
	return m.middleware
}

// Permissions 返回 PermissionService，用于权限点管理。
func (m *Manager) Permissions() *PermissionService {
	return &PermissionService{m: m}
}

// Admin 返回 AdminService，用于管理端操作。
func (m *Manager) Admin() *AdminService {
	return &AdminService{m: m}
}

// Policies 返回 PolicyService，用于 ABAC 策略管理。
func (m *Manager) Policies() *PolicyService {
	return &PolicyService{m: m}
}

// Verification 返回 VerificationService，用于邮件/短信验证码验证。
func (m *Manager) Verification() *VerificationService {
	return m.verification
}

// MFA 返回 MFAService，用于 TOTP、恢复码和 step-up 验证。
func (m *Manager) MFA() *MFAService {
	return m.mfa
}

// Organizations 返回 OrgService，用于多租户组织/部门管理。
func (m *Manager) Organizations() *OrgService {
	return m.orgSvc
}

// OAuth 返回 OAuthService，用于 OAuth/OIDC 登录流程。
func (m *Manager) OAuth() *OAuthService {
	return m.oauth
}

// Audit 返回 AuditService，用于审计日志查询。
func (m *Manager) Audit() *AuditService {
	return m.auditSvc
}

// Sessions 返回 SessionService，用于会话管理。
func (m *Manager) Sessions() *SessionService {
	return m.sessionSvc
}

// Metrics 返回 AccountMetrics，用于指标录制。
func (m *Manager) Metrics() *AccountMetrics {
	return m.metrics
}

// Health 返回 HealthService，用于 SDK 各组件的健康检查。
func (m *Manager) Health() *HealthService {
	return m.health
}

// IdP 返回 IdpService，用于 OAuth 2.0 / OIDC 身份提供商管理。
func (m *Manager) IdP() *IdpService {
	return m.idpSvc
}

// Passkey 返回 PasskeyService，用于 WebAuthn 通行密钥管理。
func (m *Manager) Passkey() *PasskeyService {
	return m.passkeySvc
}

// APITokens 返回 APITokenService，用于个人访问令牌管理。
func (m *Manager) APITokens() *APITokenService {
	return m.apiTokenSvc
}

// Consents 返回 ConsentService，用于隐私协议版本签署记录。
func (m *Manager) Consents() *ConsentService {
	return m.consentSvc
}

// Cleanup 清理过期的刷新令牌、会话、验证挑战和黑名单条目。
// 使用分布式锁防止多个实例同时执行清理。
// 委托给 cleanupAll 统一实现。
func (m *Manager) Cleanup(ctx context.Context) error {
	return m.cleanupAll(ctx)
}

// tenantID 将空租户 ID 解析为配置的默认租户。
func (m *Manager) tenantID(tenantID string) string {
	if tenantID == "" {
		return m.cfg.DefaultTenantID
	}
	return tenantID
}

func (m *Manager) phoneBindingRequired(user *User) bool {
	return m.cfg.AccountPolicy.RequireVerifiedPhone && user != nil && user.PhoneVerifiedAt == nil
}

// seedDefaults 在首次启动时插入系统角色（platform_admin、user 等）和权限（如果不存在）。
func (m *Manager) seedDefaults(ctx context.Context) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		roles := []Role{
			{Code: "platform_admin", Name: "平台管理员", Description: "平台级管理权限", IsSystem: true},
			{Code: "tenant_owner", Name: "租户所有者", Description: "租户所有者", IsSystem: true},
			{Code: "tenant_admin", Name: "租户管理员", Description: "租户管理权限", IsSystem: true},
			{Code: "security_admin", Name: "安全管理员", Description: "安全与审计权限", IsSystem: true},
			{Code: "user", Name: "普通用户", Description: "普通登录用户", IsSystem: true},
			{Code: "guest", Name: "访客", Description: "未登录访客", IsSystem: true},
		}
		for _, role := range roles {
			role.ID = newID()
			role.TenantID = m.cfg.DefaultTenantID
			role.Status = "enabled"
			role.Version = 1
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "code"}},
				DoUpdates: clause.AssignmentColumns([]string{"is_system", "status", "updated_at"}),
			}).Create(&role).Error; err != nil {
				return fmt.Errorf("seed role %s: %w", role.Code, err)
			}
		}

		perms := []Permission{
			{Code: "user:read", ResourceType: "user", Action: "read", Description: "查看用户"},
			{Code: "user:write", ResourceType: "user", Action: "write", Description: "修改用户"},
			{Code: "user:disable", ResourceType: "user", Action: "disable", Description: "禁用用户"},
			{Code: "user:delete", ResourceType: "user", Action: "delete", Description: "删除用户"},
			{Code: "role:read", ResourceType: "role", Action: "read", Description: "查看角色"},
			{Code: "role:write", ResourceType: "role", Action: "write", Description: "修改角色"},
			{Code: "permission:read", ResourceType: "permission", Action: "read", Description: "查看权限"},
			{Code: "permission:write", ResourceType: "permission", Action: "write", Description: "修改权限"},
			{Code: "session:read", ResourceType: "session", Action: "read", Description: "查看会话"},
			{Code: "session:revoke", ResourceType: "session", Action: "revoke", Description: "撤销会话"},
			{Code: "audit:read", ResourceType: "audit", Action: "read", Description: "查看审计"},
			{Code: "security:event:read", ResourceType: "security_event", Action: "read", Description: "查看安全事件"},
			{Code: "security:policy:write", ResourceType: "security_policy", Action: "write", Description: "修改安全策略"},
		}
		for _, perm := range perms {
			perm.ID = newID()
			perm.TenantID = m.cfg.DefaultTenantID
			perm.Status = "enabled"
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "code"}},
				DoUpdates: clause.AssignmentColumns([]string{"resource_type", "action", "status", "updated_at"}),
			}).Create(&perm).Error; err != nil {
				return fmt.Errorf("seed permission %s: %w", perm.Code, err)
			}
		}

		var admin Role
		if err := tx.Where("tenant_id = ? AND code = ?", m.cfg.DefaultTenantID, "platform_admin").First(&admin).Error; err != nil {
			return fmt.Errorf("load platform_admin role: %w", err)
		}
		var allPerms []Permission
		if err := tx.Where("tenant_id = ?", m.cfg.DefaultTenantID).Find(&allPerms).Error; err != nil {
			return fmt.Errorf("load seed permissions: %w", err)
		}
		for _, perm := range allPerms {
			rp := RolePermission{ID: newID(), RoleID: admin.ID, PermissionID: perm.ID}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "role_id"}, {Name: "permission_id"}},
				DoNothing: true,
			}).Create(&rp).Error; err != nil {
				return fmt.Errorf("seed platform_admin permission %s: %w", perm.Code, err)
			}
		}
		return nil
	})
}

// audit 写入审计日志条目。错误仅记录但不返回。
// 当 cfg.Audit.AsyncWrite 为 true 时，日志通过缓冲通道异步写入，
// 调用方必须调用 Close 确保所有日志在关闭前已刷新。
func (m *Manager) audit(ctx context.Context, tenantID, userID, event, status, reason, ip, userAgent string) {
	entry := &AuditLog{
		ID:        newID(),
		TenantID:  m.tenantID(tenantID),
		UserID:    userID,
		Event:     event,
		Status:    status,
		Reason:    reason,
		IP:        ip,
		UserAgent: userAgent,
	}
	if m.cfg.Audit.AsyncWrite {
		select {
		case m.auditCh <- entry:
		default:
			gaia.WarnF("[account] async audit channel full, dropping event=%s user_id=%s", event, userID)
		}
		return
	}
	if err := m.db.WithContext(ctx).Create(entry).Error; err != nil {
		gaia.WarnF("[account] write audit log failed: event=%s user_id=%s err=%v", event, userID, err)
	}
}
