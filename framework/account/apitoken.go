package account

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const personalAccessTokenPrefix = "gaia_pat_"

// PersonalAccessToken 是用户自助创建的长期 API 访问令牌。
type PersonalAccessToken struct {
	ID          string     `json:"id" gorm:"size:36;primaryKey"`
	TenantID    string     `json:"tenant_id" gorm:"size:64;not null;index:idx_acct_pat_user_status,priority:1"`
	UserID      string     `json:"user_id" gorm:"size:36;not null;index:idx_acct_pat_user_status,priority:2"`
	Name        string     `json:"name" gorm:"size:120;not null"`
	TokenPrefix string     `json:"token_prefix" gorm:"size:32;not null;index"`
	TokenHash   string     `json:"-" gorm:"size:64;not null;uniqueIndex:uniq_acct_pat_hash"`
	Scopes      string     `json:"scopes" gorm:"size:512"`
	Status      string     `json:"status" gorm:"size:24;not null;default:active;index:idx_acct_pat_user_status,priority:3"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (PersonalAccessToken) TableName() string { return "acct_api_tokens" }

// APITokenService 提供个人访问令牌创建、查询、撤销和校验能力。
type APITokenService struct {
	m *Manager
}

type CreateAPITokenRequest struct {
	TenantID  string
	UserID    string
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

type APITokenInfo struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	UserID      string     `json:"user_id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	Scopes      []string   `json:"scopes"`
	Status      string     `json:"status"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CreateAPITokenResult struct {
	APITokenInfo
	Token string `json:"token"`
}

func (s *APITokenService) Create(ctx context.Context, req CreateAPITokenRequest) (*CreateAPITokenResult, error) {
	if strings.TrimSpace(req.UserID) == "" {
		return nil, accountError(ErrInvalidArgument, "user_id 不能为空")
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, accountError(ErrInvalidArgument, "令牌名称不能为空")
	}
	tenantID := s.m.tenantID(req.TenantID)
	var user User
	if err := s.m.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", req.UserID, tenantID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, accountError(ErrInvalidArgument, "用户不存在")
		}
		return nil, err
	}
	tokenSecret, err := randomToken()
	if err != nil {
		return nil, err
	}
	token := personalAccessTokenPrefix + tokenSecret
	prefix := token
	if len(prefix) > 24 {
		prefix = prefix[:24]
	}
	row := &PersonalAccessToken{
		ID:          newID(),
		TenantID:    tenantID,
		UserID:      req.UserID,
		Name:        strings.TrimSpace(req.Name),
		TokenPrefix: prefix,
		TokenHash:   tokenHash(token),
		Scopes:      joinScopes(req.Scopes),
		Status:      "active",
		ExpiresAt:   req.ExpiresAt,
	}
	if err := s.m.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, err
	}
	info := apiTokenInfo(*row)
	s.m.audit(ctx, tenantID, req.UserID, "api_token_create", "success", "token: "+row.ID, "", "")
	return &CreateAPITokenResult{APITokenInfo: info, Token: token}, nil
}

func (s *APITokenService) List(ctx context.Context, tenantID, userID string) ([]APITokenInfo, error) {
	var rows []PersonalAccessToken
	if err := s.m.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", s.m.tenantID(tenantID), userID).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]APITokenInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiTokenInfo(row))
	}
	return out, nil
}

func (s *APITokenService) Revoke(ctx context.Context, tenantID, userID, tokenID string) error {
	now := time.Now()
	res := s.m.db.WithContext(ctx).Model(&PersonalAccessToken{}).
		Where("id = ? AND tenant_id = ? AND user_id = ? AND status = ?", tokenID, s.m.tenantID(tenantID), userID, "active").
		Updates(map[string]any{"status": "revoked", "revoked_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return accountError(ErrInvalidArgument, "API token 不存在或已撤销")
	}
	s.m.audit(ctx, tenantID, userID, "api_token_revoke", "success", "token: "+tokenID, "", "")
	return nil
}

// Validate 校验个人访问令牌并返回可用于权限判断的 Principal。
func (s *APITokenService) Validate(ctx context.Context, token string) (*Principal, error) {
	if !strings.HasPrefix(token, personalAccessTokenPrefix) {
		return nil, accountError(ErrInvalidToken, "API token 格式无效")
	}
	var row PersonalAccessToken
	if err := s.m.db.WithContext(ctx).Where("token_hash = ?", tokenHash(token)).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, accountError(ErrInvalidToken, "API token 无效")
		}
		return nil, err
	}
	if row.Status != "active" || row.RevokedAt != nil {
		return nil, accountError(ErrRevokedToken, "API token 已撤销")
	}
	if row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()) {
		return nil, accountError(ErrExpiredToken, "API token 已过期")
	}
	var user User
	if err := s.m.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", row.UserID, row.TenantID).First(&user).Error; err != nil {
		return nil, accountError(ErrInvalidToken, "API token 用户不存在")
	}
	if user.Status != UserStatusNormal {
		return nil, accountError(ErrPermissionDenied, "账号不可用")
	}
	roles, err := s.m.auth.loadRoleCodes(ctx, s.m.db.WithContext(ctx), row.UserID)
	if err != nil {
		return nil, fmt.Errorf("load token roles: %w", err)
	}
	now := time.Now()
	_ = s.m.db.WithContext(ctx).Model(&row).Update("last_used_at", now).Error
	return &Principal{
		TenantID:     row.TenantID,
		UserID:       row.UserID,
		Username:     user.Username,
		APITokenID:   row.ID,
		Scopes:       splitScopes(row.Scopes),
		Roles:        roles,
		AuthVersion:  user.AuthVersion,
		RolesVersion: user.RolesVersion,
	}, nil
}

func apiTokenInfo(row PersonalAccessToken) APITokenInfo {
	return APITokenInfo{
		ID:          row.ID,
		TenantID:    row.TenantID,
		UserID:      row.UserID,
		Name:        row.Name,
		TokenPrefix: row.TokenPrefix,
		Scopes:      splitScopes(row.Scopes),
		Status:      row.Status,
		ExpiresAt:   row.ExpiresAt,
		LastUsedAt:  row.LastUsedAt,
		CreatedAt:   row.CreatedAt,
	}
}

func joinScopes(scopes []string) string {
	clean := make([]string, 0, len(scopes))
	seen := map[string]struct{}{}
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		clean = append(clean, scope)
	}
	return strings.Join(clean, ",")
}

func splitScopes(scopes string) []string {
	if strings.TrimSpace(scopes) == "" {
		return []string{}
	}
	parts := strings.Split(scopes, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
