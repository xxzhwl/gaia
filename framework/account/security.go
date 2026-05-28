package account

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/crypto/argon2"
)

const argonKeyLen uint32 = 32

func normalizeEmail(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func normalizePhone(v string) string {
	return strings.TrimSpace(v)
}

func nullableString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// hashPassword hashes a password using Argon2id with a random salt.
func hashPassword(password string, cfg PasswordConfig) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, cfg.Argon2Time, cfg.Argon2Memory, cfg.Argon2Threads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		cfg.Argon2Memory,
		cfg.Argon2Time,
		cfg.Argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// verifyPassword checks a password against an Argon2id-encoded hash using constant-time comparison.
func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// needsPasswordUpgrade 检查已编码的密码哈希是否使用当前配置的参数。
// 如果 Argon2 参数（memory、time、threads）与配置不匹配，返回 true。
func needsPasswordUpgrade(encoded string, cfg PasswordConfig) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return true // unknown format, rehash
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return true // unparseable, rehash
	}
	return memory != cfg.Argon2Memory || timeCost != cfg.Argon2Time || threads != cfg.Argon2Threads
}

// validatePassword checks the password length against the configured min/max constraints.
func validatePassword(password string, cfg PasswordConfig) error {
	if len(password) < cfg.MinLength {
		return accountError(ErrInvalidArgument, fmt.Sprintf("密码长度不能小于%d位", cfg.MinLength))
	}
	if len(password) > cfg.MaxLength {
		return accountError(ErrInvalidArgument, fmt.Sprintf("密码长度不能超过%d位", cfg.MaxLength))
	}
	if _, ok := commonWeakPasswords[strings.ToLower(password)]; ok {
		return accountError(ErrInvalidArgument, "密码过于常见，请更换更安全的密码")
	}
	return nil
}

var commonWeakPasswords = map[string]struct{}{
	"password":     {},
	"password1":    {},
	"password123":  {},
	"12345678":     {},
	"123456789":    {},
	"1234567890":   {},
	"123456789012": {},
	"qwerty123":    {},
	"qwerty123456": {},
	"admin123456":  {},
	"letmein123":   {},
	"welcome123":   {},
	"iloveyou123":  {},
	"abc123456":    {},
	"11111111":     {},
	"00000000":     {},
}

// randomToken generates a cryptographically random 256-bit token encoded in raw URL-safe base64.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// tokenHash returns the SHA-256 hex digest of a token.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// hashUserAgent returns the SHA-256 hex digest of a user agent string, or empty if blank.
func hashUserAgent(ua string) string {
	if ua == "" {
		return ""
	}
	return tokenHash(ua)
}

// validateIdentifier checks that the username and at least one of email/phone are provided.
func validateIdentifier(req RegisterRequest) error {
	if strings.TrimSpace(req.Username) == "" {
		return errors.New("username is required")
	}
	return nil
}

// isEmailIdentifier checks whether the given string looks like a valid email address.
func isEmailIdentifier(identifier string) bool {
	addr, err := mail.ParseAddress(identifier)
	return err == nil && addr.Address == identifier && strings.Contains(identifier, "@")
}
