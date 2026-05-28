package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CredentialTOTP         = "totp"
	CredentialRecoveryCode = "recovery_code"

	MFAActionSetup   = "mfa_setup"
	MFAActionVerify  = "mfa_verify"
	MFAActionDisable = "mfa_disable"

	// totpPeriodSeconds TOTP 时间窗口长度（秒），与 SetupTOTP 生成密钥时使用的默认值一致。
	totpPeriodSeconds = 30
	// totpSkewWindows TOTP 允许的前后时钟漂移窗口数（与 pquerna/otp 默认 Skew=1 一致）。
	totpSkewWindows = 1
)

// MFAChallenge 跟踪登录过程中待处理的 MFA 挑战。
type MFAChallenge struct {
	ID                       string    `gorm:"size:36;primaryKey"`
	TenantID                 string    `gorm:"size:64;not null;index:idx_acct_mfa_challenge"`
	UserID                   string    `gorm:"size:36;not null;index:idx_acct_mfa_challenge"`
	AuthVersion              int64     `gorm:"not null"`
	Attempts                 int       `gorm:"not null;default:0"`
	MaxAttempts              int       `gorm:"not null;default:5"`
	ExpiresAt                time.Time `gorm:"not null;index:idx_acct_mfa_expires"`
	ConsumedAt               *time.Time
	Method                   string    `gorm:"size:16;not null;default:'totp'"`
	VerificationChallengeID  string    `gorm:"size:36;not null;default:''"`
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

func (MFAChallenge) TableName() string { return "acct_mfa_challenges" }

type MFAService struct {
	m *Manager
}

// MFASetupResult 包含初始设置时的 TOTP 密钥和恢复码。
type MFASetupResult struct {
	Secret        string   `json:"secret"`
	URI           string   `json:"uri"`
	RecoveryCodes []string `json:"recovery_codes"`
}

// TOTPVerifyRequest 验证 TOTP 码的请求参数。
type TOTPVerifyRequest struct {
	TenantID string
	UserID   string
	Code     string
}

// SetupTOTP 为用户生成新的 TOTP 密钥和恢复码。
func (s *MFAService) SetupTOTP(ctx context.Context, tenantID, userID, email string) (*MFASetupResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.mfa.setup_totp")
	defer span.End()
	// Generate TOTP secret
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.m.cfg.JWT.Issuer,
		AccountName: email,
	})
	if err != nil {
		return nil, fmt.Errorf("generate totp key: %w", err)
	}

	// Generate recovery codes
	recoveryCodes := make([]string, 8)
	recoveryHashes := make([]string, 8)
	for i := range recoveryCodes {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		recoveryCodes[i] = code
		recoveryHashes[i] = tokenHash(code)
	}

	err = s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Deactivate existing TOTP credential if any
		_ = tx.Model(&Credential{}).Where("user_id = ? AND tenant_id = ? AND type = ?",
			userID, tenantID, CredentialTOTP).Update("enabled", false).Error

		// Create TOTP credential
		totpCred := Credential{
			ID:         newID(),
			TenantID:   tenantID,
			UserID:     userID,
			Type:       CredentialTOTP,
			Identifier: "totp:main",
			SecretHash: key.Secret(),
			SecretMeta: "{}",
			Enabled:    true,
		}
		if err := tx.Create(&totpCred).Error; err != nil {
			return fmt.Errorf("create totp credential: %w", err)
		}

		// Deactivate existing recovery codes
		_ = tx.Model(&Credential{}).Where("user_id = ? AND tenant_id = ? AND type = ?",
			userID, tenantID, CredentialRecoveryCode).Update("enabled", false).Error

		// Create recovery code credentials
		for i, hash := range recoveryHashes {
			rc := Credential{
				ID:         newID(),
				TenantID:   tenantID,
				UserID:     userID,
				Type:       CredentialRecoveryCode,
				Identifier: fmt.Sprintf("recovery:%d", i+1),
				SecretHash: hash,
								SecretMeta: "{}",
Enabled:    true,
			}
			if err := tx.Create(&rc).Error; err != nil {
				return fmt.Errorf("create recovery code credential: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	_ = emitOutbox(s.m.db.WithContext(ctx), EventMFASetup, userID, map[string]any{
		"user_id": userID,
		"tenant_id": tenantID,
		"mfa_type": "totp",
		"setup_at": time.Now(),
	})
	s.m.audit(ctx, tenantID, userID, "mfa", "setup", "", "", "")
	return &MFASetupResult{
		Secret:        key.Secret(),
		URI:           key.URL(),
		RecoveryCodes: recoveryCodes,
	}, nil
}

// VerifyTOTP 验证用户的 TOTP 码或恢复码。
func (s *MFAService) VerifyTOTP(ctx context.Context, req TOTPVerifyRequest) (bool, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.mfa.verify_totp")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)
	var ok bool
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		ok, err = s.verifyTOTPTx(ctx, tx, tenantID, req.UserID, req.Code)
		return err
	})
	return ok, err
}

func (s *MFAService) verifyTOTPTx(ctx context.Context, tx *gorm.DB, tenantID, userID, code string) (bool, error) {
	// First try as TOTP.
	// 使用 FOR UPDATE 行锁，防止并发请求同时通过 last_used_counter 校验导致重放。
	var cred Credential
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND tenant_id = ? AND type = ? AND enabled = ?",
			userID, tenantID, CredentialTOTP, true).First(&cred).Error; err == nil {

		// 在 [-skew, +skew] 个窗口内逐个尝试匹配，从而拿到具体命中的窗口序号。
		// 等价于 pquerna/otp 默认 Skew=1 的行为，但额外返回 counter 用于防重放。
		now := time.Now()
		opts := totp.ValidateOpts{
			Period:    totpPeriodSeconds,
			Skew:      0, // 我们手动控制 skew，避免库内部一次性扫所有窗口
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		}
		for delta := -int64(totpSkewWindows); delta <= int64(totpSkewWindows); delta++ {
			t := now.Add(time.Duration(delta*totpPeriodSeconds) * time.Second)
			ok, err := totp.ValidateCustom(code, cred.SecretHash, t, opts)
			if err != nil || !ok {
				continue
			}
			counter := t.Unix() / int64(totpPeriodSeconds)

			// RFC 6238 §5.2：同一个 code（即同一时间窗）或更早窗口的 code 必须拒绝。
			if counter <= cred.LastUsedCounter {
				if m := s.m.metrics; m != nil && m.TOTPReplay != nil {
					m.TOTPReplay.Add(ctx, 1, metric.WithAttributes(
						attribute.String("type", "totp"),
					))
				}
				s.m.audit(ctx, tenantID, userID, "mfa", "replay", "totp code replay rejected", "", "")
				return false, nil
			}

			// 通过校验：原子推进 last_used_counter，并刷新 last_used_at。
			if err := tx.WithContext(ctx).Model(&Credential{}).
				Where("id = ?", cred.ID).
				Updates(map[string]any{
					"last_used_counter": counter,
					"last_used_at":      now,
				}).Error; err != nil {
				return false, err
			}
			return true, nil
		}
		// TOTP 算法校验未通过，继续尝试 recovery code 路径。
	}

	// Try as recovery code.
	codeHash := tokenHash(code)
	var rc Credential
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ? AND tenant_id = ? AND type = ? AND enabled = ? AND secret_hash = ?",
			userID, tenantID, CredentialRecoveryCode, true, codeHash).
		First(&rc).Error; err == nil {
		_ = tx.WithContext(ctx).Model(&rc).Updates(map[string]any{"enabled": false, "last_used_at": time.Now()})
		return true, nil
	}

	return false, nil
}

// HasTOTP 检查用户是否已启用 TOTP。
func (s *MFAService) HasTOTP(ctx context.Context, tenantID, userID string) bool {
	var count int64
	s.m.db.WithContext(ctx).Model(&Credential{}).
		Where("user_id = ? AND tenant_id = ? AND type = ? AND enabled = ?",
			userID, tenantID, CredentialTOTP, true).
		Count(&count)
	return count > 0
}

// DisableTOTP 移除用户的所有 MFA 凭证。
// 如果提供了 principal，将强制检查 step-up MFA。
func (s *MFAService) DisableTOTP(ctx context.Context, tenantID, userID string, principal *Principal) error {
	ctx, span := s.m.tracer.Start(ctx, "account.mfa.disable_totp")
	defer span.End()
	// Enforce step-up MFA if principal is provided
	if principal != nil {
		if err := s.m.auth.RequireStepUp(ctx, principal); err != nil {
			return err
		}
	}
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_ = tx.Model(&Credential{}).Where("user_id = ? AND tenant_id = ? AND type IN ?",
			userID, tenantID, []string{CredentialTOTP, CredentialRecoveryCode}).
			Update("enabled", false).Error
		return nil
	})
	if err == nil {
		s.m.audit(ctx, tenantID, userID, "mfa", "disable", "", "", "")
		_ = emitOutbox(s.m.db.WithContext(ctx), EventMFADisabled, userID, map[string]any{
			"user_id": userID,
			"tenant_id": tenantID,
			"disabled_at": time.Now(),
		})
	}
	return err
}

// CreateMFAChallenge 为正在进行的登录流程创建挑战。
// method 默认为 "totp"，可传 "email" 或 "sms" 创建对应类型的挑战。
func (s *MFAService) CreateMFAChallenge(ctx context.Context, tenantID, userID string, authVersion int64, method ...string) (*MFAChallenge, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.mfa.create_challenge")
	defer span.End()
	m := "totp"
	if len(method) > 0 && method[0] != "" {
		m = method[0]
	}
	challenge := &MFAChallenge{
		ID:          newID(),
		TenantID:    tenantID,
		UserID:      userID,
		AuthVersion: authVersion,
		MaxAttempts: 5,
		ExpiresAt:   time.Now().Add(5 * time.Minute),
		Method:      m,
	}
	if err := s.m.db.WithContext(ctx).Create(challenge).Error; err != nil {
		return nil, fmt.Errorf("create MFA challenge: %w", err)
	}
	return challenge, nil
}

// RequestMFACode 通过邮箱或短信发送 MFA 验证码，并创建对应的 MFA 挑战。
// 适用于用户主动请求 email/sms 二次验证（如无 TOTP 时的降级方案）。
func (s *MFAService) RequestMFACode(ctx context.Context, tenantID, userID, channel, ip string) (*MFAChallenge, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.mfa.request_code")
	defer span.End()

	var user User
	if err := s.m.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", userID, s.m.tenantID(tenantID)).First(&user).Error; err != nil {
		return nil, accountError(ErrInvalidArgument, "用户不存在")
	}

	var target string
	switch channel {
	case VerificationChannelEmail:
		if user.Email == nil || *user.Email == "" {
			return nil, accountError(ErrInvalidArgument, "用户未绑定邮箱")
		}
		target = *user.Email
	case VerificationChannelSMS:
		if user.Phone == nil || *user.Phone == "" {
			return nil, accountError(ErrInvalidArgument, "用户未绑定手机号")
		}
		target = *user.Phone
	default:
		return nil, accountError(ErrInvalidArgument, "不支持的验证通道")
	}

	vcResult, err := s.m.verification.Send(ctx, SendVerificationRequest{
		TenantID: tenantID,
		Channel:  channel,
		Target:   target,
		Purpose:  VerificationPurposeMFA,
		IP:       ip,
	})
	if err != nil {
		return nil, err
	}

	challenge, err := s.CreateMFAChallenge(ctx, s.m.tenantID(tenantID), userID, user.AuthVersion, channel)
	if err != nil {
		return nil, err
	}

	_ = s.m.db.WithContext(ctx).Model(&MFAChallenge{}).Where("id = ?", challenge.ID).Updates(map[string]any{
		"verification_challenge_id": vcResult.ChallengeID,
	}).Error
	challenge.VerificationChallengeID = vcResult.ChallengeID

	return challenge, nil
}

// ValidateMFAChallenge 验证 MFA 挑战的有效性（存在、未使用、未过期、未超限）。
// 注意：该方法不标记挑战为已消费 — 消费在 TOTP 验证成功后通过 ConsumeMFAChallenge 完成。
func (s *MFAService) ValidateMFAChallenge(ctx context.Context, challengeID string) (string, string, int64, error) {
	var challenge MFAChallenge
	if err := s.m.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", challengeID).First(&challenge).Error; err != nil {
		return "", "", 0, accountError(ErrInvalidArgument, "MFA 挑战无效")
	}
	if challenge.ConsumedAt != nil {
		return "", "", 0, accountError(ErrInvalidArgument, "MFA 挑战已使用")
	}
	if time.Now().After(challenge.ExpiresAt) {
		return "", "", 0, accountError(ErrInvalidArgument, "MFA 挑战已过期")
	}
	if challenge.Attempts >= challenge.MaxAttempts {
		return "", "", 0, accountError(ErrInvalidArgument, "MFA 挑战尝试次数过多")
	}

	return challenge.TenantID, challenge.UserID, challenge.AuthVersion, nil
}

// ConsumeMFAChallenge 将 MFA 挑战标记为已消费并递增尝试次数。
// 仅在 TOTP 验证成功后调用。
func (s *MFAService) ConsumeMFAChallenge(ctx context.Context, challengeID string) {
	now := time.Now()
	_ = s.m.db.WithContext(ctx).Model(&MFAChallenge{}).Where("id = ?", challengeID).Updates(map[string]any{
		"consumed_at": &now,
		"attempts":    gorm.Expr("attempts + 1"),
	}).Error
}

// RecordMFAFailure 递增尝试计数器而不消耗挑战。
func (s *MFAService) RecordMFAFailure(ctx context.Context, challengeID string) {
	_ = s.m.db.WithContext(ctx).Model(&MFAChallenge{}).Where("id = ?", challengeID).
		Update("attempts", gorm.Expr("attempts + 1")).Error
}

// CompleteMFA 验证有 MFA 挑战的登录的 TOTP 码并颁发令牌。
func (s *AuthService) CompleteMFA(ctx context.Context, challengeID, code string, req CompleteMFARequest) (*AuthResult, error) {
	var result *AuthResult
	var tenantID, userID string
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var challenge MFAChallenge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", challengeID).First(&challenge).Error; err != nil {
			return accountError(ErrInvalidArgument, "MFA 挑战无效")
		}
		tenantID = challenge.TenantID
		userID = challenge.UserID
		if challenge.ConsumedAt != nil {
			return accountError(ErrInvalidArgument, "MFA 挑战已使用")
		}
		if time.Now().After(challenge.ExpiresAt) {
			return accountError(ErrInvalidArgument, "MFA 挑战已过期")
		}
		if challenge.Attempts >= challenge.MaxAttempts {
			return accountError(ErrInvalidArgument, "MFA 挑战尝试次数过多")
		}

		// Verify based on MFA method (TOTP or email/SMS verification code)
		if challenge.Method == "email" || challenge.Method == "sms" {
			var u User
			if err := tx.Where("id = ? AND tenant_id = ?", challenge.UserID, challenge.TenantID).First(&u).Error; err != nil {
				return accountError(ErrInvalidToken, "用户不可用")
			}
			var channel, target string
			if challenge.Method == "email" {
				channel = VerificationChannelEmail
				target = *u.Email
			} else {
				channel = VerificationChannelSMS
				target = *u.Phone
			}
			if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
				ChallengeID: challenge.VerificationChallengeID,
				Code:        code,
				Purpose:     VerificationPurposeMFA,
				Channel:     channel,
				Target:      target,
			}); err != nil {
				_ = tx.Model(&challenge).Update("attempts", gorm.Expr("attempts + 1")).Error
				return accountError(ErrInvalidCredential, "验证码错误")
			}
		} else {
			ok, err := s.m.mfa.verifyTOTPTx(ctx, tx, challenge.TenantID, challenge.UserID, code)
			if err != nil {
				return accountError(ErrInternal, "MFA 验证失败")
			}
			if !ok {
				_ = tx.Model(&challenge).Update("attempts", gorm.Expr("attempts + 1")).Error
				return accountError(ErrInvalidCredential, "MFA 验证码错误")
			}
		}

		var user User
		if err := tx.Where("id = ? AND tenant_id = ? AND status = ?", challenge.UserID, challenge.TenantID, UserStatusNormal).First(&user).Error; err != nil {
			return accountError(ErrInvalidToken, "用户不可用")
		}
		if user.AuthVersion != challenge.AuthVersion {
			return accountError(ErrInvalidToken, "用户认证版本已变更")
		}
		if s.m.phoneBindingRequired(&user) {
			return accountError(ErrPhoneBindingRequired, "需要先绑定并验证手机号")
		}

		roles, err := s.loadRoleCodes(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		result, err = s.issueTokens(ctx, tx, &user, roles, req.DeviceID, req.IP, req.UserAgent, "", "", "")
		if err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&challenge).Updates(map[string]any{
			"consumed_at": &now,
			"attempts":    gorm.Expr("attempts + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"last_login_at": now,
			"last_login_ip": req.IP,
		}).Error
	})
	if err != nil {
		if tenantID != "" || userID != "" {
			s.m.audit(ctx, tenantID, userID, "mfa", "failed", err.Error(), req.IP, req.UserAgent)
		}
		return nil, err
	}

	mfaStart := time.Now()
	s.m.audit(ctx, tenantID, userID, "mfa", "success", "", req.IP, req.UserAgent)
	s.m.audit(ctx, tenantID, userID, "login", "success", "with mfa", req.IP, req.UserAgent)
	_ = emitOutbox(s.m.db.WithContext(ctx), EventUserLoggedIn, userID, map[string]any{
		"user_id": userID,
		"tenant_id": tenantID,
		"method": "mfa",
		"logged_in_at": time.Now(),
	})
	if m := s.m.metrics; m != nil {
		m.LoginTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
			attribute.String("reason", "mfa_complete"),
		))
		m.LoginDuration.Record(ctx, float64(time.Since(mfaStart).Microseconds())/1000.0,
			metric.WithAttributes(
				attribute.String("mfa", "true"),
			))
	}
	return result, nil
}

// CompleteMFARequest 最终令牌颁发的设备信息请求参数。
type CompleteMFARequest struct {
	DeviceID  string
	IP        string
	UserAgent string
}

// generateRecoveryCode returns a cryptographically random recovery code.
func generateRecoveryCode() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// Format as XXXXX-XXXXX
	encoded := base64.RawURLEncoding.EncodeToString(buf)
	if len(encoded) > 10 {
		return encoded[:5] + "-" + encoded[5:10], nil
	}
	return encoded, nil
}
