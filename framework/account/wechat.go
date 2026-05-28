package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
)

// WeChatProvider implements OAuthProvider for WeChat (微信开放平台) OAuth 2.0 login.
type WeChatProvider struct {
	appID      string
	appSecret  string
	httpClient *http.Client
	tokenURL   string
	userURL    string
}

// WeChatProviderOption configures optional fields of WeChatProvider.
type WeChatProviderOption func(*WeChatProvider)

// WithWeChatHTTPClient sets a custom HTTP client.
func WithWeChatHTTPClient(c *http.Client) WeChatProviderOption {
	return func(p *WeChatProvider) { p.httpClient = c }
}

// WithWeChatTokenURL sets a custom token exchange URL.
func WithWeChatTokenURL(url string) WeChatProviderOption {
	return func(p *WeChatProvider) { p.tokenURL = url }
}

// WithWeChatUserURL sets a custom user info URL.
func WithWeChatUserURL(url string) WeChatProviderOption {
	return func(p *WeChatProvider) { p.userURL = url }
}

// NewWeChatProvider creates a new WeChat OAuth provider.
// Note: WeChat uses appid and secret (not client_id/client_secret).
func NewWeChatProvider(appID, appSecret string, opts ...WeChatProviderOption) *WeChatProvider {
	p := &WeChatProvider{
		appID:     appID,
		appSecret: appSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		tokenURL: gaia.GetSafeConfStringWithDefault("Account.OAuth.WeChat.TokenURL", "https://api.weixin.qq.com/sns/oauth2/access_token"),
		userURL:  gaia.GetSafeConfStringWithDefault("Account.OAuth.WeChat.UserURL", "https://api.weixin.qq.com/sns/userinfo"),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *WeChatProvider) Name() string { return "wechat" }

type wechatTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid,omitempty"`
	ErrorCode    int    `json:"errcode,omitempty"`
	ErrorMsg     string `json:"errmsg,omitempty"`
}

type wechatUserResponse struct {
	OpenID     string `json:"openid"`
	UnionID    string `json:"unionid,omitempty"`
	Nickname   string `json:"nickname"`
	Sex        int    `json:"sex"`
	HeadImgURL string `json:"headimgurl"`
	ErrorCode  int    `json:"errcode,omitempty"`
	ErrorMsg   string `json:"errmsg,omitempty"`
}

// ExchangeCode exchanges an authorization code for WeChat access tokens.
// WeChat returns openid and optional unionid directly in the token response.
func (p *WeChatProvider) ExchangeCode(ctx context.Context, req OAuthExchangeRequest) (*OAuthToken, error) {
	if req.State == "" {
		return nil, fmt.Errorf("wechat oauth: state is required for CSRF protection")
	}
	u := fmt.Sprintf("%s?appid=%s&secret=%s&code=%s&grant_type=authorization_code",
		p.tokenURL, p.appID, p.appSecret, req.Code)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("wechat token request: %w", err)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wechat token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wechat token response read: %w", err)
	}

	var tr wechatTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("wechat token response parse: %w", err)
	}
	if tr.ErrorCode != 0 {
		return nil, fmt.Errorf("wechat oauth error: %d: %s", tr.ErrorCode, tr.ErrorMsg)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("wechat oauth: empty access token")
	}
	if tr.OpenID == "" {
		return nil, fmt.Errorf("wechat oauth: empty openid")
	}

	return &OAuthToken{
		AccessToken:  tr.AccessToken,
		ExpiresIn:    tr.ExpiresIn,
		RefreshToken: tr.RefreshToken,
		OpenID:       tr.OpenID,
	}, nil
}

// GetUserInfo fetches user profile from WeChat using the openid from the token.
func (p *WeChatProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUserInfo, error) {
	if token.OpenID == "" {
		return nil, fmt.Errorf("wechat oauth: missing openid for userinfo request")
	}
	u := fmt.Sprintf("%s?access_token=%s&openid=%s&lang=zh_CN", p.userURL, token.AccessToken, token.OpenID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("wechat userinfo request: %w", err)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wechat userinfo request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("wechat userinfo response read: %w", err)
	}

	// Try parsing as error first
	var errResp struct {
		ErrorCode int    `json:"errcode"`
		ErrorMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.ErrorCode != 0 {
		return nil, fmt.Errorf("wechat userinfo error: %d: %s", errResp.ErrorCode, errResp.ErrorMsg)
	}

	var user wechatUserResponse
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("wechat userinfo response parse: %w", err)
	}
	if user.OpenID == "" {
		return nil, fmt.Errorf("wechat oauth: empty openid in userinfo response")
	}

	subject := user.OpenID
	if user.UnionID != "" {
		subject = user.UnionID
	}

	// Normalize WeChat headimgurl: replace "/0" (default 132x132) with "/132" (larger)
	avatar := user.HeadImgURL
	if strings.HasSuffix(avatar, "/0") {
		avatar = strings.TrimSuffix(avatar, "/0") + "/132"
	}

	return &OAuthUserInfo{
		Subject:   subject,
		Email:     "",
		Name:      decodeWeChatNickname(user.Nickname),
		AvatarURL: avatar,
	}, nil
}

// decodeWeChatNickname handles WeChat's Unicode escape encoding in nicknames.
// WeChat API returns nicknames with Unicode escapes like \uxxxx for emoji
// and non-ASCII characters when the REST client doesn't handle encoding.
func decodeWeChatNickname(nickname string) string {
	if !strings.Contains(nickname, `\u`) {
		return nickname
	}
	var result strings.Builder
	result.Grow(len(nickname))
	for i := 0; i < len(nickname); {
		if i+5 < len(nickname) && nickname[i] == '\\' && nickname[i+1] == 'u' {
			result.WriteRune(rune(hexToInt(nickname[i+2 : i+6])))
			i += 6
		} else {
			result.WriteByte(nickname[i])
			i++
		}
	}
	return result.String()
}

func hexToInt(h string) int64 {
	var n int64
	for _, c := range h {
		n *= 16
		switch {
		case c >= '0' && c <= '9':
			n += int64(c - '0')
		case c >= 'a' && c <= 'f':
			n += int64(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			n += int64(c - 'A' + 10)
		}
	}
	return n
}
