package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	frameworkredis "github.com/xxzhwl/gaia/components/redis"
	"gorm.io/gorm"
)

const defaultTenantID = "default"

// Config 账户模块的全部配置参数。
type Config struct {
	AppID                          string
	Mode                           string
	DefaultTenantID                string
	DB                             *gorm.DB
	Cache                          Cache
	JWT                            JWTConfig
	Password                       PasswordConfig
	AccountPolicy                  AccountPolicyConfig
	AccessTokenTTL                 time.Duration
	RefreshTokenTTL                time.Duration
	PermissionCacheTTL             time.Duration
	PrincipalCacheMaxTTL           time.Duration
	// StepUpWindow MFA step-up 标记在当前 session 上的有效时长。
	// 默认 5 分钟。在此时间窗口内业务可重复执行敏感操作而无需重新输入 TOTP。
	// 转账等高风险动作建议业务侧另行设置 0 或更短窗口（即每次都要 step-up）。
	StepUpWindow                   time.Duration
	EnableAccessTokenDenylistCheck bool
	DenylistCacheTTL               time.Duration
	Verification                   VerificationConfig
	NotifyProvider                 NotifyProvider
	Risk                           RiskConfig
	Audit                          AuditConfig
	OAuthProviders                 map[string]OAuthProvider
	EventSubscribers               map[string]EventSubscriber
	TenantValidator                func(ctx context.Context, tenantID string) error
}

// JWTConfig JWT 签名和验证参数。
type JWTConfig struct {
	SecretKey string
	Issuer    string
	Audience  []string
	KeySet    *KeySet // optional, for asymmetric key rotation
}

// PasswordConfig 密码哈希参数。
type PasswordConfig struct {
	MinLength     int
	MaxLength     int
	Argon2Time    uint32
	Argon2Memory  uint32
	Argon2Threads uint8
}

// AccountPolicyConfig 控制账户进入系统前必须满足的身份绑定策略。
type AccountPolicyConfig struct {
	RequireVerifiedPhone bool
	RequireVerifiedEmail bool
	RequireAdminMFA      bool
}

// AuditConfig 审计日志配置。
type AuditConfig struct {
	// RetentionDays 指定审计日志的保留天数。超过此期限的日志将被清理任务删除。
	// 0 表示不自动清理。
	RetentionDays int
	// ArchiveRetentionDays 指定审计日志的归档天数。超过此期限的日志将在清理时从主表
	// 移动到归档表（acct_audit_log_archives），然后再执行 retention 删除。
	// 0 表示不启用归档。建议 ArchiveRetentionDays < RetentionDays。
	ArchiveRetentionDays int
	// AsyncWrite 启用异步写入模式。审计日志通过带缓冲的通道写入数据库，
	// 避免在高频路径（如认证中间件）上阻塞业务请求。
	// 启用后，调用方必须调用 Manager.Close() 确保所有待处理日志在关闭前已刷新。
	AsyncWrite bool
	// AsyncBufferSize 异步写入的通道缓冲区大小。仅 AsyncWrite=true 时生效。
	// 默认 100。
	AsyncBufferSize int
}

// New 创建 Manager，先应用默认值并验证配置。
func New(cfg Config) (*Manager, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return newManager(cfg), nil
}

// NewFramework 使用默认框架 MySQL 和 Redis 创建 Manager。
// 从全局 gaia 配置的默认键读取配置。
func NewFramework() (*Manager, error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return nil, fmt.Errorf("create framework mysql: %w", err)
	}
	redisClient := frameworkredis.NewFrameworkClient()
	return New(FrameworkConfig(db.GetGormDb(), redisClient))
}

// NewFrameworkWithSchema 使用指定的配置架构键创建 Manager，
// 用于 MySQL（如 "Account.Mysql"）和 Redis（如 "Account.Redis"）。
func NewFrameworkWithSchema(mysqlSchema, redisSchema string) (*Manager, error) {
	db, err := gaia.NewMysqlWithSchema(mysqlSchema)
	if err != nil {
		return nil, fmt.Errorf("create mysql with schema %s: %w", mysqlSchema, err)
	}
	redisClient := frameworkredis.NewClientWithSchema(redisSchema)
	return New(FrameworkConfig(db.GetGormDb(), redisClient))
}

// FrameworkConfig 从 gaia 框架配置构建 Config。
func FrameworkConfig(db *gorm.DB, redisClient *frameworkredis.Client) Config {
	var cache Cache
	if redisClient != nil {
		cache = NewRedisCache(redisClient, gaia.GetSafeConfStringWithDefault("Account.Redis.KeyPrefix", "acct:"))
	}
	return Config{
		AppID:           gaia.GetSafeConfStringWithDefault("Account.AppID", "gaia-account"),
		Mode:            gaia.GetSafeConfStringWithDefault("Account.Mode", "production"),
		DefaultTenantID: gaia.GetSafeConfStringWithDefault("Account.DefaultTenantID", defaultTenantID),
		DB:              db,
		Cache:           cache,
		JWT: JWTConfig{
			SecretKey: gaia.GetSafeConfStringWithDefault("Account.JWT.SecretKey", gaia.GetSafeConfString("JWT.SecretKey")),
			Issuer:    gaia.GetSafeConfStringWithDefault("Account.JWT.Issuer", gaia.GetSafeConfStringWithDefault("JWT.Issuer", "gaia-account")),
			Audience:  []string{gaia.GetSafeConfStringWithDefault("Account.JWT.Audience", gaia.GetSafeConfStringWithDefault("Account.AppID", "gaia-account"))},
		},
		Password: PasswordConfig{
			MinLength:     int(gaia.GetSafeConfInt64WithDefault("Account.Password.MinLength", 12)),
			MaxLength:     int(gaia.GetSafeConfInt64WithDefault("Account.Password.MaxLength", 128)),
			Argon2Time:    uint32(gaia.GetSafeConfInt64WithDefault("Account.Password.Argon2Time", 3)),
			Argon2Memory:  uint32(gaia.GetSafeConfInt64WithDefault("Account.Password.Argon2Memory", 64*1024)),
			Argon2Threads: uint8(gaia.GetSafeConfInt64WithDefault("Account.Password.Argon2Threads", 2)),
		},
		AccountPolicy: AccountPolicyConfig{
			RequireVerifiedPhone: gaia.GetSafeConfBoolWithDefault("Account.Policy.RequireVerifiedPhone", false),
			RequireVerifiedEmail: gaia.GetSafeConfBoolWithDefault("Account.Policy.RequireVerifiedEmail", false),
		},
		AccessTokenTTL:                 time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.JWT.AccessTokenExp", 15)),
		RefreshTokenTTL:                time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.JWT.RefreshTokenExp", 30*24*60)),
		PermissionCacheTTL:             time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.PermissionCacheTTL", 10)),
		PrincipalCacheMaxTTL:           time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.PrincipalCacheMaxTTL", 5)),
		StepUpWindow:                   time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.StepUpWindow", 5)),
		EnableAccessTokenDenylistCheck: gaia.GetSafeConfBoolWithDefault("Account.JWT.EnableAccessTokenDenylistCheck", false),
		DenylistCacheTTL:               time.Second * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.JWT.DenylistCacheTTL", 30)),
		Verification: VerificationConfig{
			CodeLength:          int(gaia.GetSafeConfInt64WithDefault("Account.Verification.CodeLength", 6)),
			CodeTTL:             time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.Verification.CodeTTL", 5)),
			MaxAttempts:         int(gaia.GetSafeConfInt64WithDefault("Account.Verification.MaxAttempts", 5)),
			SendInterval:        time.Second * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.Verification.SendInterval", 60)),
			MaxPerTargetPerHour: int(gaia.GetSafeConfInt64WithDefault("Account.Verification.MaxPerTargetPerHour", 5)),
			MaxPerIPPer10Min:    int(gaia.GetSafeConfInt64WithDefault("Account.Verification.MaxPerIPPer10Min", 20)),
		},
		Risk: RiskConfig{
			MaxLoginFailuresPerUser: int(gaia.GetSafeConfInt64WithDefault("Account.Risk.MaxLoginFailuresPerUser", 10)),
			MaxLoginFailuresPerIP:   int(gaia.GetSafeConfInt64WithDefault("Account.Risk.MaxLoginFailuresPerIP", 50)),
			FailureWindow:           time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.Risk.FailureWindow", 15)),
			LockoutDuration:         time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.Risk.LockoutDuration", 30)),
			BlockDuration:           time.Minute * time.Duration(gaia.GetSafeConfInt64WithDefault("Account.Risk.BlockDuration", 5)),
			EnableIPReputation:      gaia.GetSafeConfBoolWithDefault("Account.Risk.EnableIPReputation", false),
		},
		Audit: AuditConfig{
				RetentionDays:        int(gaia.GetSafeConfInt64WithDefault("Account.Audit.RetentionDays", 90)),
				ArchiveRetentionDays: int(gaia.GetSafeConfInt64WithDefault("Account.Audit.ArchiveRetentionDays", 0)),
				AsyncWrite:           gaia.GetSafeConfBoolWithDefault("Account.Audit.AsyncWrite", false),
				AsyncBufferSize:      int(gaia.GetSafeConfInt64WithDefault("Account.Audit.AsyncBufferSize", 100)),
			},
	}
}

// withDefaults 用合理的默认值填充零值字段。
func (c Config) withDefaults() Config {
	if c.AppID == "" {
		c.AppID = "gaia-account"
	}
	if c.Mode == "" {
		c.Mode = "production"
	}
	if c.Verification.CodeLength == 0 {
		vc := defaultVerificationConfig()
		c.Verification = vc
	}
	if c.Risk.MaxLoginFailuresPerUser == 0 {
		c.Risk = defaultRiskConfig()
	}
	if c.DefaultTenantID == "" {
		c.DefaultTenantID = defaultTenantID
	}
	if c.JWT.Issuer == "" {
		c.JWT.Issuer = c.AppID
	}
	if len(c.JWT.Audience) == 0 {
		c.JWT.Audience = []string{c.AppID}
	}
	if c.Password.MinLength == 0 {
		c.Password.MinLength = 12
	}
	if c.Password.MaxLength == 0 {
		c.Password.MaxLength = 128
	}
	if c.Password.Argon2Time == 0 {
		c.Password.Argon2Time = 3
	}
	if c.Password.Argon2Memory == 0 {
		c.Password.Argon2Memory = 64 * 1024
	}
	if c.Password.Argon2Threads == 0 {
		c.Password.Argon2Threads = 2
	}
	if c.AccessTokenTTL == 0 {
		c.AccessTokenTTL = 15 * time.Minute
	}
	if c.RefreshTokenTTL == 0 {
		c.RefreshTokenTTL = 30 * 24 * time.Hour
	}
	if c.PermissionCacheTTL == 0 {
		c.PermissionCacheTTL = 10 * time.Minute
	}
	if c.PrincipalCacheMaxTTL == 0 {
		c.PrincipalCacheMaxTTL = 5 * time.Minute
	}
	if c.DenylistCacheTTL == 0 {
		c.DenylistCacheTTL = 30 * time.Second
	}
	if c.StepUpWindow <= 0 {
		c.StepUpWindow = 5 * time.Minute
	}
	if c.Audit.AsyncBufferSize == 0 {
		c.Audit.AsyncBufferSize = 100
	}
	return c
}

// validate 检查配置中的必需字段和合理约束。
func (c Config) validate() error {
	if c.DB == nil {
		return errors.New("account db is nil")
	}
	if c.JWT.KeySet == nil && strings.TrimSpace(c.JWT.SecretKey) == "" {
		return errors.New("account jwt secret key is empty")
	}
	if c.JWT.KeySet == nil && strings.EqualFold(c.Mode, "production") && len(c.JWT.SecretKey) < 32 {
		return errors.New("account jwt secret key must be at least 32 bytes in production")
	}
	if c.Password.MinLength < 8 {
		return errors.New("account password min length must be at least 8")
	}
	if c.Password.MaxLength < c.Password.MinLength {
		return errors.New("account password max length must be greater than min length")
	}
	if c.AccessTokenTTL <= 0 || c.RefreshTokenTTL <= 0 {
		return errors.New("account token ttl must be positive")
	}
	return nil
}
