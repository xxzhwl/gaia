package account

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OAuthProvider OAuth/OIDC 提供商接口定义。
// 每个提供商（Google、GitHub、微信等）必须实现此接口。
type OAuthProvider interface {
	// Name returns the provider identifier (e.g. "google", "github").
	Name() string

	// ExchangeCode exchanges an authorization code for tokens.
	ExchangeCode(ctx context.Context, req OAuthExchangeRequest) (*OAuthToken, error)

	// GetUserInfo fetches user info from the provider using the token.
	// The implementation must validate the ID token (if OIDC) and verify the
	// signature, issuer, audience, and nonce.
	GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUserInfo, error)
}

// OAuthExchangeRequest contains the parameters needed to exchange an OAuth code.
type OAuthExchangeRequest struct {
	Code         string
	RedirectURI  string
	State        string
	Nonce        string
	CodeVerifier string
}

// OAuthToken OAuth 提供商返回的令牌数据。
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	OpenID       string `json:"openid,omitempty"` // provider's user ID (WeChat/QQ etc.)
	Nonce        string `json:"-"`                // expected nonce for ID token validation
}

// OAuthUserInfo OAuth/OIDC 提供商返回的用户信息。
type OAuthUserInfo struct {
	Subject       string `json:"sub"` // provider's unique user ID
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	AvatarURL     string `json:"avatar_url"`
}

// IdpClient OAuth 2.0 / OIDC 客户端注册信息。
// 第三方应用通过此客户端与 IdP 交互。
type IdpClient struct {
	ID                string    `gorm:"size:36;primaryKey"`
	TenantID          string    `gorm:"size:64;not null;uniqueIndex:uniq_acct_idp_clients,priority:1"`
	ClientID          string    `gorm:"size:128;not null;uniqueIndex:uniq_acct_idp_clients,priority:2"`
	ClientSecret      string    `gorm:"size:256;not null"`
	Name              string    `gorm:"size:200;not null"`
	RedirectURIs      string    `gorm:"type:text;not null"` // JSON array of allowed redirect URIs
	AllowedGrantTypes string    `gorm:"size:255;not null;default:authorization_code"` // comma-separated
	Scopes            string    `gorm:"size:255;not null;default:openid,profile"`
	Status            string    `gorm:"size:20;not null;default:enabled"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (IdpClient) TableName() string { return "acct_idp_clients" }

// AuthorizationCode OAuth 2.0 授权码。
type AuthorizationCode struct {
	Code                string    `gorm:"size:128;primaryKey"`
	TenantID            string    `gorm:"size:64;not null;index"`
	ClientID            string    `gorm:"size:128;not null;index"`
	UserID              string    `gorm:"size:36;not null"`
	Scopes              string    `gorm:"size:255;not null"`
	RedirectURI         string    `gorm:"size:512;not null"`
	CodeChallenge       string    `gorm:"size:128"`
	CodeChallengeMethod string    `gorm:"size:16"`
	Nonce               string    `gorm:"size:128"`
	ExpiresAt           time.Time `gorm:"not null;index"`
	CreatedAt           time.Time `json:"created_at"`
}

func (AuthorizationCode) TableName() string { return "acct_auth_codes" }

// PasskeyCredential WebAuthn 通行密钥凭证。
type PasskeyCredential struct {
	ID              string    `gorm:"size:36;primaryKey"`
	TenantID        string    `gorm:"size:64;not null;uniqueIndex:uniq_acct_passkey_credential,priority:1"`
	UserID          string    `gorm:"size:36;not null;index"`
	CredentialID    string    `gorm:"size:512;not null;uniqueIndex:uniq_acct_passkey_credential,priority:2"`
	PublicKey       string    `gorm:"type:text;not null"`  // CBOR-encoded public key
	CredType        string    `gorm:"size:32;not null;default:public-key"`
	AAGUID          string    `gorm:"size:64"`
	SignCount       int64     `gorm:"not null;default:0"`
	DeviceName      string    `gorm:"size:128"`
	Transports      string    `gorm:"size:255"` // comma-separated
	AttestationType string    `gorm:"size:32"`
	BackupEligible  bool      `gorm:"not null;default:false"`
	BackupState     bool      `gorm:"not null;default:false"`
	Enabled         bool      `gorm:"not null;default:true"`
	LastUsedAt      *time.Time
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (PasskeyCredential) TableName() string { return "acct_passkey_credentials" }

// OAuthAccount 将提供商身份关联到本地账户。
type OAuthAccount struct {
	ID        string    `gorm:"size:36;primaryKey"`
	TenantID  string    `gorm:"size:64;not null;uniqueIndex:uniq_acct_oauth,priority:1"`
	UserID    string    `gorm:"size:36;not null;index:idx_acct_oauth_user"`
	Provider  string    `gorm:"size:32;not null;uniqueIndex:uniq_acct_oauth,priority:2"`
	Subject   string    `gorm:"size:128;not null;uniqueIndex:uniq_acct_oauth,priority:3"`
	Email     string    `gorm:"size:160"`
	Name      string    `gorm:"size:160"`
	AvatarURL string    `gorm:"size:512"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (OAuthAccount) TableName() string { return "acct_oauth_accounts" }

type OAuthService struct {
	m *Manager
}

// OAuthLoginRequest 使用 OAuth 提供商登录的请求参数。
type OAuthLoginRequest struct {
	TenantID     string
	Provider     string
	Code         string
	RedirectURI  string
	State        string
	Nonce        string
	CodeVerifier string
	DeviceID     string
	IP           string
	UserAgent    string
}

// OAuthBindRequest 将 OAuth 账户绑定到现有用户的请求参数。
type OAuthBindRequest struct {
	TenantID     string
	UserID       string
	Provider     string
	Code         string
	RedirectURI  string
	State        string
	Nonce        string
	CodeVerifier string
}

// OAuthLogin 通过 OAuth/OIDC 提供商登录或注册用户。
func (s *OAuthService) Login(ctx context.Context, req OAuthLoginRequest) (*AuthResult, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.oauth.login")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)

	provider, ok := s.m.cfg.OAuthProviders[req.Provider]
	if !ok {
		return nil, accountError(ErrInvalidArgument, fmt.Sprintf("不支持的OAuth提供商: %s", req.Provider))
	}

	// Exchange code for tokens
	oauthToken, err := provider.ExchangeCode(ctx, OAuthExchangeRequest{
		Code:         req.Code,
		RedirectURI:  req.RedirectURI,
		State:        req.State,
		Nonce:        req.Nonce,
		CodeVerifier: req.CodeVerifier,
	})
	if err != nil {
		s.m.audit(ctx, tenantID, "", "oauth_login", "failed",
			fmt.Sprintf("code exchange failed: %s", err.Error()), req.IP, req.UserAgent)
		return nil, accountError(ErrInvalidCredential, "OAuth 登录失败：授权码无效")
	}

	// Get user info from provider
	userInfo, err := provider.GetUserInfo(ctx, oauthToken)
	if err != nil {
		s.m.audit(ctx, tenantID, "", "oauth_login", "failed",
			fmt.Sprintf("get user info failed: %s", err.Error()), req.IP, req.UserAgent)
		return nil, accountError(ErrInvalidCredential, "OAuth 登录失败：无法获取用户信息")
	}

	// Look for existing OAuth binding
	var oauthAccount OAuthAccount
	var result *AuthResult
	err = s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND provider = ? AND subject = ?",
			tenantID, req.Provider, userInfo.Subject).First(&oauthAccount).Error; err != nil {
			// No existing binding — auto-create user
			return s.createUserFromOAuth(ctx, tx, tenantID, req, userInfo, &result)
		}

		// Existing binding — find user and issue tokens
		var user User
		if err := tx.Where("id = ? AND tenant_id = ?", oauthAccount.UserID, tenantID).First(&user).Error; err != nil {
			return accountError(ErrInvalidCredential, "关联账号不存在")
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
		// Enforce admin MFA: require TOTP, or fall back to email/SMS verification
		if s.m.cfg.AccountPolicy.RequireAdminMFA && userHasAdminRole(roles) && !s.m.mfa.HasTOTP(ctx, tenantID, user.ID) {
			if r := s.m.auth.adminMFAFallback(ctx, tx, &user, req.IP); r != nil {
				result = r
				return nil
			}
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
		return nil, err
	}

	s.m.audit(ctx, tenantID, result.User.ID, "oauth_login", "success",
		"provider: "+req.Provider, req.IP, req.UserAgent)
	_ = emitOutbox(s.m.db.WithContext(ctx), EventUserLoggedIn, result.User.ID, map[string]any{
		"user_id": result.User.ID,
		"tenant_id": tenantID,
		"method": "oauth",
		"provider": req.Provider,
		"logged_in_at": time.Now(),
	})
	// Warm permission cache
	_, _ = s.m.authorizer.GetEffectivePermissions(ctx, result.User.ID)
	return result, nil
}

// Bind 将 OAuth 身份绑定到现有用户账户。
func (s *OAuthService) Bind(ctx context.Context, req OAuthBindRequest) error {
	ctx, span := s.m.tracer.Start(ctx, "account.oauth.bind")
	defer span.End()
	tenantID := s.m.tenantID(req.TenantID)

	provider, ok := s.m.cfg.OAuthProviders[req.Provider]
	if !ok {
		return accountError(ErrInvalidArgument, fmt.Sprintf("不支持的OAuth提供商: %s", req.Provider))
	}

	oauthToken, err := provider.ExchangeCode(ctx, OAuthExchangeRequest{
		Code:         req.Code,
		RedirectURI:  req.RedirectURI,
		State:        req.State,
		Nonce:        req.Nonce,
		CodeVerifier: req.CodeVerifier,
	})
	if err != nil {
		return accountError(ErrInvalidCredential, "OAuth 授权码无效")
	}

	userInfo, err := provider.GetUserInfo(ctx, oauthToken)
	if err != nil {
		return accountError(ErrInvalidCredential, "无法获取OAuth用户信息")
	}

	// Check if this OAuth account is already bound
	var count int64
	_ = s.m.db.WithContext(ctx).Model(&OAuthAccount{}).
		Where("tenant_id = ? AND provider = ? AND subject = ?",
			tenantID, req.Provider, userInfo.Subject).
		Count(&count)
	if count > 0 {
		return accountError(ErrIdentifierExists, "该OAuth账号已被绑定")
	}

	oauthAccount := &OAuthAccount{
		ID:        newID(),
		TenantID:  tenantID,
		UserID:    req.UserID,
		Provider:  req.Provider,
		Subject:   userInfo.Subject,
		Email:     userInfo.Email,
		Name:      userInfo.Name,
		AvatarURL: userInfo.AvatarURL,
	}
	if err := s.m.db.WithContext(ctx).Create(oauthAccount).Error; err != nil {
		return fmt.Errorf("create oauth account: %w", err)
	}

	s.m.audit(ctx, tenantID, req.UserID, "oauth_bind", "success",
		"provider: "+req.Provider, "", "")
	_ = emitOutbox(s.m.db.WithContext(ctx), EventOAuthBound, req.UserID, map[string]any{
		"user_id": req.UserID,
		"tenant_id": tenantID,
		"provider": req.Provider,
		"bound_at": time.Now(),
	})
	return nil
}

// Unbind 从用户账户移除 OAuth 绑定。
func (s *OAuthService) Unbind(ctx context.Context, tenantID, userID, provider string) error {
	ctx, span := s.m.tracer.Start(ctx, "account.oauth.unbind")
	defer span.End()
	tenantID = s.m.tenantID(tenantID)
	result := s.m.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND provider = ?",
		tenantID, userID, provider).Delete(&OAuthAccount{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return accountError(ErrInvalidArgument, "OAuth 绑定不存在")
	}
	s.m.audit(ctx, tenantID, userID, "oauth_unbind", "success",
		"provider: "+provider, "", "")
	_ = emitOutbox(s.m.db.WithContext(ctx), EventOAuthUnbound, userID, map[string]any{
		"user_id": userID,
		"tenant_id": tenantID,
		"provider": provider,
		"unbound_at": time.Now(),
	})
	return nil
}

// GetOAuthAccounts 返回用户的所有 OAuth 绑定。
func (s *OAuthService) GetOAuthAccounts(ctx context.Context, tenantID, userID string) ([]OAuthAccount, error) {
	var accounts []OAuthAccount
	err := s.m.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?",
		s.m.tenantID(tenantID), userID).Find(&accounts).Error
	return accounts, err
}

// createUserFromOAuth creates a new user from OAuth user info during first-time login.
func (s *OAuthService) createUserFromOAuth(ctx context.Context, tx *gorm.DB, tenantID string,
	req OAuthLoginRequest, userInfo *OAuthUserInfo, result **AuthResult) error {

	// If email is provided and no existing account, use it; otherwise generate a username
	email := normalizeEmail(userInfo.Email)
	username := s.generateOAuthUsername(ctx, tenantID, req.Provider, userInfo.Subject)

	user := User{
		ID:             newID(),
		TenantID:       tenantID,
		Username:       username,
		Email:          nullableString(email),
		Nickname:       truncateString(userInfo.Name, 80),
		AvatarURL:      truncateString(userInfo.AvatarURL, 512),
		Status:         UserStatusNormal,
		AuthVersion:    1,
		RolesVersion:   1,
		ProfileVersion: 1,
	}
	if email != "" && userInfo.EmailVerified {
		now := time.Now()
		user.EmailVerifiedAt = &now
	}
	if s.m.cfg.AccountPolicy.RequireVerifiedPhone {
		user.Status = UserStatusPending
	}

	if err := tx.Create(&user).Error; err != nil {
		if isDuplicateKeyErr(err) {
			if email != "" {
				return accountError(ErrIdentifierExists, "该邮箱已存在，请登录后手动绑定 OAuth 账号")
			}
			user.Username = fmt.Sprintf("%s_%s", username, newID()[:8])
			if err := tx.Create(&user).Error; err != nil {
				return fmt.Errorf("create user from oauth (username retry): %w", err)
			}
		} else {
			return fmt.Errorf("create user from oauth: %w", err)
		}
	}

	// Create OAuth binding
	oauthAccount := &OAuthAccount{
		ID:        newID(),
		TenantID:  tenantID,
		UserID:    user.ID,
		Provider:  req.Provider,
		Subject:   userInfo.Subject,
		Email:     userInfo.Email,
		Name:      userInfo.Name,
		AvatarURL: userInfo.AvatarURL,
	}
	if err := tx.Create(oauthAccount).Error; err != nil {
		return fmt.Errorf("create oauth account: %w", err)
	}

	// Assign default role
	if err := s.m.auth.assignDefaultRole(ctx, tx, &user); err != nil {
		return err
	}
	if s.m.phoneBindingRequired(&user) {
		*result = &AuthResult{
			User:                 user.toInfo(nil, nil),
			PhoneBindingRequired: true,
			TokenType:            "Bearer",
		}
		return nil
	}

	// Issue tokens
	roles, err := s.m.auth.loadRoleCodes(ctx, tx, user.ID)
	if err != nil {
		return err
	}
	// Enforce admin MFA: require TOTP, or fall back to email/SMS verification
	if s.m.cfg.AccountPolicy.RequireAdminMFA && userHasAdminRole(roles) && !s.m.mfa.HasTOTP(ctx, tenantID, user.ID) {
		if r := s.m.auth.adminMFAFallback(ctx, tx, &user, req.IP); r != nil {
			*result = r
			return nil
		}
		return accountError(ErrPermissionDenied, "需要先配置 MFA 后才能登录")
	}
	*result, err = s.m.auth.issueTokens(ctx, tx, &user, roles, req.DeviceID, req.IP, req.UserAgent, "", "", "")
	return err
}

// bindOAuthToUser handles the case where an OAuth user has the same email as an existing user.
func (s *OAuthService) bindOAuthToUser(tx *gorm.DB, tenantID string, user *User, provider string, userInfo *OAuthUserInfo) error {
	oa := &OAuthAccount{
		ID:        newID(),
		TenantID:  tenantID,
		UserID:    user.ID,
		Provider:  provider,
		Subject:   userInfo.Subject,
		Email:     userInfo.Email,
		Name:      userInfo.Name,
		AvatarURL: userInfo.AvatarURL,
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "provider"}, {Name: "subject"}},
		DoUpdates: clause.AssignmentColumns([]string{"user_id", "email", "name", "avatar_url", "updated_at"}),
	}).Create(oa).Error
}

func (s *OAuthService) generateOAuthUsername(ctx context.Context, tenantID, provider, subject string) string {
	base := fmt.Sprintf("%s_%s", provider, strings.ReplaceAll(subject, "-", ""))
	if len(base) > 60 {
		base = base[:60]
	}
	// Append suffix if needed to avoid collision
	var count int64
	s.m.db.WithContext(ctx).Model(&User{}).
		Where("tenant_id = ? AND username = ?", tenantID, base).
		Count(&count)
	if count == 0 {
		return base
	}
	return fmt.Sprintf("%s_%d", base, count+1)
}

func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}

func isDuplicateKeyErr(err error) bool {
	return strings.Contains(err.Error(), "Duplicate entry") ||
		strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "unique constraint")
}
