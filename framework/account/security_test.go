package account

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestPasswordHashVerify(t *testing.T) {
	// Use low-cost parameters for fast tests
	cfg := PasswordConfig{Argon2Time: 1, Argon2Memory: 64 * 1024, Argon2Threads: 1}
	hash, err := hashPassword("a-strong-password-123", cfg)
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !verifyPassword("a-strong-password-123", hash) {
		t.Fatal("expected password to verify")
	}
	if verifyPassword("wrong-password", hash) {
		t.Fatal("expected wrong password to fail")
	}
	if verifyPassword("a-strong-password-123", "$invalid$hash") {
		t.Fatal("expected invalid hash format to fail")
	}
}

func TestPasswordHashDeterministic(t *testing.T) {
	cfg := PasswordConfig{Argon2Time: 1, Argon2Memory: 64 * 1024, Argon2Threads: 1}
	// Same password produces different hashes (different salt each time)
	h1, _ := hashPassword("same-password", cfg)
	h2, _ := hashPassword("same-password", cfg)
	if h1 == h2 {
		t.Fatal("expected different salts to produce different hashes")
	}
}

func TestVerifyPasswordWithModifiedParams(t *testing.T) {
	cfg := PasswordConfig{Argon2Time: 1, Argon2Memory: 64 * 1024, Argon2Threads: 1}
	hash, _ := hashPassword("test-password", cfg)
	// Tamper with hash
	if verifyPassword("test-password", hash[:len(hash)-5]+"AAAAA") {
		t.Fatal("expected tampered hash to fail")
	}
}

func TestTokenHashStable(t *testing.T) {
	first := tokenHash("token123")
	second := tokenHash("token123")
	if first == "" || first != second {
		t.Fatalf("expected stable token hash, got %q and %q", first, second)
	}
	// Different inputs produce different hashes
	other := tokenHash("token456")
	if first == other {
		t.Fatal("expected different inputs to produce different hashes")
	}
	// SHA-256 hex hash is 64 characters
	if len(first) != 64 {
		t.Fatalf("expected 64-char hash, got %d", len(first))
	}
}

func TestNeedsPasswordUpgrade(t *testing.T) {
	cfg := PasswordConfig{Argon2Time: 3, Argon2Memory: 64 * 1024, Argon2Threads: 2}
	hash, _ := hashPassword("test-password", cfg)
	// Same params should not need upgrade
	if needsPasswordUpgrade(hash, cfg) {
		t.Fatal("expected no upgrade needed with same params")
	}
	// Different params should need upgrade
	cfg2 := PasswordConfig{Argon2Time: 4, Argon2Memory: 64 * 1024, Argon2Threads: 2}
	if !needsPasswordUpgrade(hash, cfg2) {
		t.Fatal("expected upgrade needed with different params")
	}
	// Unknown format should trigger upgrade
	if !needsPasswordUpgrade("$2a$10$invalidformat", cfg) {
		t.Fatal("expected unknown format to trigger upgrade")
	}
	// Invalid hash should trigger upgrade
	if !needsPasswordUpgrade("not-a-hash", cfg) {
		t.Fatal("expected invalid hash to trigger upgrade")
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		cfg      PasswordConfig
		wantErr  bool
	}{
		{"valid long password", "a-strong-password-123", PasswordConfig{MinLength: 12, MaxLength: 128}, false},
		{"too short", "short", PasswordConfig{MinLength: 12, MaxLength: 128}, true},
		{"too long", strings.Repeat("a", 200), PasswordConfig{MinLength: 8, MaxLength: 128}, true},
		{"common weak password", "password123", PasswordConfig{MinLength: 8, MaxLength: 128}, true},
		{"common weak password 2", "123456789012", PasswordConfig{MinLength: 8, MaxLength: 128}, true},
		{"edge case min length", "Abcdefg1", PasswordConfig{MinLength: 8, MaxLength: 128}, false},
		{"edge case max length", strings.Repeat("a", 128), PasswordConfig{MinLength: 8, MaxLength: 128}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.password, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePassword() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{" User@Example.COM ", "user@example.com"},
		{"ALLCAPS@DOMAIN.COM", "allcaps@domain.com"},
		{"already-lower@example.com", "already-lower@example.com"},
		{"", ""},
		{"  spaced@example.com  ", "spaced@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeEmail(tt.input); got != tt.want {
				t.Errorf("normalizeEmail() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{" 13800138000 ", "13800138000"},
		{"+86 138-0013-8000", "+86 138-0013-8000"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizePhone(tt.input); got != tt.want {
				t.Errorf("normalizePhone() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNullableString(t *testing.T) {
	if nullableString("") != nil {
		t.Fatal("expected empty string to become nil")
	}
	if nullableString("  ") != nil {
		t.Fatal("expected whitespace string to become nil")
	}
	v := nullableString(" user@example.com ")
	if v == nil || *v != "user@example.com" {
		t.Fatalf("unexpected nullable string value: %#v", v)
	}
}

func TestStringValue(t *testing.T) {
	if stringValue(nil) != "" {
		t.Fatal("expected nil to become empty string")
	}
	s := "hello"
	if stringValue(&s) != "hello" {
		t.Fatal("expected pointer to return value")
	}
}

func TestRandomToken(t *testing.T) {
	token, err := randomToken()
	if err != nil {
		t.Fatalf("randomToken failed: %v", err)
	}
	if len(token) == 0 {
		t.Fatal("expected non-empty token")
	}
	// Multiple calls should produce different tokens
	token2, _ := randomToken()
	if token == token2 {
		t.Fatal("expected different tokens")
	}
}

func TestHashUserAgent(t *testing.T) {
	if hashUserAgent("") != "" {
		t.Fatal("expected empty UA to produce empty hash")
	}
	hash := hashUserAgent("Mozilla/5.0 ...")
	if len(hash) != 64 {
		t.Fatalf("expected 64-char hash, got %d", len(hash))
	}
	if hashUserAgent("Mozilla/5.0 ...") != hashUserAgent("Mozilla/5.0 ...") {
		t.Fatal("expected same UA to produce same hash")
	}
}

func TestIsEmailIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"user@example.com", true},
		{"test@test.co.uk", true},
		{"not-an-email", false},
		{"@domain.com", false},
		{"", false},
		{"phone-number", false},
		{"+8613800138000", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isEmailIdentifier(tt.input); got != tt.want {
				t.Errorf("isEmailIdentifier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateIdentifier(t *testing.T) {
	if err := validateIdentifier(RegisterRequest{Username: "testuser"}); err != nil {
		t.Fatalf("expected valid identifier, got: %v", err)
	}
	if err := validateIdentifier(RegisterRequest{Username: ""}); err == nil {
		t.Fatal("expected empty username to fail")
	}
	if err := validateIdentifier(RegisterRequest{Username: "  "}); err == nil {
		t.Fatal("expected whitespace username to fail")
	}
}

func TestGenerateCode(t *testing.T) {
	code, err := generateCode(6)
	if err != nil {
		t.Fatalf("generateCode failed: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6 digits, got %d", len(code))
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Fatalf("expected digit, got %c", c)
		}
	}
	// Default length
	code2, _ := generateCode(0)
	if len(code2) != 6 {
		t.Fatalf("expected 6 digits, got %d", len(code2))
	}
	// Different invocations produce different codes
	code3, _ := generateCode(6)
	if code == code3 {
		t.Fatal("expected different codes")
	}
}

func TestCodeHash(t *testing.T) {
	code := "123456"
	hash := codeHash(code)
	if len(hash) != 64 {
		t.Fatalf("expected 64-char hash, got %d", len(hash))
	}
	if codeHash(code) != codeHash(code) {
		t.Fatal("expected stable hash")
	}
	if codeHash("123456") == codeHash("654321") {
		t.Fatal("expected different hashes for different codes")
	}
}

func TestTargetHash(t *testing.T) {
	email := "user@example.com"
	hash := targetHash(email)
	if len(hash) != 64 {
		t.Fatalf("expected 64-char hash, got %d", len(hash))
	}
	// Case sensitive (email normalization happens before hashing)
	if targetHash("user@example.com") != targetHash("user@example.com") {
		t.Fatal("expected stable hash")
	}
}

func TestConfigValidate(t *testing.T) {
	key, _ := GenerateES256Key()
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			"missing DB",
			Config{Mode: "development", JWT: JWTConfig{SecretKey: "this-is-32-bytes-for-test-secret!"}},
			true,
		},
		{
			"empty JWT secret",
			Config{DB: testDB(), Mode: "development", JWT: JWTConfig{SecretKey: ""}},
			true,
		},
		{
			"short secret in production",
			Config{DB: testDB(), Mode: "production", JWT: JWTConfig{SecretKey: "short"}},
			true,
		},
		{
			"valid production config with long secret",
			Config{DB: testDB(), Mode: "production", JWT: JWTConfig{SecretKey: "this-is-32-bytes-for-test-secret!"}},
			false,
		},
		{
			"valid KeySet without HMAC",
			Config{DB: testDB(), Mode: "production", JWT: JWTConfig{KeySet: NewKeySet([]*SigningKey{key}, "")}},
			false,
		},
		{
			"negative token TTL",
			Config{DB: testDB(), Mode: "development", JWT: JWTConfig{SecretKey: "this-is-32-bytes-for-test-secret!"}, AccessTokenTTL: -1, RefreshTokenTTL: 30 * 24 * time.Hour},
			true,
		},
		{
			"password min length < 8",
			Config{DB: testDB(), Mode: "development", JWT: JWTConfig{SecretKey: "this-is-32-bytes-for-test-secret!"}, Password: PasswordConfig{MinLength: 4, MaxLength: 128}},
			true,
		},
		{
			"password max < min",
			Config{DB: testDB(), Mode: "development", JWT: JWTConfig{SecretKey: "this-is-32-bytes-for-test-secret!"}, Password: PasswordConfig{MinLength: 20, MaxLength: 10}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg = tt.cfg.withDefaults()
			err := tt.cfg.validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigWithDefaults(t *testing.T) {
	cfg := Config{DB: testDB(), JWT: JWTConfig{SecretKey: "test"}, Mode: ""}
	cfg = cfg.withDefaults()
	if cfg.Mode != "production" {
		t.Fatalf("expected production mode default, got %s", cfg.Mode)
	}
	if cfg.DefaultTenantID != "default" {
		t.Fatalf("expected default tenant ID, got %s", cfg.DefaultTenantID)
	}
	if cfg.Verification.CodeLength != 6 {
		t.Fatalf("expected 6-digit code, got %d", cfg.Verification.CodeLength)
	}
	if cfg.Password.MinLength != 12 {
		t.Fatalf("expected min length 12, got %d", cfg.Password.MinLength)
	}
	if cfg.Password.MaxLength != 128 {
		t.Fatalf("expected max length 128, got %d", cfg.Password.MaxLength)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Fatalf("expected 15m access token TTL, got %v", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 30*24*time.Hour {
		t.Fatalf("expected 30d refresh token TTL, got %v", cfg.RefreshTokenTTL)
	}
	if cfg.Risk.MaxLoginFailuresPerUser != 10 {
		t.Fatalf("expected 10 max login failures, got %d", cfg.Risk.MaxLoginFailuresPerUser)
	}
}

func TestNewID(t *testing.T) {
	id := newID()
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
	// UUID v7 format: 8-4-4-4-12
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected UUID format, got %s", id)
	}
	// Multiple calls should produce different IDs
	id2 := newID()
	if id == id2 {
		t.Fatal("expected different IDs")
	}
}

func TestUserHasAdminRole(t *testing.T) {
	tests := []struct {
		name  string
		roles []string
		want  bool
	}{
		{"platform_admin", []string{"platform_admin"}, true},
		{"tenant_owner", []string{"tenant_owner"}, true},
		{"tenant_admin", []string{"tenant_admin"}, true},
		{"security_admin", []string{"security_admin"}, true},
		{"regular user", []string{"user"}, false},
		{"guest", []string{"guest"}, false},
		{"empty roles", []string{}, false},
		{"mixed roles", []string{"user", "tenant_admin"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userHasAdminRole(tt.roles); got != tt.want {
				t.Errorf("userHasAdminRole(%v) = %v, want %v", tt.roles, got, tt.want)
			}
		})
	}
}

func TestContains(t *testing.T) {
	list := []string{"a", "b", "c"}
	if !contains(list, "a") {
		t.Fatal("expected to find 'a'")
	}
	if contains(list, "d") {
		t.Fatal("expected not to find 'd'")
	}
	if contains(nil, "a") {
		t.Fatal("expected not to find in nil slice")
	}
	if contains([]string{}, "a") {
		t.Fatal("expected not to find in empty slice")
	}
}

func TestTruncateString(t *testing.T) {
	if truncateString("hello", 3) != "hel" {
		t.Fatal("expected truncated string")
	}
	if truncateString("hi", 3) != "hi" {
		t.Fatal("expected unchanged short string")
	}
	if truncateString("", 5) != "" {
		t.Fatal("expected empty string")
	}
}

// testDB returns a non-nil *gorm.DB for config validation tests.
func testDB() *gorm.DB {
	return &gorm.DB{}
}
