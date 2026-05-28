package account

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/golang-jwt/jwt/v5"
)

// GoogleProvider implements OAuthProvider for Google OAuth 2.0 / OIDC flow.
type GoogleProvider struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	tokenURL     string
	userURL      string
}

// GoogleProviderOption configures optional fields of GoogleProvider.
type GoogleProviderOption func(*GoogleProvider)

// WithGoogleHTTPClient sets a custom HTTP client.
func WithGoogleHTTPClient(c *http.Client) GoogleProviderOption {
	return func(p *GoogleProvider) { p.httpClient = c }
}

// WithGoogleTokenURL sets a custom token exchange URL.
func WithGoogleTokenURL(url string) GoogleProviderOption {
	return func(p *GoogleProvider) { p.tokenURL = url }
}

// WithGoogleUserURL sets a custom user info URL.
func WithGoogleUserURL(url string) GoogleProviderOption {
	return func(p *GoogleProvider) { p.userURL = url }
}

// NewGoogleProvider creates a new Google OAuth provider.
func NewGoogleProvider(clientID, clientSecret string, opts ...GoogleProviderOption) *GoogleProvider {
	p := &GoogleProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		tokenURL: gaia.GetSafeConfStringWithDefault("Account.OAuth.Google.TokenURL", "https://oauth2.googleapis.com/token"),
		userURL:  gaia.GetSafeConfStringWithDefault("Account.OAuth.Google.UserURL", "https://www.googleapis.com/oauth2/v3/userinfo"),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func (p *GoogleProvider) Name() string { return "google" }

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

type googleUser struct {
	Sub           string `json:"sub"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Locale        string `json:"locale"`
}

// ExchangeCode exchanges an authorization code for Google OAuth tokens.
func (p *GoogleProvider) ExchangeCode(ctx context.Context, req OAuthExchangeRequest) (*OAuthToken, error) {
	if req.State == "" {
		return nil, fmt.Errorf("google oauth: state is required for CSRF protection")
	}
	form := url.Values{
		"code":          {req.Code},
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"redirect_uri":  {req.RedirectURI},
		"grant_type":    {"authorization_code"},
	}
	if req.CodeVerifier != "" {
		form.Set("code_verifier", req.CodeVerifier)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("google token request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("google token response read: %w", err)
	}

	var tr googleTokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("google token response parse: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("google oauth error: %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("google oauth: empty access token")
	}

	return &OAuthToken{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		ExpiresIn:    tr.ExpiresIn,
		RefreshToken: tr.RefreshToken,
		IDToken:      tr.IDToken,
		Nonce:        req.Nonce,
	}, nil
}

// GetUserInfo fetches user profile from Google's userinfo endpoint.
// If an id_token is available, validates it (OIDC) for signature, issuer, audience, and nonce.
func (p *GoogleProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUserInfo, error) {
	// Validate ID token (OIDC) if present
	if token.IDToken != "" {
		if err := p.verifyIDToken(ctx, token.IDToken, token.Nonce); err != nil {
			return nil, fmt.Errorf("google id token validation failed: %w", err)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("google userinfo request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("google userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google userinfo api: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var user googleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("google userinfo response parse: %w", err)
	}

	return &OAuthUserInfo{
		Subject:       user.Sub,
		Email:         user.Email,
		EmailVerified: user.EmailVerified,
		Name:          user.Name,
		AvatarURL:     user.Picture,
	}, nil
}

var (
	googleJWKSOnce sync.Once
	googleJWKSKeys []*jwtVerificationKey
	googleJWKSErr  error
)

type jwtVerificationKey struct {
	kid string
	key any
}

// googleJWKSResponse represents the JWKS response from Google.
type googleJWKSResponse struct {
	Keys []googleJWK `json:"keys"`
}

type googleJWK struct {
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	X5c []string `json:"x5c,omitempty"`
}

// googleIDTokenClaims represents the expected claims in a Google OIDC ID token.
type googleIDTokenClaims struct {
	Nonce string `json:"nonce,omitempty"`
	jwt.RegisteredClaims
}

// verifyIDToken validates the Google OIDC ID token's signature, issuer, audience, and nonce.
// Uses Google's JWKS endpoint to fetch public keys.
func (p *GoogleProvider) verifyIDToken(ctx context.Context, idToken, expectedNonce string) error {
	keys, err := p.fetchAndParseJWKS(ctx)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}

	var claims googleIDTokenClaims
	parsed, err := jwt.ParseWithClaims(idToken, &claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		for _, k := range keys {
			if k.kid == kid {
				return k.key, nil
			}
		}
		return nil, fmt.Errorf("no matching JWK found for kid: %s", kid)
	},
		jwt.WithIssuer("https://accounts.google.com"),
		jwt.WithAudience(p.clientID),
	)
	if err != nil {
		return err
	}
	if !parsed.Valid {
		return fmt.Errorf("invalid id token")
	}
	// Validate nonce if expected
	if expectedNonce != "" && claims.Nonce != "" && claims.Nonce != expectedNonce {
		return fmt.Errorf("id token nonce mismatch")
	}
	return nil
}

func (p *GoogleProvider) fetchAndParseJWKS(ctx context.Context) ([]*jwtVerificationKey, error) {
	googleJWKSOnce.Do(func() {
		jwksURL := gaia.GetSafeConfStringWithDefault("Account.OAuth.Google.JWKSURL", "https://www.googleapis.com/oauth2/v3/certs")
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
		if err != nil {
			googleJWKSErr = fmt.Errorf("jwks request: %w", err)
			return
		}
		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			googleJWKSErr = fmt.Errorf("jwks request: %w", err)
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			googleJWKSErr = fmt.Errorf("jwks response read: %w", err)
			return
		}

		var jwks googleJWKSResponse
		if err := json.Unmarshal(body, &jwks); err != nil {
			googleJWKSErr = fmt.Errorf("jwks parse: %w", err)
			return
		}

		for _, jwk := range jwks.Keys {
			if jwk.Use != "sig" || jwk.Kty != "RSA" {
				continue
			}
			key, err := jwkToPublicKey(jwk)
			if err != nil {
				continue
			}
			googleJWKSKeys = append(googleJWKSKeys, &jwtVerificationKey{kid: jwk.Kid, key: key})
		}
		if len(googleJWKSKeys) == 0 {
			googleJWKSErr = fmt.Errorf("no valid RSA signing keys found in JWKS")
		}
	})
	return googleJWKSKeys, googleJWKSErr
}

func jwkToPublicKey(jwk googleJWK) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, err
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}

