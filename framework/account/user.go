package account

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// UserService 提供用户资料查询、更新、修改密码和删除账户功能。
type UserService struct {
	m *Manager
}

// UserInfo 用户信息，包含角色和权限的只读视图。
type UserInfo struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	Username        string     `json:"username"`
	Email           string     `json:"email"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
	Phone           string     `json:"phone"`
	PhoneVerifiedAt *time.Time `json:"phone_verified_at"`
	Nickname        string     `json:"nickname"`
	AvatarURL       string     `json:"avatar_url"`
	Status          string     `json:"status"`
	Roles           []string   `json:"roles"`
	Permissions     []string   `json:"permissions"`
	CreatedAt       time.Time  `json:"created_at"`
}

// UpdateProfileRequest 更新用户资料的请求参数。
type UpdateProfileRequest struct {
	UserID    string
	Nickname  string
	AvatarURL string
}

// ChangePasswordRequest 修改密码的请求参数。
type ChangePasswordRequest struct {
	UserID      string
	OldPassword string
	NewPassword string
	// Principal 为可选的登录主体，设置后将强制检查 step-up MFA。
	Principal *Principal
}

// DeleteAccountRequest 删除账户的请求参数。
type DeleteAccountRequest struct {
	UserID string
}

func (u User) toInfo(roles, permissions []string) *UserInfo {
	return &UserInfo{
		ID:              u.ID,
		TenantID:        u.TenantID,
		Username:        u.Username,
		Email:           stringValue(u.Email),
		EmailVerifiedAt: u.EmailVerifiedAt,
		Phone:           stringValue(u.Phone),
		PhoneVerifiedAt: u.PhoneVerifiedAt,
		Nickname:        u.Nickname,
		AvatarURL:       u.AvatarURL,
		Status:          u.Status,
		Roles:           roles,
		Permissions:     permissions,
		CreatedAt:       u.CreatedAt,
	}
}

// GetByID 返回指定用户 ID 的用户信息、角色和有效权限。
func (s *UserService) GetByID(ctx context.Context, userID string) (*UserInfo, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.user.get_by_id")
	defer span.End()
	var user User
	if err := s.m.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	roles, err := s.m.auth.loadRoleCodes(ctx, s.m.db, user.ID)
	if err != nil {
		return nil, err
	}
	perms, err := s.m.authorizer.GetEffectivePermissionsForUser(ctx, user.ID, user.TenantID, user.RolesVersion)
	if err != nil {
		return nil, err
	}
	return user.toInfo(roles, perms), nil
}

// UpdateProfile 更新用户的昵称和/或头像 URL，返回更新后的资料。
func (s *UserService) UpdateProfile(ctx context.Context, req UpdateProfileRequest) (*UserInfo, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.user.update_profile")
	defer span.End()
	updates := map[string]any{
		"profile_version": gorm.Expr("profile_version + 1"),
	}
	if req.Nickname != "" {
		updates["nickname"] = req.Nickname
	}
	if req.AvatarURL != "" {
		updates["avatar_url"] = req.AvatarURL
	}
	if err := s.m.db.WithContext(ctx).Model(&User{}).Where("id = ?", req.UserID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(ctx, req.UserID)
}

// ChangePassword 验证旧密码，设置新密码，增加 auth_version 并撤销所有现有会话。
func (s *UserService) ChangePassword(ctx context.Context, req ChangePasswordRequest) error {
	ctx, span := s.m.tracer.Start(ctx, "account.user.change_password")
	defer span.End()
	// Enforce step-up MFA if principal is provided
	if req.Principal != nil {
		if err := s.m.auth.RequireStepUp(ctx, req.Principal); err != nil {
			return err
		}
	}
	if err := validatePassword(req.NewPassword, s.m.cfg.Password); err != nil {
		return err
	}
	var sessionIDs []string
	_ = s.m.db.WithContext(ctx).Model(&Session{}).Where("user_id = ?", req.UserID).Pluck("id", &sessionIDs).Error
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cred Credential
		if err := tx.Where("user_id = ? AND type = ? AND enabled = ?", req.UserID, CredentialPassword, true).First(&cred).Error; err != nil {
			return accountError(ErrInvalidCredential, "密码凭证不存在")
		}
		if !verifyPassword(req.OldPassword, cred.SecretHash) {
			return accountError(ErrInvalidCredential, "旧密码错误")
		}
		newHash, err := hashPassword(req.NewPassword, s.m.cfg.Password)
		if err != nil {
			return fmt.Errorf("hash new password: %w", err)
		}
		if err := tx.Model(&cred).Update("secret_hash", newHash).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", req.UserID).Update("auth_version", gorm.Expr("auth_version + 1")).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&Session{}).Where("user_id = ? AND status = ?", req.UserID, SessionActive).Updates(map[string]any{
			"status":     SessionRevoked,
			"revoked_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE acct_refresh_tokens
JOIN acct_sessions ON acct_sessions.id = acct_refresh_tokens.session_id
SET acct_refresh_tokens.status = ?
WHERE acct_sessions.user_id = ?`, RefreshRevoked, req.UserID).Error
	})
	if err == nil {
		s.m.auth.invalidatePrincipalCaches(ctx, sessionIDs)
		_ = emitOutbox(s.m.db.WithContext(ctx), EventPasswordChanged, "", map[string]any{
			"user_id":             req.UserID,
			"password_changed_at": time.Now(),
		})
	}
	return err
}

// DeleteAccount 软删除用户，禁用凭证，撤销会话并清除角色分配。
func (s *UserService) DeleteAccount(ctx context.Context, req DeleteAccountRequest) error {
	ctx, span := s.m.tracer.Start(ctx, "account.user.delete")
	defer span.End()
	now := time.Now()
	var sessionIDs []string
	_ = s.m.db.WithContext(ctx).Model(&Session{}).Where("user_id = ?", req.UserID).Pluck("id", &sessionIDs).Error
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Credential{}).Where("user_id = ?", req.UserID).Update("enabled", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&Session{}).Where("user_id = ? AND status = ?", req.UserID, SessionActive).Updates(map[string]any{
			"status":     SessionRevoked,
			"revoked_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE acct_refresh_tokens
JOIN acct_sessions ON acct_sessions.id = acct_refresh_tokens.session_id
SET acct_refresh_tokens.status = ?
WHERE acct_sessions.user_id = ?`, RefreshRevoked, req.UserID).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", req.UserID).Delete(&UserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", req.UserID).Updates(map[string]any{
			"status":       UserStatusDeleted,
			"auth_version": gorm.Expr("auth_version + 1"),
		}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", req.UserID).Delete(&User{}).Error
	})
	if err == nil {
		s.m.auth.invalidatePrincipalCaches(ctx, sessionIDs)
		_ = s.m.authorizer.invalidatePermissions(ctx, req.UserID)
		_ = emitOutbox(s.m.db.WithContext(ctx), EventUserDeleted, req.UserID, map[string]any{
			"user_id":    req.UserID,
			"deleted_at": time.Now(),
		})
	}
	return err
}

// GetCurrent 返回当前登录用户的完整信息（含角色和权限）。
// userID 应从已认证的 Principal 中获取。
func (s *UserService) GetCurrent(ctx context.Context, userID string) (*UserInfo, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.user.get_current")
	defer span.End()
	return s.GetByID(ctx, userID)
}

// StartResetPasswordRequest 发起重置密码的请求参数。
type StartResetPasswordRequest struct {
	TenantID   string
	Username   string
	Identifier string // email or phone
	Channel    string // "email" or "sms"
}

// StartResetPassword 验证用户身份并向其邮箱/手机发送重置密码验证码。
func (s *UserService) StartResetPassword(ctx context.Context, req StartResetPasswordRequest) (*SendVerificationResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.user.start_reset_password")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)
	username := strings.TrimSpace(req.Username)
	identifier := strings.TrimSpace(req.Identifier)
	if username == "" && identifier == "" {
		return nil, accountError(ErrInvalidArgument, "邮箱或手机号不能为空")
	}

	channel := strings.ToLower(req.Channel)
	target := identifier
	if username != "" {
		var user User
		if err := s.m.db.WithContext(ctx).Where("tenant_id = ? AND username = ?", tenantID, username).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, accountError(ErrInvalidArgument, "账号不存在")
			}
			return nil, err
		}
		if user.Status == UserStatusDisabled || user.Status == UserStatusDeleted {
			return nil, accountError(ErrPermissionDenied, "账号不可用")
		}
		switch channel {
		case VerificationChannelEmail:
			req.Channel = VerificationChannelEmail
			if user.Email == nil || *user.Email == "" {
				return nil, accountError(ErrInvalidArgument, "用户未绑定邮箱")
			}
			target = normalizeEmail(*user.Email)
		case VerificationChannelSMS:
			req.Channel = VerificationChannelSMS
			if user.Phone == nil || *user.Phone == "" {
				return nil, accountError(ErrInvalidArgument, "用户未绑定手机号")
			}
			target = normalizePhone(*user.Phone)
		default:
			return nil, accountError(ErrInvalidArgument, "验证码渠道必须是 email 或 sms")
		}
	} else {
		var count int64
		switch channel {
		case VerificationChannelEmail:
			req.Channel = VerificationChannelEmail
			target = normalizeEmail(identifier)
			if err := s.m.db.WithContext(ctx).Model(&User{}).Where("tenant_id = ? AND email = ?", tenantID, target).Count(&count).Error; err != nil {
				return nil, err
			}
		case VerificationChannelSMS:
			req.Channel = VerificationChannelSMS
			target = normalizePhone(identifier)
			if err := s.m.db.WithContext(ctx).Model(&User{}).Where("tenant_id = ? AND phone = ?", tenantID, target).Count(&count).Error; err != nil {
				return nil, err
			}
		default:
			if isEmailIdentifier(identifier) {
				req.Channel = VerificationChannelEmail
				channel = VerificationChannelEmail
				target = normalizeEmail(identifier)
				if err := s.m.db.WithContext(ctx).Model(&User{}).Where("tenant_id = ? AND email = ?", tenantID, target).Count(&count).Error; err != nil {
					return nil, err
				}
			} else {
				req.Channel = VerificationChannelSMS
				channel = VerificationChannelSMS
				target = normalizePhone(identifier)
				if err := s.m.db.WithContext(ctx).Model(&User{}).Where("tenant_id = ? AND phone = ?", tenantID, target).Count(&count).Error; err != nil {
					return nil, err
				}
			}
		}
		if count == 0 {
			if channel == VerificationChannelEmail {
				return nil, accountError(ErrInvalidArgument, "未找到绑定该邮箱的账号")
			}
			return nil, accountError(ErrInvalidArgument, "未找到绑定该手机号的账号")
		}
	}

	result, err := s.m.verification.Send(ctx, SendVerificationRequest{
		TenantID: tenantID,
		Channel:  req.Channel,
		Target:   target,
		Purpose:  VerificationPurposeResetPassword,
	})
	if err != nil {
		return nil, err
	}

	s.m.audit(ctx, tenantID, "", "start_reset_password", "success", "channel: "+req.Channel, "", "")
	return result, nil
}

// CompleteResetPasswordRequest 完成重置密码的请求参数。
type CompleteResetPasswordRequest struct {
	TenantID    string
	ChallengeID string
	Code        string
	NewPassword string
}

// CompleteResetPassword 验证重置密码验证码并更新密码。
func (s *UserService) CompleteResetPassword(ctx context.Context, req CompleteResetPasswordRequest) error {
	ctx, span := s.m.tracer.Start(ctx, "account.user.complete_reset_password")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)

	var userID string
	var sessionIDs []string
	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var challenge VerificationChallenge
		if err := tx.Where("id = ? AND tenant_id = ?", req.ChallengeID, tenantID).First(&challenge).Error; err != nil {
			return accountError(ErrInvalidArgument, "验证码挑战不存在")
		}
		if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
			TenantID:    tenantID,
			ChallengeID: req.ChallengeID,
			Code:        req.Code,
			Purpose:     VerificationPurposeResetPassword,
		}); err != nil {
			return err
		}

		var user User
		query := tx.Where("tenant_id = ?", tenantID)
		switch challenge.Channel {
		case VerificationChannelEmail:
			query = query.Where("SHA2(email, 256) = ?", challenge.TargetHash)
		case VerificationChannelSMS:
			query = query.Where("SHA2(phone, 256) = ?", challenge.TargetHash)
		default:
			return accountError(ErrInvalidArgument, "验证码渠道无效")
		}
		if err := query.First(&user).Error; err != nil {
			return accountError(ErrInvalidArgument, "账号不存在")
		}
		userID = user.ID

		if err := validatePassword(req.NewPassword, s.m.cfg.Password); err != nil {
			return err
		}
		newHash, err := hashPassword(req.NewPassword, s.m.cfg.Password)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		if err := tx.Model(&Credential{}).Where("user_id = ? AND type = ? AND enabled = ?", user.ID, CredentialPassword, true).
			Update("secret_hash", newHash).Error; err != nil {
			return err
		}
		if err := tx.Model(&User{}).Where("id = ?", user.ID).Update("auth_version", gorm.Expr("auth_version + 1")).Error; err != nil {
			return err
		}
		_ = tx.Model(&Session{}).Where("user_id = ?", user.ID).Pluck("id", &sessionIDs).Error
		if err := tx.Model(&Session{}).Where("user_id = ? AND status = ?", user.ID, SessionActive).Updates(map[string]any{
			"status":     SessionRevoked,
			"revoked_at": time.Now(),
		}).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE acct_refresh_tokens
	JOIN acct_sessions ON acct_sessions.id = acct_refresh_tokens.session_id
	SET acct_refresh_tokens.status = ?
	WHERE acct_sessions.user_id = ?`, RefreshRevoked, user.ID).Error
	})
	if err != nil {
		return err
	}

	s.m.auth.invalidatePrincipalCaches(ctx, sessionIDs)
	s.m.audit(ctx, tenantID, userID, "reset_password", "success", "", "", "")
	return nil
}

// BindEmailRequest 绑定邮箱的请求参数。
type BindEmailRequest struct {
	TenantID    string
	UserID      string
	Email       string
	ChallengeID string
	Code        string
}

// BindEmail 通过验证码验证绑定邮箱。
func (s *UserService) BindEmail(ctx context.Context, req BindEmailRequest) error {
	ctx, span := s.m.tracer.Start(ctx, "account.user.bind_email")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)
	email := normalizeEmail(req.Email)

	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
			TenantID:    tenantID,
			ChallengeID: req.ChallengeID,
			Code:        req.Code,
			Purpose:     VerificationPurposeBind,
			Channel:     VerificationChannelEmail,
			Target:      email,
		}); err != nil {
			return err
		}

		var count int64
		if err := tx.Model(&User{}).Where("tenant_id = ? AND email = ? AND id != ?", tenantID, email, req.UserID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return accountError(ErrIdentifierExists, "邮箱已被其他账号绑定")
		}

		now := time.Now()
		return tx.Model(&User{}).Where("id = ? AND tenant_id = ?", req.UserID, tenantID).Updates(map[string]any{
			"email":             &email,
			"email_verified_at": &now,
			"profile_version":   gorm.Expr("profile_version + 1"),
		}).Error
	})
}

// BindPhone 通过验证码验证绑定手机号。
func (s *UserService) BindPhone(ctx context.Context, tenantID, userID, phone, challengeID, code string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.user.bind_phone")
	defer span.End()
	tenantID = s.m.tenantID(tenantID)
	phone = normalizePhone(phone)

	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.m.verification.verifyTx(ctx, tx, VerifyCodeRequest{
			TenantID:    tenantID,
			ChallengeID: challengeID,
			Code:        code,
			Purpose:     VerificationPurposeBind,
			Channel:     VerificationChannelSMS,
			Target:      phone,
		}); err != nil {
			return err
		}

		var count int64
		if err := tx.Model(&User{}).Where("tenant_id = ? AND phone = ? AND id != ?", tenantID, phone, userID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return accountError(ErrIdentifierExists, "手机号已被其他账号绑定")
		}

		now := time.Now()
		return tx.Model(&User{}).Where("id = ? AND tenant_id = ?", userID, tenantID).Updates(map[string]any{
			"phone":             &phone,
			"phone_verified_at": &now,
			"profile_version":   gorm.Expr("profile_version + 1"),
		}).Error
	})
}
