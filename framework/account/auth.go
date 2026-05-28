package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/xxzhwl/gaia"
)

// AuthService 提供用户注册、登录、令牌刷新、登出和令牌验证等认证功能。
type AuthService struct {
	m *Manager
}

// RegisterRequest 用户注册请求参数。
type RegisterRequest struct {
	TenantID                     string
	Username                     string
	Password                     string
	Email                        string
	EmailVerificationChallengeID string
	EmailVerificationCode        string
	Phone                        string
	PhoneVerificationChallengeID string
	PhoneVerificationCode        string
	Nickname                     string
	DeviceID                     string
	IP                           string
	UserAgent                    string
}

// LoginRequest 用户登录请求参数。
type LoginRequest struct {
	TenantID       string
	Identifier     string
	IdentifierType string
	Password       string
	DeviceID       string
	IP             string
	UserAgent      string
}

// RefreshRequest 令牌刷新请求参数。
type RefreshRequest struct {
	RefreshToken string
	DeviceID     string
	IP           string
	UserAgent    string
}

// LogoutRequest 登出请求参数。
type LogoutRequest struct {
	AccessToken  string
	RefreshToken string
	SessionID    string
}

// AuthResult 认证结果，包含用户信息、令牌和可选的 MFA 挑战信息。
type AuthResult struct {
	User         *UserInfo `json:"user"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenType    string    `json:"token_type"`

	// MFA challenge fields (non-empty when MFA is required before issuing tokens)
	MFARequired  bool   `json:"mfa_required"`
	MFAChallenge string `json:"mfa_challenge,omitempty"`

	// PhoneBindingRequired is true when policy requires a verified phone before token issuance.
	PhoneBindingRequired bool `json:"phone_binding_required"`
}

// Principal 表示经过身份验证的用户主体，包含用户、租户、角色和会话信息。
type Principal struct {
	UserID        string   `json:"user_id"`
	TenantID      string   `json:"tenant_id"`
	Username      string   `json:"username"`
	SessionID     string   `json:"sid"`
	APITokenID    string   `json:"api_token_id,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	AuthVersion   int64    `json:"auth_version"`
	RolesVersion  int64    `json:"roles_version"`
	PhoneVerified bool     `json:"phone_verified"`
	Roles         []string `json:"roles"`
}

type accessClaims struct {
	UserID        string   `json:"user_id"`
	TenantID      string   `json:"tenant_id"`
	Username      string   `json:"username"`
	SessionID     string   `json:"sid"`
	AuthVersion   int64    `json:"auth_version"`
	RolesVersion  int64    `json:"roles_version"`
	PhoneVerified bool     `json:"phone_verified"`
	Roles         []string `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

// BindPhoneRequest 通过已验证的短信验证码为账号绑定手机号，并在成功后签发令牌。
type BindPhoneRequest struct {
	TenantID       string
	Identifier     string
	IdentifierType string
	Password       string
	Phone          string
	ChallengeID    string
	Code           string
	DeviceID       string
	IP             string
	UserAgent      string
}

// Register 使用给定的凭证创建新用户账户并颁发令牌。
func (s *AuthService) Register(ctx context.Context, req RegisterRequest) (*AuthResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.register")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)
	req.Username = strings.TrimSpace(req.Username)
	req.Email = normalizeEmail(req.Email)
	req.Phone = normalizePhone(req.Phone)
	if err := validateIdentifier(req); err != nil {
		return nil, accountError(ErrInvalidArgument, err.Error())
	}
	if err := validatePassword(req.Password, s.m.cfg.Password); err != nil {
		return nil, err
	}

	var result *AuthResult
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
		var phoneVerifiedAt *time.Time
		if req.Phone != "" && req.PhoneVerificationChallengeID != "" && req.PhoneVerificationCode != "" {
			if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
				TenantID:    tenantID,
				ChallengeID: req.PhoneVerificationChallengeID,
				Code:        req.PhoneVerificationCode,
				Purpose:     VerificationPurposeBind,
				Channel:     VerificationChannelSMS,
				Target:      req.Phone,
			}); err != nil {
				return err
			}
			now := time.Now()
			phoneVerifiedAt = &now
		}
		var emailVerifiedAt *time.Time
		if req.Email != "" && req.EmailVerificationChallengeID != "" && req.EmailVerificationCode != "" {
			if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
				TenantID:    tenantID,
				ChallengeID: req.EmailVerificationChallengeID,
				Code:        req.EmailVerificationCode,
				Purpose:     VerificationPurposeRegister,
				Channel:     VerificationChannelEmail,
				Target:      req.Email,
			}); err != nil {
				return err
			}
			now := time.Now()
			emailVerifiedAt = &now
		}
		status := UserStatusNormal
		if s.m.cfg.AccountPolicy.RequireVerifiedPhone && phoneVerifiedAt == nil {
			status = UserStatusPending
		}
		if s.m.cfg.AccountPolicy.RequireVerifiedEmail && emailVerifiedAt == nil && req.Email != "" {
			status = UserStatusPending
		}
		user := User{
			ID:              newID(),
			TenantID:        tenantID,
			Username:        req.Username,
			Email:           nullableString(req.Email),
			Phone:           nullableString(req.Phone),
			EmailVerifiedAt: emailVerifiedAt,
			PhoneVerifiedAt: phoneVerifiedAt,
			Nickname:        req.Nickname,
			Status:          status,
			AuthVersion:     1,
			RolesVersion:    1,
			ProfileVersion:  1,
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
		if err := s.assignDefaultRole(ctx, tx, &user); err != nil {
			return err
		}
		if err := emitOutbox(tx, EventUserRegistered, user.ID, map[string]any{
			"user_id":       user.ID,
			"tenant_id":     tenantID,
			"username":      user.Username,
			"email":         user.Email,
			"phone":         user.Phone,
			"registered_at": time.Now(),
		}); err != nil {
			gaia.WarnF("[account] emit user registered event failed: %v", err)
		}
		if s.m.phoneBindingRequired(&user) {
			result = &AuthResult{
				User:                 user.toInfo(nil, nil),
				PhoneBindingRequired: true,
				TokenType:            "Bearer",
			}
			return nil
		}
		roles, err := s.loadRoleCodes(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		authResult, err := s.issueTokens(ctx, tx, &user, roles, req.DeviceID, req.IP, req.UserAgent, "", "", "")
		if err != nil {
			return err
		}
		result = authResult
		return nil
	})
	if err != nil {
		s.m.audit(ctx, tenantID, "", "register", "failed", err.Error(), req.IP, req.UserAgent)
		if m := s.m.metrics; m != nil {
			m.RegisterTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
			))
		}
		return nil, err
	}
	s.m.audit(ctx, tenantID, result.User.ID, "register", "success", "", req.IP, req.UserAgent)
	if m := s.m.metrics; m != nil {
		m.RegisterTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
		))
	}
	return result, nil
}

// Login 通过标识符和密码进行用户认证，执行风险评估，可选要求 MFA，成功后颁发访问和刷新令牌。
func (s *AuthService) Login(ctx context.Context, req LoginRequest) (*AuthResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.login")
	defer span.End()
	loginStart := time.Now()
	tenantID := s.m.tenantID(req.TenantID)
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" || req.Password == "" {
		return nil, accountError(ErrInvalidArgument, "登录标识和密码不能为空")
	}

	var user User
	var result *AuthResult
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("tenant_id = ?", tenantID)
		switch req.IdentifierType {
		case "email":
			query = query.Where("email = ?", normalizeEmail(identifier))
		case "phone":
			query = query.Where("phone = ?", normalizePhone(identifier))
		case "username":
			query = query.Where("username = ?", identifier)
		default:
			if isEmailIdentifier(identifier) {
				query = query.Where("email = ?", normalizeEmail(identifier))
			} else {
				query = query.Where("username = ? OR phone = ?", identifier, normalizePhone(identifier))
			}
		}
		if err := query.First(&user).Error; err != nil {
			return accountError(ErrInvalidCredential, "账号或密码错误")
		}
		if user.Status == UserStatusDisabled || user.Status == UserStatusDeleted {
			return accountError(ErrPermissionDenied, "账号不可用")
		}
		if user.Status == UserStatusLocked {
			if user.LockedUntil != nil && time.Now().After(*user.LockedUntil) {
				if err := tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
					"status":       UserStatusNormal,
					"locked_until": nil,
				}).Error; err != nil {
					return err
				}
				user.Status = UserStatusNormal
				user.LockedUntil = nil
			} else {
				return accountError(ErrAccountLocked, "账号已锁定")
			}
		}

		// Risk assessment before credential check
		riskResult, err := s.m.risk.Assess(ctx, tenantID, user.ID, req.IP)
		if err != nil {
			return err
		}
		switch riskResult.Decision {
		case RiskBlock, RiskLock:
			if m := s.m.metrics; m != nil {
				m.RiskBlocked.Add(ctx, 1, metric.WithAttributes(
					attribute.String("reason", riskResult.Reason),
				))
			}
			return accountError(ErrRateLimited, riskResult.Reason)
		case RiskDelay:
			return accountError(ErrRateLimited, riskResult.Reason)
		}

		var cred Credential
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND user_id = ? AND type = ? AND enabled = ?", tenantID, user.ID, CredentialPassword, true).
			First(&cred).Error; err != nil {
			return accountError(ErrInvalidCredential, "账号或密码错误")
		}
		if !verifyPassword(req.Password, cred.SecretHash) {
			return accountError(ErrInvalidCredential, "账号或密码错误")
		}
		// Auto-upgrade password hash if argon2 parameters have changed
		if needsPasswordUpgrade(cred.SecretHash, s.m.cfg.Password) {
			newHash, hashErr := hashPassword(req.Password, s.m.cfg.Password)
			if hashErr == nil {
				_ = tx.Model(&cred).Update("secret_hash", newHash)
			}
		}
		if s.m.phoneBindingRequired(&user) {
			result = &AuthResult{
				User:                 user.toInfo(nil, nil),
				PhoneBindingRequired: true,
				TokenType:            "Bearer",
			}
			return nil
		}

		// Check if MFA is required
		if s.m.mfa.HasTOTP(ctx, tenantID, user.ID) {
			challenge, err := s.m.mfa.CreateMFAChallenge(ctx, tenantID, user.ID, user.AuthVersion)
			if err != nil {
				return err
			}
			result = &AuthResult{
				MFARequired:  true,
				MFAChallenge: challenge.ID,
				User:         user.toInfo(nil, nil),
			}
			return nil
		}
		// Risk-triggered email/SMS MFA when no TOTP is configured
		if riskResult.Decision == RiskChallenge && (user.EmailVerifiedAt != nil || user.PhoneVerifiedAt != nil) {
			var channel, target string
			if user.EmailVerifiedAt != nil {
				channel = VerificationChannelEmail
				target = *user.Email
			} else {
				channel = VerificationChannelSMS
				target = *user.Phone
			}
			vcResult, err := s.m.verification.Send(ctx, SendVerificationRequest{
				TenantID: tenantID,
				Channel:  channel,
				Target:   target,
				Purpose:  VerificationPurposeMFA,
				IP:       req.IP,
			})
			if err != nil {
				return err
			}
			challenge, err := s.m.mfa.CreateMFAChallenge(ctx, tenantID, user.ID, user.AuthVersion)
			if err != nil {
				return err
			}
			// Store verification challenge reference
			_ = tx.Model(&MFAChallenge{}).Where("id = ?", challenge.ID).Updates(map[string]any{
				"method":                    channel,
				"verification_challenge_id": vcResult.ChallengeID,
			}).Error
			result = &AuthResult{
				MFARequired:  true,
				MFAChallenge: challenge.ID,
				User:         user.toInfo(nil, nil),
			}
			return nil
		}

		// If risk was challenge but user has no MFA channel available, reject
		if riskResult.Decision == RiskChallenge {
			return accountError(ErrRateLimited, "需要额外验证才能登录")
		}

		roles, err := s.loadRoleCodes(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		// Enforce admin MFA if configured
		if s.m.cfg.AccountPolicy.RequireAdminMFA && userHasAdminRole(roles) && !s.m.mfa.HasTOTP(ctx, tenantID, user.ID) {
			return accountError(ErrPermissionDenied, "需要先配置 MFA 后才能登录")
		}

		result, err = s.issueTokens(ctx, tx, &user, roles, req.DeviceID, req.IP, req.UserAgent, "", "", "")
		if err != nil {
			return err
		}
		now := time.Now()
		if err := emitOutbox(tx, EventUserLoggedIn, user.ID, map[string]any{
			"user_id":      user.ID,
			"tenant_id":    tenantID,
			"method":       "password",
			"logged_in_at": now,
		}); err != nil {
			gaia.WarnF("[account] emit user logged in event failed: %v", err)
		}
		return tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"last_login_at": now,
			"last_login_ip": req.IP,
		}).Error
	})
	if err != nil {
		reason := "invalid credential"
		if user.ID != "" {
			// Record failure for risk tracking
			s.m.risk.RecordFailure(ctx, tenantID, user.ID, req.IP)
			reason = err.Error()
		}
		s.m.audit(ctx, tenantID, user.ID, "login", "failed", reason, req.IP, req.UserAgent)
		if m := s.m.metrics; m != nil {
			m.LoginTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("reason", truncateString(reason, 80)),
			))
		}
		return nil, err
	}
	s.m.risk.RecordSuccess(ctx, tenantID, user.ID, req.IP)
	// Don't log login success for MFA-required — the full login completes in CompleteMFA
	if !result.MFARequired && !result.PhoneBindingRequired {
		s.m.audit(ctx, tenantID, user.ID, "login", "success", "", req.IP, req.UserAgent)
	}
	if m := s.m.metrics; m != nil {
		m.LoginTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
			attribute.String("reason", ""),
		))
		recordDuration(ctx, m.LoginDuration, loginStart,
			attribute.String("mfa", fmt.Sprintf("%t", result.MFARequired)),
		)
	}
	// Warm permission cache for subsequent authz checks
	if !result.MFARequired && !result.PhoneBindingRequired {
		_, _ = s.m.authorizer.GetEffectivePermissions(ctx, user.ID)
	}
	return result, nil
}

// BindPhoneAndLogin verifies the user's password and SMS code, binds the phone,
// satisfies the mandatory phone policy, and issues tokens.
func (s *AuthService) BindPhoneAndLogin(ctx context.Context, req BindPhoneRequest) (*AuthResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.bind_phone_login")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)
	identifier := strings.TrimSpace(req.Identifier)
	phone := normalizePhone(req.Phone)
	if identifier == "" || req.Password == "" || phone == "" || req.ChallengeID == "" || req.Code == "" {
		return nil, accountError(ErrInvalidArgument, "登录标识、密码、手机号和验证码不能为空")
	}

	var result *AuthResult
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user User
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ?", tenantID)
		switch req.IdentifierType {
		case "email":
			query = query.Where("email = ?", normalizeEmail(identifier))
		case "phone":
			query = query.Where("phone = ?", normalizePhone(identifier))
		case "username":
			query = query.Where("username = ?", identifier)
		default:
			if isEmailIdentifier(identifier) {
				query = query.Where("email = ?", normalizeEmail(identifier))
			} else {
				query = query.Where("username = ? OR phone = ?", identifier, normalizePhone(identifier))
			}
		}
		if err := query.First(&user).Error; err != nil {
			return accountError(ErrInvalidCredential, "账号或密码错误")
		}
		if user.Status == UserStatusDisabled || user.Status == UserStatusDeleted {
			return accountError(ErrPermissionDenied, "账号不可用")
		}
		if user.Status == UserStatusLocked {
			if user.LockedUntil != nil && time.Now().After(*user.LockedUntil) {
				if err := tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
					"status":       UserStatusNormal,
					"locked_until": nil,
				}).Error; err != nil {
					return err
				}
				user.Status = UserStatusNormal
				user.LockedUntil = nil
			} else {
				return accountError(ErrAccountLocked, "账号已锁定")
			}
		}
		var cred Credential
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND user_id = ? AND type = ? AND enabled = ?", tenantID, user.ID, CredentialPassword, true).
			First(&cred).Error; err != nil {
			return accountError(ErrInvalidCredential, "账号或密码错误")
		}
		if !verifyPassword(req.Password, cred.SecretHash) {
			return accountError(ErrInvalidCredential, "账号或密码错误")
		}
		if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
			TenantID:    tenantID,
			ChallengeID: req.ChallengeID,
			Code:        req.Code,
			Purpose:     VerificationPurposeBind,
			Channel:     VerificationChannelSMS,
			Target:      phone,
		}); err != nil {
			return err
		}

		now := time.Now()
		updates := map[string]any{
			"phone":             nullableString(phone),
			"phone_verified_at": &now,
			"auth_version":      gorm.Expr("auth_version + 1"),
			"profile_version":   gorm.Expr("profile_version + 1"),
		}
		if user.Status == UserStatusPending {
			updates["status"] = UserStatusNormal
		}
		if err := tx.Model(&User{}).Where("id = ? AND tenant_id = ?", user.ID, tenantID).Updates(updates).Error; err != nil {
			return err
		}
		user.Phone = nullableString(phone)
		user.PhoneVerifiedAt = &now
		user.Status = UserStatusNormal
		user.AuthVersion++

		roles, err := s.loadRoleCodes(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		// Enforce admin MFA if configured
		if s.m.cfg.AccountPolicy.RequireAdminMFA && userHasAdminRole(roles) && !s.m.mfa.HasTOTP(ctx, tenantID, user.ID) {
			return accountError(ErrPermissionDenied, "需要先配置 MFA 后才能登录")
		}

		result, err = s.issueTokens(ctx, tx, &user, roles, req.DeviceID, req.IP, req.UserAgent, "", "", "")
		if err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"last_login_at": now,
			"last_login_ip": req.IP,
		}).Error
	})
	if err != nil {
		s.m.audit(ctx, tenantID, "", "bind_phone", "failed", err.Error(), req.IP, req.UserAgent)
		return nil, err
	}
	s.m.audit(ctx, tenantID, result.User.ID, "bind_phone", "success", "", req.IP, req.UserAgent)
	s.m.audit(ctx, tenantID, result.User.ID, "login", "success", "after phone binding", req.IP, req.UserAgent)
	_ = emitOutbox(s.m.db.WithContext(ctx), EventUserLoggedIn, result.User.ID, map[string]any{
		"user_id":      result.User.ID,
		"tenant_id":    tenantID,
		"method":       "phone_binding",
		"logged_in_at": time.Now(),
	})
	// Warm permission cache
	_, _ = s.m.authorizer.GetEffectivePermissions(ctx, result.User.ID)
	return result, nil
}

// LoginWithVerificationCodeRequest 验证码登录请求参数。
type LoginWithVerificationCodeRequest struct {
	TenantID       string
	Identifier     string // email or phone
	IdentifierType string // "email" or "phone"
	ChallengeID    string
	Code           string
	DeviceID       string
	IP             string
	UserAgent      string
}

// LoginWithVerificationCode 通过邮箱或手机验证码进行登录。
// 验证码必须事先通过 VerificationService.Send 发送，使用 VerificationPurposeLogin 目的。
func (s *AuthService) LoginWithVerificationCode(ctx context.Context, req LoginWithVerificationCodeRequest) (*AuthResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.login_code")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)
	identifier := strings.TrimSpace(req.Identifier)
	if identifier == "" || req.ChallengeID == "" || req.Code == "" {
		return nil, accountError(ErrInvalidArgument, "登录标识、验证码不能为空")
	}

	// Determine the target (email or phone) and identifier type
	var channel, target string
	switch req.IdentifierType {
	case "email":
		target = normalizeEmail(identifier)
		channel = VerificationChannelEmail
	case "phone":
		target = normalizePhone(identifier)
		channel = VerificationChannelSMS
	default:
		if isEmailIdentifier(identifier) {
			target = normalizeEmail(identifier)
			channel = VerificationChannelEmail
		} else {
			target = normalizePhone(identifier)
			channel = VerificationChannelSMS
		}
	}

	var result *AuthResult
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Verify the verification code
		if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
			TenantID:    tenantID,
			ChallengeID: req.ChallengeID,
			Code:        req.Code,
			Purpose:     VerificationPurposeLogin,
			Channel:     channel,
			Target:      target,
		}); err != nil {
			return err
		}

		// Look up user by the verified email or phone
		var user User
		query := tx.Where("tenant_id = ?", tenantID)
		if channel == VerificationChannelEmail {
			query = query.Where("email = ?", target)
		} else {
			query = query.Where("phone = ?", target)
		}
		if err := query.First(&user).Error; err != nil {
			return accountError(ErrInvalidCredential, "账号不存在")
		}
		if user.Status == UserStatusDisabled || user.Status == UserStatusDeleted {
			return accountError(ErrPermissionDenied, "账号不可用")
		}
		if user.Status == UserStatusLocked {
			if user.LockedUntil != nil && time.Now().After(*user.LockedUntil) {
				if err := tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
					"status":       UserStatusNormal,
					"locked_until": nil,
				}).Error; err != nil {
					return err
				}
				user.Status = UserStatusNormal
				user.LockedUntil = nil
			} else {
				return accountError(ErrAccountLocked, "账号已锁定")
			}
		}
		if s.m.phoneBindingRequired(&user) {
			result = &AuthResult{
				User:                 user.toInfo(nil, nil),
				PhoneBindingRequired: true,
				TokenType:            "Bearer",
			}
			return nil
		}

		roles, err := s.m.auth.loadRoleCodes(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		// Enforce admin MFA if configured
		if s.m.cfg.AccountPolicy.RequireAdminMFA && userHasAdminRole(roles) && !s.m.mfa.HasTOTP(ctx, tenantID, user.ID) {
			return accountError(ErrPermissionDenied, "需要先配置 MFA 后才能登录")
		}
		result, err = s.m.auth.issueTokens(ctx, tx, &user, roles, req.DeviceID, req.IP, req.UserAgent, "", "", "")
		if err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"last_login_at": now,
			"last_login_ip": req.IP,
		}).Error
	})
	if err != nil {
		s.m.audit(ctx, tenantID, "", "login_with_code", "failed", err.Error(), req.IP, req.UserAgent)
		return nil, err
	}
	s.m.audit(ctx, tenantID, result.User.ID, "login_with_code", "success", "", req.IP, req.UserAgent)
	// Warm permission cache
	if !result.PhoneBindingRequired {
		_, _ = s.m.authorizer.GetEffectivePermissions(ctx, result.User.ID)
	}
	return result, nil
}

// Refresh 轮换刷新令牌并颁发新的访问和刷新令牌。
// 检测令牌重用（重放攻击）并撤销整个令牌族。
func (s *AuthService) Refresh(ctx context.Context, req RefreshRequest) (*AuthResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.refresh")
	defer span.End()
	if req.RefreshToken == "" {
		return nil, accountError(ErrInvalidToken, "refresh token 不能为空")
	}
	hash := tokenHash(req.RefreshToken)
	var result *AuthResult
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var oldToken RefreshToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("token_hash = ?", hash).First(&oldToken).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return accountError(ErrInvalidToken, "refresh token 无效")
			}
			return err
		}
		if oldToken.Status != RefreshActive {
			now := time.Now()
			_ = tx.Model(&RefreshToken{}).Where("family_id = ?", oldToken.FamilyID).Updates(map[string]any{
				"status":     RefreshRevoked,
				"updated_at": now,
			}).Error
			_ = tx.Model(&Session{}).Where("family_id = ?", oldToken.FamilyID).Updates(map[string]any{
				"status":     SessionRevoked,
				"revoked_at": now,
			}).Error
			if m := s.m.metrics; m != nil {
				m.TokenReplay.Add(ctx, 1)
			}
			return accountError(ErrRevokedToken, "refresh token 重放，当前会话族已撤销")
		}
		if oldToken.ExpiresAt.Before(time.Now()) {
			_ = tx.Model(&oldToken).Update("status", RefreshExpired).Error
			return accountError(ErrExpiredToken, "refresh token 已过期")
		}
		var session Session
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ?", oldToken.SessionID, SessionActive).First(&session).Error; err != nil {
			return accountError(ErrInvalidToken, "会话无效")
		}
		if session.ExpiresAt.Before(time.Now()) {
			_ = tx.Model(&session).Updates(map[string]any{"status": SessionExpired}).Error
			return accountError(ErrExpiredToken, "会话已过期")
		}
		var user User
		if err := tx.Where("id = ? AND status = ?", session.UserID, UserStatusNormal).First(&user).Error; err != nil {
			return accountError(ErrInvalidToken, "用户不可用")
		}
		if s.m.phoneBindingRequired(&user) {
			return accountError(ErrPhoneBindingRequired, "需要先绑定并验证手机号")
		}
		now := time.Now()
		if err := tx.Model(&oldToken).Updates(map[string]any{
			"status":  RefreshUsed,
			"used_at": now,
		}).Error; err != nil {
			return err
		}
		roles, err := s.loadRoleCodes(ctx, tx, user.ID)
		if err != nil {
			return err
		}
		result, err = s.issueTokens(ctx, tx, &user, roles, req.DeviceID, req.IP, req.UserAgent, session.ID, session.FamilyID, oldToken.TokenHash)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Logout 撤销会话及其所有刷新令牌。
// 从提供的访问令牌、刷新令牌或直接会话 ID 中解析会话 ID。
func (s *AuthService) Logout(ctx context.Context, req LogoutRequest) error {
	ctx, span := s.m.tracer.Start(ctx, "account.logout")
	defer span.End()
	sessionID := req.SessionID
	if sessionID == "" && req.AccessToken != "" {
		claims, err := s.parseAccessToken(req.AccessToken)
		if err == nil {
			sessionID = claims.SessionID
		}
	}
	if sessionID == "" && req.RefreshToken != "" {
		var token RefreshToken
		if err := s.m.db.WithContext(ctx).Where("token_hash = ?", tokenHash(req.RefreshToken)).First(&token).Error; err == nil {
			sessionID = token.SessionID
		}
	}
	if sessionID == "" {
		return accountError(ErrInvalidArgument, "session id 不能为空")
	}
	now := time.Now()
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Session{}).Where("id = ?", sessionID).Updates(map[string]any{
			"status":     SessionRevoked,
			"revoked_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&RefreshToken{}).Where("session_id = ?", sessionID).Update("status", RefreshRevoked).Error
	})
	if err == nil {
		_ = emitOutbox(s.m.db.WithContext(ctx), EventUserLoggedOut, "", map[string]any{
			"session_id":    sessionID,
			"logged_out_at": time.Now(),
		})
		s.invalidatePrincipalCache(ctx, sessionID)
	}
	return err
}

// LogoutAll 撤销用户的所有活跃会话和刷新令牌。
func (s *AuthService) LogoutAll(ctx context.Context, userID string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.logout_all")
	defer span.End()
	now := time.Now()
	var sessionIDs []string
	_ = s.m.db.WithContext(ctx).Model(&Session{}).Where("user_id = ?", userID).Pluck("id", &sessionIDs).Error
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Session{}).Where("user_id = ? AND status = ?", userID, SessionActive).Updates(map[string]any{
			"status":     SessionRevoked,
			"revoked_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE acct_refresh_tokens
JOIN acct_sessions ON acct_sessions.id = acct_refresh_tokens.session_id
SET acct_refresh_tokens.status = ?
WHERE acct_sessions.user_id = ?`, RefreshRevoked, userID).Error
	})
	if err == nil {
		s.invalidatePrincipalCaches(ctx, sessionIDs)
	}
	return err
}

// Validate 解析并验证访问令牌，检查租户有效性、黑名单状态、会话状态和版本新鲜度，然后返回 Principal。
func (s *AuthService) Validate(ctx context.Context, accessToken string) (*Principal, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.validate_token")
	defer span.End()
	claims, err := s.parseAccessToken(accessToken)
	if err != nil {
		s.m.audit(ctx, "", "", "validate_token", "failed", "invalid jwt", "", "")
		if m := s.m.metrics; m != nil {
			m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("reason", "invalid_jwt"),
			))
		}
		return nil, accountError(ErrInvalidToken, "access token 无效")
	}
	if s.m.cfg.TenantValidator != nil {
		if err := s.m.cfg.TenantValidator(ctx, claims.TenantID); err != nil {
			s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "tenant invalid: "+err.Error(), "", "")
			if m := s.m.metrics; m != nil {
				m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("status", "failed"),
					attribute.String("reason", "tenant_invalid"),
				))
			}
			return nil, accountError(ErrInvalidToken, "租户不可用")
		}
	}
	if s.m.cfg.AccountPolicy.RequireVerifiedPhone && !claims.PhoneVerified {
		s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "phone binding required", "", "")
		return nil, accountError(ErrPhoneBindingRequired, "需要先绑定并验证手机号")
	}
	if s.m.cfg.EnableAccessTokenDenylistCheck {
		if denied, _ := s.isDenied(ctx, claims.ID); denied {
			s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "denylisted jti", "", "")
			if m := s.m.metrics; m != nil {
				m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
					attribute.String("status", "failed"),
					attribute.String("reason", "denylisted"),
				))
			}
			return nil, accountError(ErrRevokedToken, "access token 已吊销")
		}
	}
	if principal, ok, err := s.getCachedPrincipal(ctx, claims); err != nil {
		return nil, err
	} else if ok {
		return principal, nil
	}
	var row struct {
		UserStatus       string
		CurrentAuth      int64
		CurrentRoles     int64
		SessionStatus    string
		SessionExpiresAt time.Time
		PhoneVerifiedAt  *time.Time
	}
	if err := s.m.db.WithContext(ctx).Table("acct_users").
		Select("acct_users.status AS user_status, acct_users.auth_version AS current_auth, acct_users.roles_version AS current_roles, acct_users.phone_verified_at AS phone_verified_at, acct_sessions.status AS session_status, acct_sessions.expires_at AS session_expires_at").
		Joins("JOIN acct_sessions ON acct_sessions.user_id = acct_users.id AND acct_sessions.id = ?", claims.SessionID).
		Where("acct_users.id = ? AND acct_users.tenant_id = ?", claims.UserID, claims.TenantID).
		Scan(&row).Error; err != nil {
		s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "lookup failed: "+err.Error(), "", "")
		if m := s.m.metrics; m != nil {
			m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("reason", "lookup_failed"),
			))
		}
		return nil, accountError(ErrInvalidToken, "access token 无效")
	}
	if row.UserStatus == "" {
		s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "user or session not found", "", "")
		if m := s.m.metrics; m != nil {
			m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("reason", "not_found"),
			))
		}
		return nil, accountError(ErrInvalidToken, "access token 无效")
	}
	if row.SessionStatus != SessionActive || row.SessionExpiresAt.Before(time.Now()) {
		s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "session inactive", "", "")
		if m := s.m.metrics; m != nil {
			m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("reason", "session_inactive"),
			))
		}
		return nil, accountError(ErrInvalidToken, "会话无效")
	}
	if row.UserStatus != UserStatusNormal {
		s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "user status: "+row.UserStatus, "", "")
		if m := s.m.metrics; m != nil {
			m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("reason", "user_status"),
			))
		}
		return nil, accountError(ErrInvalidToken, "用户不可用")
	}
	if claims.AuthVersion != row.CurrentAuth || claims.RolesVersion != row.CurrentRoles {
		s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "version mismatch", "", "")
		if m := s.m.metrics; m != nil {
			m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
				attribute.String("status", "failed"),
				attribute.String("reason", "version_mismatch"),
			))
		}
		return nil, accountError(ErrInvalidToken, "access token 版本已失效")
	}
	if s.m.cfg.AccountPolicy.RequireVerifiedPhone && row.PhoneVerifiedAt == nil {
		s.m.audit(ctx, claims.TenantID, claims.UserID, "validate_token", "failed", "phone binding required", "", "")
		return nil, accountError(ErrPhoneBindingRequired, "需要先绑定并验证手机号")
	}
	principal := &Principal{
		UserID:        claims.UserID,
		TenantID:      claims.TenantID,
		Username:      claims.Username,
		SessionID:     claims.SessionID,
		AuthVersion:   claims.AuthVersion,
		RolesVersion:  claims.RolesVersion,
		PhoneVerified: claims.PhoneVerified,
		Roles:         claims.Roles,
	}
	_ = s.cachePrincipal(ctx, claims, principal)
	if m := s.m.metrics; m != nil {
		m.TokenValidationTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "success"),
			attribute.String("reason", ""),
		))
	}
	return principal, nil
}

// assignDefaultRole 给新注册的账户分配内置的 "user" 角色。
func (s *AuthService) assignDefaultRole(ctx context.Context, tx *gorm.DB, user *User) error {
	var role Role
	if err := tx.WithContext(ctx).Where("tenant_id = ? AND code = ? AND status = ?", user.TenantID, "user", "enabled").First(&role).Error; err != nil {
		return fmt.Errorf("load default user role: %w", err)
	}
	userRole := UserRole{
		ID:        newID(),
		TenantID:  user.TenantID,
		UserID:    user.ID,
		RoleID:    role.ID,
		ScopeType: "tenant",
		ScopeID:   user.TenantID,
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&userRole).Error
}

// loadRoleCodes 返回分配给指定用户的所有启用角色代码。
func (s *AuthService) loadRoleCodes(ctx context.Context, tx *gorm.DB, userID string) ([]string, error) {
	var roles []string
	err := tx.WithContext(ctx).Model(&Role{}).
		Joins("JOIN acct_user_roles ON acct_user_roles.role_id = acct_roles.id").
		Where("acct_user_roles.user_id = ? AND acct_roles.status = ?", userID, "enabled").
		Pluck("acct_roles.code", &roles).Error
	return roles, err
}

// issueTokens 为用户创建会话、访问令牌和刷新令牌。
// 当 sessionID 和 familyID 为空时，将生成新的。
func (s *AuthService) issueTokens(ctx context.Context, tx *gorm.DB, user *User, roles []string, deviceID, ip, userAgent, sessionID, familyID, previousHash string) (*AuthResult, error) {
	if sessionID == "" {
		sessionID = newID()
	}
	if familyID == "" {
		familyID = newID()
	}
	sessionExpiresAt := time.Now().Add(s.m.cfg.RefreshTokenTTL)
	if previousHash == "" {
		session := Session{
			ID:            sessionID,
			TenantID:      user.TenantID,
			UserID:        user.ID,
			FamilyID:      familyID,
			DeviceID:      deviceID,
			IP:            ip,
			UserAgentHash: hashUserAgent(userAgent),
			Status:        SessionActive,
			ExpiresAt:     sessionExpiresAt,
		}
		if err := tx.Create(&session).Error; err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
	} else {
		if err := tx.Model(&Session{}).Where("id = ? AND status = ?", sessionID, SessionActive).
			Update("expires_at", sessionExpiresAt).Error; err != nil {
			return nil, fmt.Errorf("extend session: %w", err)
		}
	}
	accessToken, accessExpiresAt, err := s.signAccessToken(user, roles, sessionID)
	if err != nil {
		return nil, err
	}
	refreshToken, err := randomToken()
	if err != nil {
		return nil, err
	}
	refresh := RefreshToken{
		ID:           newID(),
		TenantID:     user.TenantID,
		SessionID:    sessionID,
		FamilyID:     familyID,
		TokenHash:    tokenHash(refreshToken),
		PreviousHash: previousHash,
		Status:       RefreshActive,
		ExpiresAt:    sessionExpiresAt,
	}
	if err := tx.WithContext(ctx).Create(&refresh).Error; err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}
	if m := s.m.metrics; m != nil {
		m.TokenIssued.Add(ctx, 2) // access + refresh
	}
	return &AuthResult{
		User:         user.toInfo(roles, nil),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    accessExpiresAt,
		TokenType:    "Bearer",
	}, nil
}

// signAccessToken 创建带有用户声明和会话 ID 的签名 JWT 访问令牌。
func (s *AuthService) signAccessToken(user *User, roles []string, sessionID string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.m.cfg.AccessTokenTTL)
	claims := accessClaims{
		UserID:        user.ID,
		TenantID:      user.TenantID,
		Username:      user.Username,
		SessionID:     sessionID,
		AuthVersion:   user.AuthVersion,
		RolesVersion:  user.RolesVersion,
		PhoneVerified: user.PhoneVerifiedAt != nil,
		Roles:         roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        newID(),
			Subject:   user.ID,
			Issuer:    s.m.cfg.JWT.Issuer,
			Audience:  s.m.cfg.JWT.Audience,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	// Use KeySet for signing if configured (supports asymmetric keys and rotation)
	if keySet := s.m.cfg.JWT.KeySet; keySet != nil {
		signed, err := keySet.Sign(claims)
		return signed, expiresAt, err
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = "hmac-default"
	signed, err := token.SignedString([]byte(s.m.cfg.JWT.SecretKey))
	return signed, expiresAt, err
}

// parseAccessToken 解析并验证 JWT 访问令牌，验证签名。
// 如果配置了 KeySet 则使用非对称密钥验证，否则回退到 HMAC。
func (s *AuthService) parseAccessToken(tokenString string) (*accessClaims, error) {
	claims := &accessClaims{}

	// Use KeySet for verification if configured (supports asymmetric keys and rotation)
	if keySet := s.m.cfg.JWT.KeySet; keySet != nil {
		token, err := keySet.ParseWithKeySet(tokenString, claims,
			jwt.WithIssuer(s.m.cfg.JWT.Issuer),
			jwt.WithAudience(s.m.cfg.JWT.Audience...),
		)
		if err != nil {
			return nil, err
		}
		if !token.Valid {
			return nil, errors.New("invalid token")
		}
		return claims, nil
	}

	// Fallback to HMAC verification
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return []byte(s.m.cfg.JWT.SecretKey), nil
	}, jwt.WithIssuer(s.m.cfg.JWT.Issuer), jwt.WithAudience(s.m.cfg.JWT.Audience...))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// principalCacheKey 返回从访问声明派生的主体的缓存键。
func (s *AuthService) principalCacheKey(claims *accessClaims) string {
	return fmt.Sprintf("principal:%s:%s:%s:%d:%d", claims.TenantID, claims.UserID, claims.SessionID, claims.AuthVersion, claims.RolesVersion)
}

// principalSessionKey 返回将会话 ID 映射到其主体缓存键的缓存键。
func (s *AuthService) principalSessionKey(sessionID string) string {
	return "principal-session:" + sessionID
}

// getCachedPrincipal 返回给定声明的缓存主体（如果有）。
func (s *AuthService) getCachedPrincipal(ctx context.Context, claims *accessClaims) (*Principal, bool, error) {
	if s.m.cache == nil {
		return nil, false, nil
	}
	raw, ok, err := s.m.cache.Get(ctx, s.principalCacheKey(claims))
	if err != nil || !ok {
		return nil, false, err
	}
	var principal Principal
	if err := json.Unmarshal([]byte(raw), &principal); err != nil {
		return nil, false, nil
	}
	return &principal, true, nil
}

// cachePrincipal 将主体存储在缓存中，TTL 受 PrincipalCacheMaxTTL 限制。
func (s *AuthService) cachePrincipal(ctx context.Context, claims *accessClaims, principal *Principal) error {
	if s.m.cache == nil || claims.ExpiresAt == nil {
		return nil
	}
	ttl := time.Until(claims.ExpiresAt.Time)
	if ttl <= 0 {
		return nil
	}
	if ttl > s.m.cfg.PrincipalCacheMaxTTL {
		ttl = s.m.cfg.PrincipalCacheMaxTTL
	}
	key := s.principalCacheKey(claims)
	data, err := json.Marshal(principal)
	if err != nil {
		return err
	}
	if err := s.m.cache.Set(ctx, key, string(data), ttl); err != nil {
		return err
	}
	return s.m.cache.Set(ctx, s.principalSessionKey(claims.SessionID), key, ttl)
}

// invalidatePrincipalCache 移除指定会话 ID 的缓存主体。
func (s *AuthService) invalidatePrincipalCache(ctx context.Context, sessionID string) {
	if s.m.cache == nil || sessionID == "" {
		return
	}
	key, ok, err := s.m.cache.Get(ctx, s.principalSessionKey(sessionID))
	if err == nil && ok {
		_ = s.m.cache.Del(ctx, key)
	}
	_ = s.m.cache.Del(ctx, s.principalSessionKey(sessionID))
}

// invalidatePrincipalCaches 移除所有指定会话 ID 的缓存主体。
func (s *AuthService) invalidatePrincipalCaches(ctx context.Context, sessionIDs []string) {
	for _, sessionID := range sessionIDs {
		s.invalidatePrincipalCache(ctx, sessionID)
	}
}

// invalidateUserPrincipalCaches 移除用户所有会话的全部缓存主体。
func (s *AuthService) invalidateUserPrincipalCaches(ctx context.Context, userID string) {
	if s.m.cache == nil || userID == "" {
		return
	}
	var sessionIDs []string
	if err := s.m.db.WithContext(ctx).Model(&Session{}).Where("user_id = ?", userID).Pluck("id", &sessionIDs).Error; err != nil {
		return
	}
	s.invalidatePrincipalCaches(ctx, sessionIDs)
}

// RequireStepUp 检查当前会话最近是否完成过 step-up MFA。
//
// 语义：
//  1. 用户**未启用 TOTP** → 直接放行（业务方可结合自身策略额外要求其它二次因素）。
//  2. principal 缺少 SessionID → 拒绝；通常意味着调用方未携带合法 access token，
//     或属于内部服务直调，敏感操作必须显式带上 sid 否则保守兜底。
//  3. 找不到对应 session 行 / session 已被吊销/过期 → 拒绝。
//  4. session.mfa_satisfied_at 为空，或距离现在已超过 cfg.StepUpWindow → 拒绝。
//
// 该检查严格按 sid 维度（每个浏览器/设备独立）：
// 用户在 Safari 上 step-up 不会让 Chrome 上的会话获得敏感操作权限。
// 不再依赖外部缓存——即使 Redis 不可用，敏感操作也不会被静默放行。
func (s *AuthService) RequireStepUp(ctx context.Context, principal *Principal) error {
	if principal == nil {
		return accountError(ErrInvalidArgument, "principal 不能为空")
	}
	// 用户未启用 TOTP，跳过 step-up（业务方如有额外需求可显式拦截）。
	if !s.m.mfa.HasTOTP(ctx, principal.TenantID, principal.UserID) {
		return nil
	}
	if principal.SessionID == "" {
		s.recordStepUpDenied(ctx, "no_sid")
		return accountError(ErrPermissionDenied, "需要二次验证才能执行此操作")
	}

	var sess Session
	if err := s.m.db.WithContext(ctx).
		Select("id", "status", "mfa_satisfied_at").
		Where("id = ? AND tenant_id = ? AND user_id = ?",
			principal.SessionID, principal.TenantID, principal.UserID).
		First(&sess).Error; err != nil {
		s.recordStepUpDenied(ctx, "session_not_found")
		return accountError(ErrPermissionDenied, "需要二次验证才能执行此操作")
	}
	if sess.Status != SessionActive {
		s.recordStepUpDenied(ctx, "session_inactive")
		return accountError(ErrInvalidToken, "会话已失效")
	}

	window := s.m.cfg.StepUpWindow
	if window <= 0 {
		window = 5 * time.Minute
	}
	if sess.MFASatisfiedAt == nil || time.Since(*sess.MFASatisfiedAt) > window {
		s.recordStepUpDenied(ctx, "expired_or_missing")
		return accountError(ErrPermissionDenied, "需要二次验证才能执行此操作")
	}
	return nil
}

// RequestStepUp 创建 step-up MFA 挑战，返回挑战 ID。
// 客户端应使用此 ID 引导用户完成 TOTP 验证。
func (s *AuthService) RequestStepUp(ctx context.Context, principal *Principal) (string, error) {
	if principal == nil {
		return "", accountError(ErrInvalidArgument, "principal 不能为空")
	}
	if !s.m.mfa.HasTOTP(ctx, principal.TenantID, principal.UserID) {
		return "", accountError(ErrInvalidArgument, "没有配置 MFA，无需二次验证")
	}
	// Read current auth version
	var user User
	if err := s.m.db.WithContext(ctx).Where("id = ?", principal.UserID).First(&user).Error; err != nil {
		return "", err
	}
	challenge, err := s.m.mfa.CreateMFAChallenge(ctx, principal.TenantID, principal.UserID, user.AuthVersion)
	if err != nil {
		return "", err
	}
	return challenge.ID, nil
}

// CompleteStepUp 验证 step-up MFA 挑战，并把成功标记写入「当前 session」（按 sid）。
//
// 实现要点：
//   - 标记仅对发起 step-up 的那一个 session 有效——同一用户在其他浏览器/设备上的
//     会话不会自动获得敏感操作权限。
//   - 标记的有效期由 cfg.StepUpWindow 控制（默认 5 分钟）。
//   - 失败时同样递增 challenge 的 attempts 计数，使 MaxAttempts=5 限速生效，
//     防止攻击者持有 access_token 后在 5 分钟挑战窗口内对 6 位数暴力尝试。
//   - TOTP 验证通过 + session 标记写入 在同一事务中完成，避免跨事务半成功。
func (s *AuthService) CompleteStepUp(ctx context.Context, principal *Principal, challengeID, code string) error {
	if principal == nil {
		return accountError(ErrInvalidArgument, "principal 不能为空")
	}
	if principal.SessionID == "" {
		return accountError(ErrInvalidArgument, "principal.SessionID 不能为空")
	}
	tenantID, userID, _, err := s.m.mfa.ValidateMFAChallenge(ctx, challengeID)
	if err != nil {
		return err
	}
	if userID != principal.UserID || tenantID != principal.TenantID {
		return accountError(ErrPermissionDenied, "MFA 挑战不属于当前用户")
	}

	err = s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ok, verr := s.m.mfa.verifyTOTPTx(ctx, tx, tenantID, userID, code)
		if verr != nil {
			return accountError(ErrInternal, "MFA 验证失败")
		}
		if !ok {
			// TOTP 错误：在同一事务内递增 challenge.attempts，达到 MaxAttempts 后挑战自动作废。
			_ = tx.Model(&MFAChallenge{}).Where("id = ?", challengeID).
				Update("attempts", gorm.Expr("attempts + 1")).Error
			return accountError(ErrInvalidCredential, "MFA 验证码错误")
		}
		// 仅给当前 session 打 step-up 标记
		now := time.Now()
		res := tx.Model(&Session{}).
			Where("id = ? AND tenant_id = ? AND user_id = ? AND status = ?",
				principal.SessionID, principal.TenantID, principal.UserID, SessionActive).
			Update("mfa_satisfied_at", &now)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return accountError(ErrInvalidToken, "当前会话已失效")
		}
		// 消费 challenge：成功路径
		nowTs := time.Now()
		return tx.Model(&MFAChallenge{}).Where("id = ?", challengeID).Updates(map[string]any{
			"consumed_at": &nowTs,
			"attempts":    gorm.Expr("attempts + 1"),
		}).Error
	})
	if err != nil {
		return err
	}

	s.m.audit(ctx, principal.TenantID, principal.UserID, "mfa", "stepup", "", "", "")
	if m := s.m.metrics; m != nil && m.StepUpGranted != nil {
		m.StepUpGranted.Add(ctx, 1)
	}
	return nil
}

// recordStepUpDenied 记录 step-up 检查失败的指标。
func (s *AuthService) recordStepUpDenied(ctx context.Context, reason string) {
	if m := s.m.metrics; m != nil && m.StepUpDenied != nil {
		m.StepUpDenied.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
	}
}

// adminMFAFallback 为未配置 TOTP 的管理员用户触发 email/SMS MFA。
// 返回 AuthResult（MFARequired=true）表示成功发送验证码，返回 nil 表示无可用通道。
func (s *AuthService) adminMFAFallback(ctx context.Context, tx *gorm.DB, user *User, ip string) *AuthResult {
	// Try email first, then SMS
	channels := []string{VerificationChannelEmail, VerificationChannelSMS}
	for _, channel := range channels {
		var target string
		switch channel {
		case VerificationChannelEmail:
			if user.Email != nil && *user.Email != "" {
				target = *user.Email
			}
		case VerificationChannelSMS:
			if user.Phone != nil && *user.Phone != "" {
				target = *user.Phone
			}
		}
		if target == "" {
			continue
		}

		vcResult, err := s.m.verification.Send(ctx, SendVerificationRequest{
			TenantID: user.TenantID,
			Channel:  channel,
			Target:   target,
			Purpose:  VerificationPurposeMFA,
			IP:       ip,
		})
		if err != nil {
			continue
		}

		mfaChallenge := &MFAChallenge{
			ID:                      newID(),
			TenantID:                user.TenantID,
			UserID:                  user.ID,
			AuthVersion:             user.AuthVersion,
			MaxAttempts:             5,
			ExpiresAt:               time.Now().Add(5 * time.Minute),
			Method:                  channel,
			VerificationChallengeID: vcResult.ChallengeID,
		}
		if err := tx.Create(mfaChallenge).Error; err != nil {
			continue
		}

		return &AuthResult{
			MFARequired:  true,
			MFAChallenge: mfaChallenge.ID,
		}
	}
	return nil
}
