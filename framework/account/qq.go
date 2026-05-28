package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/xxzhwl/gaia"
)

// QQProvider implements OAuthProvider for QQ OAuth 2.0 login.
type QQProvider struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	tokenURL     string
	openidURL    string
	userURL      string
}

// QQProviderOption configures optional fields of QQProvider.
type QQProviderOption func(*QQProvider)

// WithQQHTTPClient sets a custom HTTP client.
func WithQQHTTPClient(c *http.Client) QQProviderOption {
	return func(p *QQProvider) { p.httpClient = c }
}

// WithQQTokenURL sets a custom token exchange URL.
func WithQQTokenURL(url string) QQProviderOption {
	return func(p *QQProvider) { p.tokenURL = url }
}

// WithQQOpenIDURL sets a custom openid URL.
func WithQQOpenIDURL(url string) QQProviderOption {
	return func(p *QQProvider) { p.openidURL = url }
}

// WithQQUserURL sets a custom user info URL.
func WithQQUserURL(url string) QQProviderOption {
	return func(p *QQProvider) { p.userURL = url }
}

// NewQQProvider creates a new QQ OAuth provider.
func NewQQProvider(clientID, clientSecret string, opts ...QQProviderOption) *QQProvider {
	p := &QQProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		tokenURL:  gaia.GetSafeConfStringWithDefault("Account.OAuth.QQ.TokenURL", "https://graph.qq.com/oauth2.0/token"),
		openidURL: gaia.GetSafeConfStringWithDefault("Account.OAuth.QQ.OpenIDURL", "https://graph.qq.com/oauth2.0/me"),
		userURL:   gaia.GetSafeConfStringWithDefault("Account.OAuth.QQ.UserURL", "https://graph.qq.com/user/get_user_info"),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *QQProvider) Name() string { return "qq" }

type qqTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        int    `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

type qqOpenIDResponse struct {
	ClientID string `json:"client_id"`
	OpenID   string `json:"openid"`
}

type qqUserResponse struct {
	Ret       int    `json:"ret"`
	Msg       string `json:"msg"`
	Nickname  string `json:"nickname"`
	Gender    string `json:"gender"`
	Figure50  string `json:"figureurl_qq_1"`
	Figure100 string `json:"figureurl_qq_2"`
}

// ExchangeCode exchanges an authorization code for QQ access tokens.
func (p *QQProvider) ExchangeCode(ctx context.Context, req OAuthExchangeRequest) (*OAuthToken, error) {
	if req.State == "" {
		return nil, fmt.Errorf("qq oauth: state is required for CSRF protection")
	}
	u := fmt.Sprintf("%s?grant_type=authorization_code&client_id=%s&client_secret=%s&code=%s&redirect_uri=%s&fmt=json",
		p.tokenURL, p.clientID, p.clientSecret, req.Code, req.RedirectURI)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("qq token request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("qq token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("qq token response read: %w", err)
	}

	var tr qqTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("qq token response parse: %w", err)
	}
	if tr.Error != 0 {
		return nil, fmt.Errorf("qq oauth error: %d: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("qq oauth: empty access token")
	}

	return &OAuthToken{
		AccessToken:  tr.AccessToken,
		ExpiresIn:    tr.ExpiresIn,
		RefreshToken: tr.RefreshToken,
	}, nil
}

// GetUserInfo fetches user profile from QQ. Requires two calls: get openid, then get user info.
func (p *QQProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUserInfo, error) {
	openID, err := p.fetchOpenID(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}

	user, err := p.fetchUserInfo(ctx, token.AccessToken, openID)
	if err != nil {
		return nil, err
	}

	return &OAuthUserInfo{
		Subject:   openID,
		Email:     "",
		Name:      user.Nickname,
		AvatarURL: user.Figure100,
	}, nil
}

func (p *QQProvider) fetchOpenID(ctx context.Context, accessToken string) (string, error) {
	u := fmt.Sprintf("%s?access_token=%s&fmt=json", p.openidURL, accessToken)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("qq openid request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("qq openid request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("qq openid response read: %w", err)
	}

	var oid qqOpenIDResponse
	if err := json.Unmarshal(body, &oid); err != nil {
		return "", fmt.Errorf("qq openid response parse: %w", err)
	}
	if oid.OpenID == "" {
		return "", fmt.Errorf("qq oauth: empty openid")
	}
	return oid.OpenID, nil
}

func (p *QQProvider) fetchUserInfo(ctx context.Context, accessToken, openID string) (*qqUserResponse, error) {
	u := fmt.Sprintf("%s?access_token=%s&oauth_consumer_key=%s&openid=%s",
		p.userURL, accessToken, p.clientID, openID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("qq userinfo request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("qq userinfo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("qq userinfo response read: %w", err)
	}

	var user qqUserResponse
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("qq userinfo response parse: %w", err)
	}
	if user.Ret != 0 {
		return nil, fmt.Errorf("qq userinfo error: %d: %s", user.Ret, user.Msg)
	}
	return &user, nil
}
