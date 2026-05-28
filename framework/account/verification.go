package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	VerificationChannelEmail = "email"
	VerificationChannelSMS   = "sms"

	VerificationPurposeRegister      = "register"
	VerificationPurposeLogin         = "login"
	VerificationPurposeResetPassword = "reset_password"
	VerificationPurposeBind          = "bind"
	VerificationPurposeMFA           = "mfa"
)

// NotifyProvider 通过邮件或短信发送验证码。
// SDK 提供此接口；生产环境必须配置真实的提供商。
type NotifyProvider interface {
	Send(ctx context.Context, channel string, target string, code string) error
}

// NotifyProviderFunc 将函数适配为 NotifyProvider 接口。
type NotifyProviderFunc func(ctx context.Context, channel, target, code string) error

func (f NotifyProviderFunc) Send(ctx context.Context, channel, target, code string) error {
	return f(ctx, channel, target, code)
}

type VerificationChallenge struct {
	ID          string     `gorm:"size:36;primaryKey"`
	TenantID    string     `gorm:"size:64;not null;index:idx_acct_vc_target,priority:1;index:idx_acct_vc_expires,priority:1"`
	TargetHash  string     `gorm:"size:64;not null;index:idx_acct_vc_target,priority:2"`
	Channel     string     `gorm:"size:16;not null"`
	Purpose     string     `gorm:"size:32;not null;index:idx_acct_vc_target,priority:3"`
	CodeHash    string     `gorm:"size:64;not null"`
	Attempts    int        `gorm:"not null;default:0"`
	MaxAttempts int        `gorm:"not null;default:5"`
	ExpiresAt   time.Time  `gorm:"not null;index:idx_acct_vc_expires,priority:2"`
	ConsumedAt  *time.Time `gorm:"default:null"`
	CreatedAt   time.Time  `gorm:"autoCreateTime"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime"`
}

func (VerificationChallenge) TableName() string { return "acct_verification_challenges" }

type VerificationService struct {
	m *Manager
}

// VerificationConfig 验证码行为的可配置参数。
type VerificationConfig struct {
	CodeLength          int
	CodeTTL             time.Duration
	MaxAttempts         int
	SendInterval        time.Duration
	MaxPerTargetPerHour int
	MaxPerIPPer10Min    int
}

func defaultVerificationConfig() VerificationConfig {
	return VerificationConfig{
		CodeLength:          6,
		CodeTTL:             5 * time.Minute,
		MaxAttempts:         5,
		SendInterval:        60 * time.Second,
		MaxPerTargetPerHour: 5,
		MaxPerIPPer10Min:    20,
	}
}

// SendVerificationRequest 发送验证码的请求参数。
type SendVerificationRequest struct {
	TenantID string
	Channel  string // email / sms
	Target   string // email address or phone number
	Purpose  string // register / login / reset_password / bind / mfa
	IP       string
}

// SendVerificationResult 发送验证码的结果，返回挑战 ID（不含验证码）。
type SendVerificationResult struct {
	ChallengeID string
	ExpiresAt   time.Time
}

// VerifyCodeRequest 验证码的验证请求参数。
type VerifyCodeRequest struct {
	TenantID    string
	ChallengeID string
	Code        string
	Purpose     string
	Channel     string
	Target      string
}

func (s *VerificationService) Send(ctx context.Context, req SendVerificationRequest) (*SendVerificationResult, error) {
	tenantID := s.m.tenantID(req.TenantID)
	vc := s.m.cfg.Verification
	targetHash := targetHash(req.Target)

	// Rate limit: same target + purpose, minimum send interval
	if err := s.checkSendRateLimit(ctx, tenantID, req.Channel, req.Target, req.Purpose, vc); err != nil {
		return nil, err
	}
	// Rate limit: per IP
	if req.IP != "" {
		if err := s.checkIPRateLimit(ctx, tenantID, req.IP, vc); err != nil {
			return nil, err
		}
	}

	code, err := generateCode(vc.CodeLength)
	if err != nil {
		return nil, fmt.Errorf("generate verification code: %w", err)
	}

	codeHash := codeHash(code)
	now := time.Now()
	challenge := &VerificationChallenge{
		ID:          newID(),
		TenantID:    tenantID,
		TargetHash:  targetHash,
		Channel:     req.Channel,
		Purpose:     req.Purpose,
		CodeHash:    codeHash,
		Attempts:    0,
		MaxAttempts: vc.MaxAttempts,
		ExpiresAt:   now.Add(vc.CodeTTL),
	}
	if err := s.m.db.WithContext(ctx).Create(challenge).Error; err != nil {
		return nil, fmt.Errorf("create verification challenge: %w", err)
	}

	if s.m.cfg.NotifyProvider == nil {
		if strings.EqualFold(s.m.cfg.Mode, "production") {
			return nil, accountError(ErrInternal, "未配置验证码通知提供商")
		}
		gaia.WarnF("[account] no NotifyProvider configured, verification challenge created: channel=%s challenge=%s",
			req.Channel, challenge.ID)
	} else if err := s.m.cfg.NotifyProvider.Send(ctx, req.Channel, req.Target, code); err != nil {
		gaia.WarnF("[account] NotifyProvider.Send failed: channel=%s target=%s err=%v", req.Channel, req.Target, err)
		return nil, accountError(ErrInternal, "验证码发送失败")
	}

	if m := s.m.metrics; m != nil {
		m.VerificationSendTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("channel", req.Channel),
			attribute.String("purpose", req.Purpose),
			attribute.String("status", "success"),
		))
	}
	return &SendVerificationResult{ChallengeID: challenge.ID, ExpiresAt: challenge.ExpiresAt}, nil
}

func (s *VerificationService) Verify(ctx context.Context, req VerifyCodeRequest) error {
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.verifyTx(ctx, tx, req)
	})
}

func (s *VerificationService) verifyTx(ctx context.Context, tx *gorm.DB, req VerifyCodeRequest) error {
	tenantID := s.m.tenantID(req.TenantID)

	var challenge VerificationChallenge
	query := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ? AND purpose = ?", req.ChallengeID, tenantID, req.Purpose)
	if req.Channel != "" {
		query = query.Where("channel = ?", req.Channel)
	}
	if req.Target != "" {
		query = query.Where("target_hash = ?", targetHash(req.Target))
	}
	if err := query.First(&challenge).Error; err != nil {
		return accountError(ErrInvalidArgument, "验证码挑战不存在")
	}
	if challenge.ConsumedAt != nil {
		return accountError(ErrInvalidArgument, "验证码已使用")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return accountError(ErrInvalidArgument, "验证码已过期")
	}
	if challenge.Attempts >= challenge.MaxAttempts {
		return accountError(ErrRateLimited, "验证码尝试次数过多")
	}

	computed := codeHash(req.Code)
	if computed != challenge.CodeHash {
		_ = tx.WithContext(ctx).Model(&challenge).Update("attempts", gorm.Expr("attempts + 1")).Error
		return accountError(ErrInvalidArgument, "验证码错误")
	}

	now := time.Now()
	if err := tx.WithContext(ctx).Model(&challenge).Updates(map[string]any{
		"attempts":    gorm.Expr("attempts + 1"),
		"consumed_at": &now,
	}).Error; err != nil {
		return err
	}
	if m := s.m.metrics; m != nil {
		m.VerificationVerifyTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("channel", challenge.Channel),
			attribute.String("purpose", challenge.Purpose),
			attribute.String("status", "success"),
		))
	}
	return nil
}
func (s *VerificationService) checkSendRateLimit(ctx context.Context, tenantID, channel, target, purpose string, cfg VerificationConfig) error {
	if s.m.cache == nil {
		return nil
	}
	key := fmt.Sprintf("rate:v:%s:%s:%s:%s", tenantID, channel, targetHash(target), purpose)
	last, ok, err := s.m.cache.Get(ctx, key)
	if err != nil {
		gaia.WarnF("[account] rate limit cache read error: %v", err)
		return nil // degrade open on cache error
	}
	if ok {
		return accountError(ErrRateLimited, fmt.Sprintf("验证码发送过于频繁，请 %s 后再试", last))
	}
	// Record send with TTL = send interval
	if err := s.m.cache.Set(ctx, key, time.Now().Add(cfg.SendInterval).Format("15:04:05"), cfg.SendInterval); err != nil {
		gaia.WarnF("[account] rate limit cache write error: %v", err)
	}

	// Per-target per-hour limit
	hourKey := fmt.Sprintf("rate:vh:%s:%s:%s", tenantID, targetHash(target), purpose)
	count, _, err := s.m.cache.Get(ctx, hourKey)
	if err == nil && count != "" {
		// Simple counter approach - we'll check via DB
	}
	// Use DB for hourly count as it's more reliable
	var hourlyCount int64
	_ = s.m.db.WithContext(ctx).Model(&VerificationChallenge{}).
		Where("tenant_id = ? AND target_hash = ? AND purpose = ? AND created_at > ?",
			tenantID, targetHash(target), purpose, time.Now().Add(-1*time.Hour)).
		Count(&hourlyCount).Error
	if hourlyCount >= int64(cfg.MaxPerTargetPerHour) {
		return accountError(ErrRateLimited, "该账号验证码请求过于频繁，请稍后再试")
	}
	return nil
}

func (s *VerificationService) checkIPRateLimit(ctx context.Context, tenantID, ip string, cfg VerificationConfig) error {
	if s.m.cache == nil || ip == "" {
		return nil
	}
	key := fmt.Sprintf("rate:vip:%s:%s", tenantID, ip)
	countStr, ok, err := s.m.cache.Get(ctx, key)
	if err != nil {
		return nil
	}
	var count int
	if ok {
		fmt.Sscanf(countStr, "%d", &count)
	}
	count++
	if count > cfg.MaxPerIPPer10Min {
		return accountError(ErrRateLimited, "请求过于频繁，请稍后再试")
	}
	// Use 10-minute TTL; if key doesn't exist yet, set it fresh
	if !ok {
		_ = s.m.cache.Set(ctx, key, "1", 10*time.Minute)
	} else {
		_ = s.m.cache.Set(ctx, key, fmt.Sprintf("%d", count), 10*time.Minute)
	}
	return nil
}

// CleanupExpired 删除过期的验证挑战。
func (s *VerificationService) CleanupExpired(ctx context.Context) error {
	return s.m.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&VerificationChallenge{}).Error
}

// generateCode returns a numeric code of the given length.
func generateCode(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		code[i] = byte('0' + n.Int64())
	}
	return string(code), nil
}

func codeHash(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func targetHash(target string) string {
	sum := sha256.Sum256([]byte(target))
	return hex.EncodeToString(sum[:])
}
