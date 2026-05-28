package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// IdpService OAuth 2.0 / OIDC 身份提供商服务。
// 支持授权码流程（authorization_code）和客户端凭证流程（client_credentials）。
type IdpService struct {
	m *Manager
}

// IdpClientRequest 注册 OAuth 客户端的请求参数。
type IdpClientRequest struct {
	TenantID          string
	Name              string
	RedirectURIs      []string
	AllowedGrantTypes []string
	Scopes            []string
}

// IdpClientResponse 注册/查询客户端的返回。
type IdpClientResponse struct {
	ID                string   `json:"id"`
	ClientID          string   `json:"client_id"`
	ClientSecret      string   `json:"client_secret,omitempty"`
	Name              string   `json:"name"`
	RedirectURIs      []string `json:"redirect_uris"`
	AllowedGrantTypes []string `json:"allowed_grant_types"`
	Scopes            []string `json:"scopes"`
	Status            string   `json:"status"`
}

// AuthorizeRequest 授权码请求参数（从授权端点解析）。
type AuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	UserID              string
	TenantID            string
	CodeChallenge       string
	CodeChallengeMethod string
}

// TokenRequest 令牌端点请求。
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	ClientSecret string
	CodeVerifier string
	RefreshToken string
	Scope        string
	TenantID     string
	UserID       string // set by caller for client_credentials
	Roles        []string
}

// TokenResponse OAuth 令牌响应。
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// RegisterClient 注册新的 OAuth 客户端。
func (s *IdpService) RegisterClient(ctx context.Context, req IdpClientRequest) (*IdpClientResponse, error) {
	clientID, err := generateClientID()
	if err != nil {
		return nil, err
	}
	clientSecret, err := generateClientSecret()
	if err != nil {
		return nil, err
	}
	redirectURIs, _ := json.Marshal(req.RedirectURIs)
	grantTypes := "authorization_code"
	if len(req.AllowedGrantTypes) > 0 {
		grantTypes = strings.Join(req.AllowedGrantTypes, ",")
	}
	scopes := "openid,profile"
	if len(req.Scopes) > 0 {
		scopes = strings.Join(req.Scopes, ",")
	}
	client := &IdpClient{
		ID:                newID(),
		TenantID:          s.m.tenantID(req.TenantID),
		ClientID:          clientID,
		ClientSecret:      clientSecret,
		Name:              req.Name,
		RedirectURIs:      string(redirectURIs),
		AllowedGrantTypes: grantTypes,
		Scopes:            scopes,
		Status:            "enabled",
	}
	if err := s.m.db.WithContext(ctx).Create(client).Error; err != nil {
		return nil, err
	}
	var uris []string
	json.Unmarshal([]byte(client.RedirectURIs), &uris)
	return &IdpClientResponse{
		ID:           client.ID,
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		Name:         client.Name,
		RedirectURIs: uris,
		Scopes:       strings.Split(client.Scopes, ","),
		Status:       client.Status,
	}, nil
}

// GetClient 查询 OAuth 客户端详情。
func (s *IdpService) GetClient(ctx context.Context, clientID string) (*IdpClientResponse, error) {
	var client IdpClient
	if err := s.m.db.WithContext(ctx).Where("client_id = ?", clientID).First(&client).Error; err != nil {
		return nil, err
	}
	var uris []string
	json.Unmarshal([]byte(client.RedirectURIs), &uris)
	return &IdpClientResponse{
		ID:           client.ID,
		ClientID:     client.ClientID,
		Name:         client.Name,
		RedirectURIs: uris,
		Scopes:       strings.Split(client.Scopes, ","),
		Status:       client.Status,
	}, nil
}

// ListClients 列出租户下的所有 OAuth 客户端。
func (s *IdpService) ListClients(ctx context.Context, tenantID string) ([]IdpClientResponse, error) {
	var clients []IdpClient
	if err := s.m.db.WithContext(ctx).Where("tenant_id = ?", s.m.tenantID(tenantID)).Find(&clients).Error; err != nil {
		return nil, err
	}
	resp := make([]IdpClientResponse, len(clients))
	for i, c := range clients {
		var uris []string
		json.Unmarshal([]byte(c.RedirectURIs), &uris)
		resp[i] = IdpClientResponse{
			ID:           c.ID,
			ClientID:     c.ClientID,
			Name:         c.Name,
			RedirectURIs: uris,
			Scopes:       strings.Split(c.Scopes, ","),
			Status:       c.Status,
		}
	}
	return resp, nil
}

// DeleteClient 删除 OAuth 客户端。
func (s *IdpService) DeleteClient(ctx context.Context, clientID string) error {
	return s.m.db.WithContext(ctx).Where("client_id = ?", clientID).Delete(&IdpClient{}).Error
}

// Authorize 创建授权码（授权码流程的第一步）。
func (s *IdpService) Authorize(ctx context.Context, req AuthorizeRequest) (*AuthorizationCode, error) {
	client, err := s.loadClient(ctx, req.ClientID)
	if err != nil {
		return nil, accountError(ErrInvalidArgument, "客户端不存在")
	}
	if client.Status != "enabled" {
		return nil, accountError(ErrInvalidArgument, "客户端已禁用")
	}
	if !s.hasGrantType(client, "authorization_code") {
		return nil, accountError(ErrInvalidArgument, "客户端不支持授权码流程")
	}
	if !s.matchesRedirectURI(client, req.RedirectURI) {
		return nil, accountError(ErrInvalidArgument, "重定向 URI 不匹配")
	}

	code, err := generateAuthCode()
	if err != nil {
		return nil, err
	}
	authCode := &AuthorizationCode{
		Code:                code,
		TenantID:            s.m.tenantID(req.TenantID),
		ClientID:            req.ClientID,
		UserID:              req.UserID,
		Scopes:              req.Scope,
		RedirectURI:         req.RedirectURI,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Nonce:               req.State,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}
	if err := s.m.db.WithContext(ctx).Create(authCode).Error; err != nil {
		return nil, err
	}
	if err := s.recordAuthorizedApp(ctx, authCode.TenantID, authCode.UserID, authCode.ClientID, authCode.Scopes); err != nil {
		return nil, err
	}
	return authCode, nil
}

// Token 令牌端点，处理授权码兑换和令牌刷新。
func (s *IdpService) Token(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	client, err := s.loadClient(ctx, req.ClientID)
	if err != nil {
		return nil, accountError(ErrInvalidArgument, "客户端不存在")
	}
	if client.Status != "enabled" {
		return nil, accountError(ErrInvalidArgument, "客户端已禁用")
	}

	switch req.GrantType {
	case "authorization_code":
		return s.handleAuthCodeGrant(ctx, client, req)
	case "refresh_token":
		return s.handleRefreshTokenGrant(ctx, client, req)
	case "client_credentials":
		return s.handleClientCredentials(ctx, client, req)
	default:
		return nil, accountError(ErrInvalidArgument, "不支持的 grant_type")
	}
}

// JWKS 返回 IdP 的 JSON Web Key Set 字符串，用于 OIDC 发现。
func (s *IdpService) JWKS(ctx context.Context) (string, error) {
	if s.m.cfg.JWT.KeySet != nil {
		b, err := s.m.cfg.JWT.KeySet.JWKS()
		if err != nil {
			return "{}", err
		}
		return string(b), nil
	}
	return "{}", nil
}

// OpenIDConfig 返回 OIDC 发现配置。
func (s *IdpService) OpenIDConfig(ctx context.Context, issuer string) map[string]interface{} {
	return map[string]interface{}{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth/authorize",
		"token_endpoint":                        issuer + "/oauth/token",
		"jwks_uri":                              issuer + "/oauth/certs",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token", "client_credentials"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"ES256", "RS256", "HS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
	}
}

func (s *IdpService) handleAuthCodeGrant(ctx context.Context, client *IdpClient, req TokenRequest) (*TokenResponse, error) {
	if !s.verifyClientSecret(client, req.ClientSecret) {
		return nil, accountError(ErrInvalidCredential, "客户端密钥错误")
	}

	var authCode AuthorizationCode
	if err := s.m.db.WithContext(ctx).Where("code = ?", req.Code).First(&authCode).Error; err != nil {
		return nil, accountError(ErrInvalidArgument, "授权码无效")
	}
	if authCode.ExpiresAt.Before(time.Now()) {
		s.m.db.WithContext(ctx).Delete(&authCode)
		return nil, accountError(ErrExpiredToken, "授权码已过期")
	}
	if authCode.ClientID != req.ClientID {
		return nil, accountError(ErrInvalidArgument, "授权码与客户端不匹配")
	}
	if authCode.RedirectURI != req.RedirectURI {
		return nil, accountError(ErrInvalidArgument, "重定向 URI 不匹配")
	}

	if authCode.CodeChallenge != "" {
		if req.CodeVerifier == "" {
			return nil, accountError(ErrInvalidArgument, "需要 code_verifier")
		}
		if !verifyPKCE(req.CodeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
			return nil, accountError(ErrInvalidCredential, "PKCE 验证失败")
		}
	}

	s.m.db.WithContext(ctx).Delete(&authCode)

	return s.issueTokens(ctx, authCode.TenantID, authCode.UserID, authCode.ClientID, authCode.Scopes, nil)
}

func (s *IdpService) handleRefreshTokenGrant(ctx context.Context, client *IdpClient, req TokenRequest) (*TokenResponse, error) {
	if !s.verifyClientSecret(client, req.ClientSecret) {
		return nil, accountError(ErrInvalidCredential, "客户端密钥错误")
	}
	return nil, accountError(ErrInvalidArgument, "refresh_token grant 请使用 AuthService.Refresh")
}

func (s *IdpService) handleClientCredentials(ctx context.Context, client *IdpClient, req TokenRequest) (*TokenResponse, error) {
	if !s.hasGrantType(client, "client_credentials") {
		return nil, accountError(ErrInvalidArgument, "客户端不支持 client_credentials")
	}
	if !s.verifyClientSecret(client, req.ClientSecret) {
		return nil, accountError(ErrInvalidCredential, "客户端密钥错误")
	}
	userID := req.UserID
	if userID == "" {
		userID = "svc:" + client.ClientID
	}
	return s.issueTokens(ctx, s.m.tenantID(req.TenantID), userID, client.ClientID, client.Scopes, req.Roles)
}

func (s *IdpService) issueTokens(ctx context.Context, tenantID, userID, clientID, scopes string, roles []string) (*TokenResponse, error) {
	user := &User{
		ID:           userID,
		TenantID:     tenantID,
		Username:     userID,
		AuthVersion:  1,
		RolesVersion: 1,
	}
	if roles == nil {
		roles = []string{}
	}
	token, _, err := s.m.auth.signAccessToken(user, roles, clientID)
	if err != nil {
		return nil, err
	}

	resp := &TokenResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.m.cfg.AccessTokenTTL.Seconds()),
	}

	if strings.Contains(scopes, "openid") {
		idToken, err := s.signIDToken(ctx, tenantID, userID, clientID)
		if err == nil {
			resp.IDToken = idToken
		}
	}

	return resp, nil
}

func (s *IdpService) signIDToken(ctx context.Context, tenantID, userID, clientID string) (string, error) {
	claims := jwt.MapClaims{
		"iss":       s.m.cfg.JWT.Issuer,
		"sub":       userID,
		"aud":       clientID,
		"exp":       jwt.NewNumericDate(time.Now().Add(s.m.cfg.AccessTokenTTL)),
		"iat":       jwt.NewNumericDate(time.Now()),
		"auth_time": jwt.NewNumericDate(time.Now()),
	}
	if s.m.cfg.JWT.KeySet != nil {
		return s.m.cfg.JWT.KeySet.Sign(claims)
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(s.m.cfg.JWT.SecretKey))
}

func (s *IdpService) loadClient(ctx context.Context, clientID string) (*IdpClient, error) {
	var client IdpClient
	if err := s.m.db.WithContext(ctx).Where("client_id = ?", clientID).First(&client).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

func (s *IdpService) hasGrantType(client *IdpClient, grantType string) bool {
	for _, gt := range strings.Split(client.AllowedGrantTypes, ",") {
		if strings.TrimSpace(gt) == grantType {
			return true
		}
	}
	return false
}

func (s *IdpService) matchesRedirectURI(client *IdpClient, uri string) bool {
	var uris []string
	if err := json.Unmarshal([]byte(client.RedirectURIs), &uris); err != nil {
		return false
	}
	for _, u := range uris {
		if u == uri {
			return true
		}
	}
	return false
}

func (s *IdpService) verifyClientSecret(client *IdpClient, secret string) bool {
	return client.ClientSecret == secret
}

func generateClientID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateClientSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateAuthCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func verifyPKCE(verifier, challenge, method string) bool {
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])
	if method == "S256" {
		return expected == challenge
	}
	return verifier == challenge
}
