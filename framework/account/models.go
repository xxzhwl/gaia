package account

import (
	"time"

	"gorm.io/gorm"
)

const (
	UserStatusNormal   = "normal"
	UserStatusDisabled = "disabled"
	UserStatusLocked   = "locked"
	UserStatusPending  = "pending"
	UserStatusDeleted  = "deleted"

	CredentialPassword = "password"

	SessionActive      = "active"
	SessionRevoked     = "revoked"
	SessionExpired     = "expired"
	SessionRiskBlocked = "risk_blocked"

	RefreshActive  = "active"
	RefreshUsed    = "used"
	RefreshRevoked = "revoked"
	RefreshReused  = "reused"
	RefreshExpired = "expired"
)

// User 用户模型，包含账户信息和认证版本控制。
type User struct {
	ID              string         `json:"id" gorm:"size:36;primaryKey"`
	TenantID        string         `json:"tenant_id" gorm:"size:64;not null;default:default;uniqueIndex:uniq_acct_users_username,priority:1;uniqueIndex:uniq_acct_users_email,priority:1;uniqueIndex:uniq_acct_users_phone,priority:1;index:idx_acct_users_tenant_status_created,priority:1"`
	Username        string         `json:"username" gorm:"size:80;not null;uniqueIndex:uniq_acct_users_username,priority:2"`
	Email           *string        `json:"email" gorm:"size:160;uniqueIndex:uniq_acct_users_email,priority:2"`
	EmailVerifiedAt *time.Time     `json:"email_verified_at"`
	Phone           *string        `json:"phone" gorm:"size:32;uniqueIndex:uniq_acct_users_phone,priority:2"`
	PhoneVerifiedAt *time.Time     `json:"phone_verified_at"`
	Nickname        string         `json:"nickname" gorm:"size:80"`
	AvatarURL       string         `json:"avatar_url" gorm:"size:512"`
	Status          string         `json:"status" gorm:"size:20;not null;default:normal;index:idx_acct_users_tenant_status_created,priority:2"`
	LockedUntil     *time.Time     `json:"locked_until"`
	AuthVersion     int64          `json:"auth_version" gorm:"not null;default:1"`
	RolesVersion    int64          `json:"roles_version" gorm:"not null;default:1"`
	ProfileVersion  int64          `json:"profile_version" gorm:"not null;default:1"`
	LastLoginAt     *time.Time     `json:"last_login_at"`
	LastLoginIP     string         `json:"last_login_ip" gorm:"size:64"`
	CreatedAt       time.Time      `json:"created_at" gorm:"index:idx_acct_users_tenant_status_created,priority:3"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index"`
}

func (User) TableName() string { return "acct_users" }

// Credential 用户凭证，支持密码、TOTP、恢复码等多种类型。
type Credential struct {
	ID         string     `json:"id" gorm:"size:36;primaryKey"`
	TenantID   string     `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_credentials_identifier,priority:1;index:idx_acct_credentials_user_type,priority:1"`
	UserID     string     `json:"user_id" gorm:"size:36;not null;index:idx_acct_credentials_user_type,priority:2"`
	Type       string     `json:"type" gorm:"size:32;not null;uniqueIndex:uniq_acct_credentials_identifier,priority:2;index:idx_acct_credentials_user_type,priority:3"`
	Identifier string     `json:"identifier" gorm:"size:220;not null;uniqueIndex:uniq_acct_credentials_identifier,priority:3"`
	SecretHash string     `json:"-" gorm:"type:text;not null"`
	SecretMeta string     `json:"-" gorm:"type:json"`
	Enabled    bool       `json:"enabled" gorm:"not null;default:true"`
	LastUsedAt *time.Time `json:"last_used_at"`
	// LastUsedCounter 上次成功验证使用的 TOTP 时间窗口序号（now/period）。
	// 用于实现 RFC 6238 §5.2 的一次性约束：拒绝同一码或更早窗口码的重放。
	// 仅对 type=totp 的凭证有意义；recovery_code 通过 enabled=false 实现一次性。
	LastUsedCounter int64     `json:"last_used_counter" gorm:"not null;default:0"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// BeforeCreate 确保 SecretMeta 字段为有效 JSON（MySQL JSON 列不接受空字符串）。
func (c *Credential) BeforeCreate(tx *gorm.DB) error {
	if c.SecretMeta == "" {
		c.SecretMeta = "{}"
	}
	return nil
}

func (Credential) TableName() string { return "acct_credentials" }

// Session 用户会话，记录设备信息和活跃状态。
type Session struct {
	ID            string     `json:"id" gorm:"size:36;primaryKey"`
	TenantID      string     `json:"tenant_id" gorm:"size:64;not null;index:idx_acct_sessions_user_status,priority:1"`
	UserID        string     `json:"user_id" gorm:"size:36;not null;index:idx_acct_sessions_user_status,priority:2"`
	FamilyID      string     `json:"family_id" gorm:"size:36;not null;index:idx_acct_sessions_family"`
	DeviceID      string     `json:"device_id" gorm:"size:128"`
	DeviceName    string     `json:"device_name" gorm:"size:128"`
	IP            string     `json:"ip" gorm:"size:64"`
	UserAgentHash string     `json:"user_agent_hash" gorm:"size:64"`
	Status        string     `json:"status" gorm:"size:24;not null;default:active;index:idx_acct_sessions_user_status,priority:3"`
	ExpiresAt     time.Time  `json:"expires_at" gorm:"not null;index:idx_acct_sessions_expires"`
	RevokedAt     *time.Time `json:"revoked_at"`
	// MFASatisfiedAt 当前会话最近一次成功完成 step-up MFA 的时间戳。
	// 业务侧通过 AuthService.RequireStepUp 校验 (now - MFASatisfiedAt) <= Config.StepUpWindow，
	// 否则拒绝敏感操作并要求重新走 step-up 流程。
	// 该标记**严格按 sid 维度**：用户在 Safari 上 step-up 不会让 Chrome 上的会话获得敏感操作权限。
	// 当 session 被吊销 / 改密 / 风控时整条记录失效，标记自然作废。
	MFASatisfiedAt *time.Time `json:"mfa_satisfied_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Session) TableName() string { return "acct_sessions" }

// RefreshToken 刷新令牌，支持令牌族和重放检测。
type RefreshToken struct {
	ID           string     `json:"id" gorm:"size:36;primaryKey"`
	TenantID     string     `json:"tenant_id" gorm:"size:64;not null"`
	SessionID    string     `json:"session_id" gorm:"size:36;not null;index:idx_acct_refresh_session_status,priority:1"`
	FamilyID     string     `json:"family_id" gorm:"size:36;not null;index:idx_acct_refresh_family"`
	TokenHash    string     `json:"-" gorm:"size:64;not null;uniqueIndex:uniq_acct_refresh_hash"`
	PreviousHash string     `json:"-" gorm:"size:64"`
	Status       string     `json:"status" gorm:"size:24;not null;default:active;index:idx_acct_refresh_session_status,priority:2"`
	ExpiresAt    time.Time  `json:"expires_at" gorm:"not null;index:idx_acct_refresh_expires"`
	UsedAt       *time.Time `json:"used_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (RefreshToken) TableName() string { return "acct_refresh_tokens" }

// Role 角色，定义了一组权限的集合。
type Role struct {
	ID          string    `json:"id" gorm:"size:36;primaryKey"`
	TenantID    string    `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_roles_code,priority:1"`
	Code        string    `json:"code" gorm:"size:100;not null;uniqueIndex:uniq_acct_roles_code,priority:2"`
	Name        string    `json:"name" gorm:"size:100;not null"`
	Description string    `json:"description" gorm:"size:255"`
	IsSystem    bool      `json:"is_system" gorm:"not null;default:false"`
	Status      string    `json:"status" gorm:"size:20;not null;default:enabled"`
	Version     int64     `json:"version" gorm:"not null;default:1"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Role) TableName() string { return "acct_roles" }

// Permission 权限，表示对某类资源执行特定操作的能力。
type Permission struct {
	ID           string    `json:"id" gorm:"size:36;primaryKey"`
	TenantID     string    `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_permissions_code,priority:1"`
	Code         string    `json:"code" gorm:"size:160;not null;uniqueIndex:uniq_acct_permissions_code,priority:2"`
	ResourceType string    `json:"resource_type" gorm:"size:80"`
	Action       string    `json:"action" gorm:"size:80"`
	Description  string    `json:"description" gorm:"size:255"`
	Status       string    `json:"status" gorm:"size:20;not null;default:enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Permission) TableName() string { return "acct_permissions" }

// UserRole 用户与角色的关联，支持作用域限定。
type UserRole struct {
	ID        string    `json:"id" gorm:"size:36;primaryKey"`
	TenantID  string    `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_user_roles,priority:1"`
	UserID    string    `json:"user_id" gorm:"size:36;not null;uniqueIndex:uniq_acct_user_roles,priority:2;index:idx_acct_user_roles_user"`
	RoleID    string    `json:"role_id" gorm:"size:36;not null;uniqueIndex:uniq_acct_user_roles,priority:3;index:idx_acct_user_roles_role"`
	ScopeType string    `json:"scope_type" gorm:"size:32;not null;default:tenant;uniqueIndex:uniq_acct_user_roles,priority:4"`
	ScopeID   string    `json:"scope_id" gorm:"size:64;not null;default:default;uniqueIndex:uniq_acct_user_roles,priority:5"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserRole) TableName() string { return "acct_user_roles" }

// RolePermission 角色与权限的关联。
type RolePermission struct {
	ID           string    `json:"id" gorm:"size:36;primaryKey"`
	RoleID       string    `json:"role_id" gorm:"size:36;not null;uniqueIndex:uniq_acct_role_permissions,priority:1;index:idx_acct_role_permissions_role"`
	PermissionID string    `json:"permission_id" gorm:"size:36;not null;uniqueIndex:uniq_acct_role_permissions,priority:2;index:idx_acct_role_permissions_permission"`
	CreatedAt    time.Time `json:"created_at"`
}

func (RolePermission) TableName() string { return "acct_role_permissions" }

// AuditLog 审计日志，记录认证和授权事件。
type AuditLog struct {
	ID        string    `json:"id" gorm:"size:36;primaryKey"`
	TenantID  string    `json:"tenant_id" gorm:"size:64;not null;index:idx_acct_audit_tenant_created,priority:1"`
	UserID    string    `json:"user_id" gorm:"size:36;index"`
	Event     string    `json:"event" gorm:"size:80;not null;index"`
	IP        string    `json:"ip" gorm:"size:64"`
	UserAgent string    `json:"user_agent" gorm:"size:512"`
	Status    string    `json:"status" gorm:"size:20;not null"`
	Reason    string    `json:"reason" gorm:"size:255"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_acct_audit_tenant_created,priority:2"`
}

func (AuditLog) TableName() string { return "acct_audit_logs" }

// AuditLogArchive 归档审计日志，与 AuditLog 结构相同但使用独立表。
type AuditLogArchive struct {
	ID        string    `json:"id" gorm:"size:36;primaryKey"`
	TenantID  string    `json:"tenant_id" gorm:"size:64;not null;index:idx_acct_audit_archive_tenant_created,priority:1"`
	UserID    string    `json:"user_id" gorm:"size:36;index"`
	Event     string    `json:"event" gorm:"size:80;not null;index"`
	IP        string    `json:"ip" gorm:"size:64"`
	UserAgent string    `json:"user_agent" gorm:"size:512"`
	Status    string    `json:"status" gorm:"size:20;not null"`
	Reason    string    `json:"reason" gorm:"size:255"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_acct_audit_archive_tenant_created,priority:2"`
	// ArchivedAt 记录归档时间。
	ArchivedAt time.Time `json:"archived_at" gorm:"not null"`
}

func (AuditLogArchive) TableName() string { return "acct_audit_log_archives" }
