package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
)

// GitHubProvider implements OAuthProvider for GitHub OAuth flow.
type GitHubProvider struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	tokenURL     string // token exchange endpoint
	userURL      string // user profile endpoint
	emailURL     string // user emails endpoint
}

// GitHubProviderOption configures optional fields of GitHubProvider.
type GitHubProviderOption func(*GitHubProvider)

// WithGitHubHTTPClient sets a custom HTTP client.
func WithGitHubHTTPClient(c *http.Client) GitHubProviderOption {
	return func(p *GitHubProvider) { p.httpClient = c }
}

// WithGitHubTokenURL sets a custom token exchange URL.
func WithGitHubTokenURL(url string) GitHubProviderOption {
	return func(p *GitHubProvider) { p.tokenURL = url }
}

// WithGitHubUserURL sets a custom user info URL.
func WithGitHubUserURL(url string) GitHubProviderOption {
	return func(p *GitHubProvider) { p.userURL = url }
}

// WithGitHubEmailURL sets a custom user emails URL.
func WithGitHubEmailURL(url string) GitHubProviderOption {
	return func(p *GitHubProvider) { p.emailURL = url }
}

// NewGitHubProvider creates a new GitHub OAuth provider.
// URLs default to the configuration keys:
// Account.OAuth.GitHub.TokenURL, Account.OAuth.GitHub.UserURL, Account.OAuth.GitHub.EmailURL.
func NewGitHubProvider(clientID, clientSecret string, opts ...GitHubProviderOption) *GitHubProvider {
	p := &GitHubProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		tokenURL: gaia.GetSafeConfStringWithDefault("Account.OAuth.GitHub.TokenURL", "https://github.com/login/oauth/access_token"),
		userURL:  gaia.GetSafeConfStringWithDefault("Account.OAuth.GitHub.UserURL", "https://api.github.com/user"),
		emailURL: gaia.GetSafeConfStringWithDefault("Account.OAuth.GitHub.EmailURL", "https://api.github.com/user/emails"),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *GitHubProvider) Name() string { return "github" }

// githubTokenResponse maps the GitHub access token response.
type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
	ErrorDesc   string `json:"error_description,omitempty"`
}

// githubUser maps the GitHub /user API response.
type githubUser struct {
	ID        int    `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// githubEmail maps the GitHub /user/emails API response entry.
type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

// ExchangeCode exchanges an authorization code for GitHub access tokens.
func (p *GitHubProvider) ExchangeCode(ctx context.Context, req OAuthExchangeRequest) (*OAuthToken, error) {
	if req.State == "" {
		return nil, fmt.Errorf("github oauth: state is required for CSRF protection")
	}
	form := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {req.Code},
		"redirect_uri":  {req.RedirectURI},
	}
	if req.CodeVerifier != "" {
		form.Set("code_verifier", req.CodeVerifier)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("github token request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github token response read: %w", err)
	}

	var tr githubTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("github token response parse: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("github oauth error: %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("github oauth: empty access token")
	}

	return &OAuthToken{
		AccessToken: tr.AccessToken,
		TokenType:   tr.TokenType,
	}, nil
}

// GetUserInfo fetches the GitHub user profile and primary email.
func (p *GitHubProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUserInfo, error) {
	user, err := p.fetchUser(ctx, token.AccessToken)
	if err != nil {
		return nil, err
	}

	info := &OAuthUserInfo{
		Subject:       fmt.Sprintf("%d", user.ID),
		Email:         user.Email,
		EmailVerified: false,
		Name:          user.Name,
		AvatarURL:     user.AvatarURL,
	}

	// GitHub only includes email in /user if it's public.
	// Fetch primary verified email from /user/emails for non-public emails.
	if info.Email == "" {
		email, verified, err := p.fetchPrimaryEmail(ctx, token.AccessToken)
		if err == nil {
			info.Email = email
			info.EmailVerified = verified
		}
	}

	return info, nil
}

func (p *GitHubProvider) fetchUser(ctx context.Context, accessToken string) (*githubUser, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github user request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Accept", "application/vnd.github+json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github user request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github user api: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("github user response parse: %w", err)
	}
	return &user, nil
}

func (p *GitHubProvider) fetchPrimaryEmail(ctx context.Context, accessToken string) (string, bool, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.emailURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("github email request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Accept", "application/vnd.github+json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return "", false, fmt.Errorf("github email request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("github email api: %s", resp.Status)
	}

	var emails []githubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", false, fmt.Errorf("github email response parse: %w", err)
	}

	for _, e := range emails {
		if e.Primary {
			return e.Email, e.Verified, nil
		}
	}
	// fallback to first email if no primary marked
	if len(emails) > 0 {
		return emails[0].Email, emails[0].Verified, nil
	}
	return "", false, nil
}
