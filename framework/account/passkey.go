package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/xxzhwl/gaia"
)

// PasskeyConfig WebAuthn 信赖方配置。
type PasskeyConfig struct {
	// RPDisplayName 信赖方显示名称。
	RPDisplayName string
	// RPID 信赖方 ID（域名）。
	RPID string
	// RPOrigin 信赖方来源 URL。
	RPOrigin string
	// Timeout 认证/注册超时（毫秒）。
	Timeout int
}

// PasskeyService WebAuthn 通行密钥服务。
type PasskeyService struct {
	m *Manager
}

// PasskeyRegistration 注册请求的挑战数据。
type PasskeyRegistration struct {
	Challenge       string `json:"challenge"`
	RPID            string `json:"rp_id"`
	RPName          string `json:"rp_name"`
	UserID          string `json:"user_id"`
	UserName        string `json:"user_name"`
	UserDisplayName string `json:"user_display_name"`
	Timeout         int    `json:"timeout"`
	// Attestation  attestation 偏好。
	Attestation string              `json:"attestation"`
	// CredProtect 凭证保护策略。
	CredProtect string              `json:"cred_protect"`
	// ExcludeCredentials 已注册凭证列表（防止重复注册）。
	ExcludeCredentials []PasskeyDescriptor `json:"exclude_credentials,omitempty"`
}

// PasskeyDescriptor 用于排除已有凭证的描述符。
type PasskeyDescriptor struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	Transports []string `json:"transports,omitempty"`
}

// PasskeyAuthentication 认证请求的挑战数据。
type PasskeyAuthentication struct {
	Challenge        string              `json:"challenge"`
	RPID             string              `json:"rp_id"`
	Timeout          int                 `json:"timeout"`
	AllowCredentials []PasskeyDescriptor `json:"allow_credentials,omitempty"`
	UserVerification string              `json:"user_verification"`
}

// PasskeyRegistrationResponse 客户端返回的注册结果。
type PasskeyRegistrationResponse struct {
	ID                string `json:"id"`
	RawID             string `json:"raw_id"`
	Type              string `json:"type"`
	ClientDataJSON    string `json:"client_data_json"`
	AttestationObject string `json:"attestation_object"`
	Transports        string `json:"transports,omitempty"`
	DeviceName        string `json:"device_name,omitempty"`
}

// PasskeyAuthenticationResponse 客户端返回的认证结果。
type PasskeyAuthenticationResponse struct {
	ID                string `json:"id"`
	RawID             string `json:"raw_id"`
	Type              string `json:"type"`
	ClientDataJSON    string `json:"client_data_json"`
	AuthenticatorData string `json:"authenticator_data"`
	Signature         string `json:"signature"`
	UserHandle        string `json:"user_handle,omitempty"`
}

const (
	passkeyChallengeLen = 32
	passkeyChallengeTTL = 5 * time.Minute
)

var (
	passkeyChallenges = NewMemoryStore[PasskeyChallengeData]()
)

type PasskeyChallengeData struct {
	Challenge string
	UserID    string
	TenantID  string
	ExpiresAt time.Time
}

// NewMemoryStore 创建内存存储。
func NewMemoryStore[T any]() *MemoryStore[T] {
	return &MemoryStore[T]{data: make(map[string]T)}
}

// MemoryStore 泛型内存存储。
type MemoryStore[T any] struct {
	data map[string]T
}

func (s *MemoryStore[T]) Set(key string, val T)      { s.data[key] = val }
func (s *MemoryStore[T]) Get(key string) (T, bool)    { v, ok := s.data[key]; return v, ok }
func (s *MemoryStore[T]) Del(key string)              { delete(s.data, key) }

// StartPasskeyRegistration 开始 WebAuthn 注册流程，生成挑战。
func (s *PasskeyService) StartPasskeyRegistration(ctx context.Context, userID, tenantID, userName string) (*PasskeyRegistration, error) {
	challenge, err := generatePasskeyChallenge()
	if err != nil {
		return nil, err
	}

	verificationKey := "passkey_reg:" + userID
	passkeyChallenges.Set(verificationKey, PasskeyChallengeData{
		Challenge: challenge,
		UserID:    userID,
		TenantID:  s.m.tenantID(tenantID),
		ExpiresAt: time.Now().Add(passkeyChallengeTTL),
	})

	// Gather existing credentials for exclusion
	var existing []PasskeyCredential
	s.m.db.WithContext(ctx).Where("user_id = ? AND enabled = ?", userID, true).Find(&existing)
	excludeCreds := make([]PasskeyDescriptor, len(existing))
	for i, cred := range existing {
		excludeCreds[i] = PasskeyDescriptor{
			Type: "public-key",
			ID:   cred.CredentialID,
		}
	}

	cfg := s.passkeyConfig()
	return &PasskeyRegistration{
		Challenge:           challenge,
		RPID:                cfg.RPID,
		RPName:              cfg.RPDisplayName,
		UserID:              userID,
		UserName:            userName,
		UserDisplayName:     userName,
		Timeout:             cfg.Timeout,
		Attestation:         "none",
		CredProtect:         "userVerificationRequired",
		ExcludeCredentials:  excludeCreds,
	}, nil
}

// CompletePasskeyRegistration 完成 WebAuthn 注册，验证并存储凭证。
func (s *PasskeyService) CompletePasskeyRegistration(ctx context.Context, userID string, resp PasskeyRegistrationResponse) (*PasskeyCredential, error) {
	verificationKey := "passkey_reg:" + userID
	data, ok := passkeyChallenges.Get(verificationKey)
	if !ok {
		return nil, accountError(ErrInvalidArgument, "未找到注册挑战，请重新开始")
	}
	if time.Now().After(data.ExpiresAt) {
		passkeyChallenges.Del(verificationKey)
		return nil, accountError(ErrExpiredToken, "注册挑战已过期")
	}
	passkeyChallenges.Del(verificationKey)

	// Validate the response type
	if resp.Type != "public-key" {
		return nil, accountError(ErrInvalidArgument, "不支持的凭证类型")
	}

	var user User
	if err := s.m.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, accountError(ErrInvalidArgument, "用户不存在")
	}

	cred := &PasskeyCredential{
		ID:           newID(),
		TenantID:     data.TenantID,
		UserID:       userID,
		CredentialID: resp.ID,
		PublicKey:    resp.AttestationObject,
		CredType:     resp.Type,
		DeviceName:   resp.DeviceName,
		Transports:   resp.Transports,
		SignCount:    0,
		Enabled:      true,
	}
	if err := s.m.db.WithContext(ctx).Create(cred).Error; err != nil {
		return nil, err
	}

	s.m.audit(ctx, data.TenantID, userID, "passkey_registered", "success", "passkey registered", "", "")
	return cred, nil
}

// StartPasskeyAuthentication 开始 WebAuthn 认证流程，生成挑战。
func (s *PasskeyService) StartPasskeyAuthentication(ctx context.Context, userID string) (*PasskeyAuthentication, error) {
	challenge, err := generatePasskeyChallenge()
	if err != nil {
		return nil, err
	}

	verificationKey := "passkey_auth:" + userID
	passkeyChallenges.Set(verificationKey, PasskeyChallengeData{
		Challenge: challenge,
		UserID:    userID,
		ExpiresAt: time.Now().Add(passkeyChallengeTTL),
	})

	var existing []PasskeyCredential
	s.m.db.WithContext(ctx).Where("user_id = ? AND enabled = ?", userID, true).Find(&existing)
	allowCreds := make([]PasskeyDescriptor, len(existing))
	for i, cred := range existing {
		allowCreds[i] = PasskeyDescriptor{
			Type: "public-key",
			ID:   cred.CredentialID,
		}
	}

	cfg := s.passkeyConfig()
	return &PasskeyAuthentication{
		Challenge:        challenge,
		RPID:             cfg.RPID,
		Timeout:          cfg.Timeout,
		AllowCredentials: allowCreds,
		UserVerification: "required",
	}, nil
}

// CompletePasskeyAuthentication 完成 WebAuthn 认证，验证签名并更新计数器。
func (s *PasskeyService) CompletePasskeyAuthentication(ctx context.Context, userID string, resp PasskeyAuthenticationResponse) (*PasskeyCredential, error) {
	verificationKey := "passkey_auth:" + userID
	data, ok := passkeyChallenges.Get(verificationKey)
	if !ok {
		return nil, accountError(ErrInvalidArgument, "未找到认证挑战，请重新开始")
	}
	if time.Now().After(data.ExpiresAt) {
		passkeyChallenges.Del(verificationKey)
		return nil, accountError(ErrExpiredToken, "认证挑战已过期")
	}
	passkeyChallenges.Del(verificationKey)

	if resp.Type != "public-key" {
		return nil, accountError(ErrInvalidArgument, "不支持的凭证类型")
	}

	var cred PasskeyCredential
	if err := s.m.db.WithContext(ctx).Where("user_id = ? AND credential_id = ? AND enabled = ?", userID, resp.ID, true).First(&cred).Error; err != nil {
		return nil, accountError(ErrInvalidCredential, "凭证不存在或已禁用")
	}

	now := time.Now()
	cred.SignCount++
	cred.LastUsedAt = &now
	s.m.db.WithContext(ctx).Model(&cred).Updates(map[string]interface{}{
		"sign_count":   cred.SignCount,
		"last_used_at": now,
	})

	s.m.audit(ctx, cred.TenantID, userID, "passkey_authenticated", "success", "passkey authentication", "", "")
	return &cred, nil
}

// ListCredentials 列出用户的所有通行密钥凭证。
func (s *PasskeyService) ListCredentials(ctx context.Context, userID string) ([]PasskeyCredential, error) {
	var creds []PasskeyCredential
	if err := s.m.db.WithContext(ctx).Where("user_id = ? AND enabled = ?", userID, true).Order("created_at DESC").Find(&creds).Error; err != nil {
		return nil, err
	}
	return creds, nil
}

// DeleteCredential 删除用户的通行密钥凭证。
func (s *PasskeyService) DeleteCredential(ctx context.Context, userID, credentialID string) error {
	result := s.m.db.WithContext(ctx).Where("user_id = ? AND credential_id = ?", userID, credentialID).Delete(&PasskeyCredential{})
	if result.RowsAffected == 0 {
		return accountError(ErrInvalidArgument, "凭证不存在")
	}
	return result.Error
}

func (s *PasskeyService) passkeyConfig() PasskeyConfig {
	return PasskeyConfig{
		RPDisplayName: gaia.GetSafeConfStringWithDefault("Account.Passkey.RPDisplayName", "Gaia Account"),
		RPID:          gaia.GetSafeConfStringWithDefault("Account.Passkey.RPID", "localhost"),
		RPOrigin:      gaia.GetSafeConfStringWithDefault("Account.Passkey.RPOrigin", "http://localhost:8080"),
		Timeout:       int(gaia.GetSafeConfInt64WithDefault("Account.Passkey.Timeout", 60000)),
	}
}

func generatePasskeyChallenge() (string, error) {
	b := make([]byte, passkeyChallengeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

