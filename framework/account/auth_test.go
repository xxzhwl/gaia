package account

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

func TestAccessTokenSignAndParse(t *testing.T) {
	cfg := testAuthConfig()
	m := testManager(t, cfg)

	user := &User{
		ID:          "user-123",
		TenantID:    "default",
		Username:    "testuser",
		AuthVersion: 1,
		RolesVersion: 1,
	}

	token, expiresAt, err := m.auth.signAccessToken(user, []string{"user"}, "session-456")
	if err != nil {
		t.Fatalf("signAccessToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if expiresAt.Before(time.Now()) || expiresAt.After(time.Now().Add(30*time.Minute)) {
		t.Fatalf("unexpected expiration: %v", expiresAt)
	}

	// Parse and verify
	claims, err := m.auth.parseAccessToken(token)
	if err != nil {
		t.Fatalf("parseAccessToken failed: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Fatalf("expected user-123, got %s", claims.UserID)
	}
	if claims.SessionID != "session-456" {
		t.Fatalf("expected session-456, got %s", claims.SessionID)
	}
	if claims.AuthVersion != 1 {
		t.Fatalf("expected AuthVersion 1, got %d", claims.AuthVersion)
	}
	if claims.Issuer != "test-issuer" {
		t.Fatalf("expected test-issuer, got %s", claims.Issuer)
	}
}

func TestAccessTokenHMACKID(t *testing.T) {
	cfg := testAuthConfig()
	m := testManager(t, cfg)

	token, _, err := m.auth.signAccessToken(
		&User{ID: "u1", TenantID: "default", Username: "u", AuthVersion: 1, RolesVersion: 1},
		[]string{"user"}, "s1",
	)
	if err != nil {
		t.Fatalf("signAccessToken failed: %v", err)
	}

	// Check kid in header
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsed, _, _ := parser.ParseUnverified(token, &accessClaims{})
	if parsed.Header["kid"] != "hmac-default" {
		t.Fatalf("expected kid hmac-default, got %v", parsed.Header["kid"])
	}
}

func TestAccessTokenWithRoles(t *testing.T) {
	cfg := testAuthConfig()
	m := testManager(t, cfg)

	token, _, err := m.auth.signAccessToken(
		&User{ID: "u1", TenantID: "default", Username: "u", AuthVersion: 1, RolesVersion: 1},
		[]string{"admin", "user"}, "s1",
	)
	if err != nil {
		t.Fatalf("signAccessToken failed: %v", err)
	}

	claims, err := m.auth.parseAccessToken(token)
	if err != nil {
		t.Fatalf("parseAccessToken failed: %v", err)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" {
		t.Fatalf("expected [admin user] roles, got %v", claims.Roles)
	}
}

func TestAccessTokenInvalidSignature(t *testing.T) {
	cfg1 := testAuthConfig()
	m1 := testManager(t, cfg1)

	token, _, _ := m1.auth.signAccessToken(
		&User{ID: "u1", TenantID: "default", Username: "u", AuthVersion: 1, RolesVersion: 1},
		[]string{"user"}, "s1",
	)

	// Try to parse with different secret
	cfg2 := testAuthConfig()
	cfg2.JWT.SecretKey = "a-different-secret-key-that-is-32-bytes!"
	m2 := testManager(t, cfg2)

	_, err := m2.auth.parseAccessToken(token)
	if err == nil {
		t.Fatal("expected parse to fail with different secret")
	}
}

func TestAccessTokenExpired(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			Issuer:    "test-issuer",
		},
	})
	signed, _ := token.SignedString([]byte("this-is-32-bytes-for-test-secret!"))

	cfg := testAuthConfig()
	m := testManager(t, cfg)

	_, err := m.auth.parseAccessToken(signed)
	if err == nil {
		t.Fatal("expected parse to fail for expired token")
	}
}

func TestAccessTokenParseWithWrongIssuer(t *testing.T) {
	cfg := testAuthConfig()
	m := testManager(t, cfg)

	token, _, _ := m.auth.signAccessToken(
		&User{ID: "u1", TenantID: "default", Username: "u", AuthVersion: 1, RolesVersion: 1},
		[]string{}, "s1",
	)

	// Modify issuer
	cfg2 := testAuthConfig()
	cfg2.JWT.Issuer = "wrong-issuer"
	m2 := testManager(t, cfg2)

	_, err := m2.auth.parseAccessToken(token)
	if err == nil {
		t.Fatal("expected parse to fail with wrong issuer")
	}
}

func TestPrincipalCacheKey(t *testing.T) {
	s := &AuthService{}
	claims := &accessClaims{
		TenantID:    "t1",
		UserID:      "u1",
		SessionID:   "s1",
		AuthVersion: 1,
		RolesVersion: 2,
	}
	key := s.principalCacheKey(claims)
	expected := "principal:t1:u1:s1:1:2"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}

func TestPrincipalSessionKey(t *testing.T) {
	s := &AuthService{}
	key := s.principalSessionKey("s1")
	if key != "principal-session:s1" {
		t.Fatalf("expected principal-session:s1, got %s", key)
	}
}

func TestPermissionCacheKey(t *testing.T) {
	key := (&Authorizer{}).permissionCacheKey("t1", "u1", 3)
	expected := "perms:t1:u1:3"
	if key != expected {
		t.Fatalf("expected %s, got %s", expected, key)
	}
}

func TestDenylistCacheKey(t *testing.T) {
	key := denylistCacheKey("jti-123")
	if key != "deny:jti:jti-123" {
		t.Fatalf("expected deny:jti:jti-123, got %s", key)
	}
}

func TestFailureKey(t *testing.T) {
	s := &RiskService{m: &Manager{cfg: Config{DefaultTenantID: "default"}}}
	key := s.failureKey("t1", "user", "u1")
	if key != "risk:fail:t1:user:u1" {
		t.Fatalf("expected risk:fail:t1:user:u1, got %s", key)
	}
}

func TestTenantID(t *testing.T) {
	m := &Manager{cfg: Config{DefaultTenantID: "default"}}
	if id := m.tenantID(""); id != "default" {
		t.Fatalf("expected default, got %s", id)
	}
	if id := m.tenantID("custom-tenant"); id != "custom-tenant" {
		t.Fatalf("expected custom-tenant, got %s", id)
	}
}

// testAuthConfig returns a minimal auth config for testing.
func testAuthConfig() Config {
	return Config{
		Mode:              "test",
		DefaultTenantID:   "default",
		DB:                &gorm.DB{},
		JWT:               JWTConfig{SecretKey: "this-is-32-bytes-for-test-secret!", Issuer: "test-issuer", Audience: []string{"test"}},
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   30 * 24 * time.Hour,
		Password:          PasswordConfig{MinLength: 8, MaxLength: 128, Argon2Time: 1, Argon2Memory: 64 * 1024, Argon2Threads: 1},
		Verification:      defaultVerificationConfig(),
		Risk:              defaultRiskConfig(),
		PermissionCacheTTL: 10 * time.Minute,
		PrincipalCacheMaxTTL: 5 * time.Minute,
		DenylistCacheTTL:  30 * time.Second,
	}
}

// testManager creates a minimal Manager for testing.
func testManager(_ *testing.T, cfg Config) *Manager {
	m := newManager(cfg)
	return m
}
