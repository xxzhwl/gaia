package account

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// SigningKey 持有单个签名密钥对及其标识符。
type SigningKey struct {
	ID        string    `json:"id"`
	Algorithm string    `json:"algorithm"` // ES256, RS256
	Public    string    `json:"public"`    // PEM-encoded public key
	Private   string    `json:"private"`   // PEM-encoded private key (signing only)
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IsActive  bool      `json:"is_active"`
}

// KeySet 管理 JWT 的签名和验证密钥。
// 支持多密钥轮换：一个活跃签名密钥和多个验证密钥（之前活跃的密钥）。
type KeySet struct {
	mu         sync.RWMutex
	signingKey *SigningKey
	verifyKeys map[string]*SigningKey
	hmacSecret []byte // fallback for HS256
}

// NewKeySet 从给定的密钥列表创建 KeySet。
func NewKeySet(keys []*SigningKey, hmacSecret string) *KeySet {
	ks := &KeySet{
		verifyKeys: make(map[string]*SigningKey),
		hmacSecret: []byte(hmacSecret),
	}
	for _, k := range keys {
		if k.IsActive {
			ks.signingKey = k
		}
		ks.verifyKeys[k.ID] = k
	}
	return ks
}

// GenerateES256Key 创建新的 ES256（ECDSA P-256）签名密钥。
func GenerateES256Key() (*SigningKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ecdsa key: %w", err)
	}

	privBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshal ecdsa private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal ecdsa public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	return &SigningKey{
		ID:        uuid.NewString(),
		Algorithm: "ES256",
		Public:    string(pubPEM),
		Private:   string(privPEM),
		CreatedAt: time.Now(),
		IsActive:  true,
	}, nil
}

// GenerateRS256Key 创建新的 RS256（RSA 2048）签名密钥。
func GenerateRS256Key() (*SigningKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes})

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshal rsa public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	return &SigningKey{
		ID:        uuid.NewString(),
		Algorithm: "RS256",
		Public:    string(pubPEM),
		Private:   string(privPEM),
		CreatedAt: time.Now(),
		IsActive:  true,
	}, nil
}

// Rotate 生成新密钥并将其提升为活跃签名密钥。
// 旧签名密钥保留用于验证，直到用它签发的所有令牌过期。
func (ks *KeySet) Rotate(algorithm string) (*SigningKey, error) {
	var newKey *SigningKey
	var err error

	switch algorithm {
	case "ES256":
		newKey, err = GenerateES256Key()
	case "RS256":
		newKey, err = GenerateRS256Key()
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
	if err != nil {
		return nil, err
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	// Deactivate the current signing key but keep it for verification
	if ks.signingKey != nil {
		ks.signingKey.IsActive = false
	}
	ks.signingKey = newKey
	ks.verifyKeys[newKey.ID] = newKey

	return newKey, nil
}

// Sign 使用活跃签名密钥创建 JWT。
func (ks *KeySet) Sign(claims jwt.Claims) (string, error) {
	ks.mu.RLock()
	signingKey := ks.signingKey
	hmacSecret := ks.hmacSecret
	ks.mu.RUnlock()

	if signingKey == nil && len(hmacSecret) == 0 {
		return "", errors.New("no signing key configured")
	}

	// Use HMAC signing if no asymmetric key is configured (backward compat)
	if signingKey == nil {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		return token.SignedString(hmacSecret)
	}

	var method jwt.SigningMethod
	switch signingKey.Algorithm {
	case "ES256":
		method = jwt.SigningMethodES256
	case "RS256":
		method = jwt.SigningMethodRS256
	default:
		return "", fmt.Errorf("unsupported signing algorithm: %s", signingKey.Algorithm)
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = signingKey.ID

	privateKey, err := parsePrivateKey(signingKey)
	if err != nil {
		return "", err
	}
	return token.SignedString(privateKey)
}

// ParseWithKeySet 解析并验证 JWT，通过 kid 查找密钥。
// 该方法会正确填充传入的 claims 对象。
func (ks *KeySet) ParseWithKeySet(tokenString string, claims jwt.Claims, opts ...jwt.ParserOption) (*jwt.Token, error) {
	// 先无验证解析以提取 kid
	partial, _, err := new(jwt.Parser).ParseUnverified(tokenString, claims)
	if err != nil {
		return nil, err
	}

	kid, _ := partial.Header["kid"].(string)

	ks.mu.RLock()
	var key any
	if kid != "" {
		if k, ok := ks.verifyKeys[kid]; ok {
			key, _ = parsePublicKey(k)
		}
	} else if len(ks.hmacSecret) > 0 {
		key = ks.hmacSecret
	} else if ks.signingKey != nil {
		key, _ = parsePublicKey(ks.signingKey)
	}
	ks.mu.RUnlock()

	if key == nil {
		return nil, errors.New("no matching key found for token verification")
	}

	parser := jwt.NewParser(opts...)
	return parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return key, nil
	})
}

// JWKS 以 JWKS 格式返回公钥，供 JWKS 端点使用。
func (ks *KeySet) JWKS() ([]byte, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()

	var keys []map[string]any
	for _, k := range ks.verifyKeys {
		jwk, err := signingKeyToJWK(k)
		if err != nil {
			continue
		}
		keys = append(keys, jwk)
	}

	return json.Marshal(map[string]any{"keys": keys})
}

// ActiveKeyID 返回当前签名密钥的 ID。
func (ks *KeySet) ActiveKeyID() string {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	if ks.signingKey == nil {
		return ""
	}
	return ks.signingKey.ID
}

// LoadKeySetFromFile 从 JSON 文件加载 KeySet。
func LoadKeySetFromFile(path string) (*KeySet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read key set file: %w", err)
	}
	var payload struct {
		Keys       []*SigningKey `json:"keys"`
		HMACSecret string        `json:"hmac_secret"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse key set file: %w", err)
	}
	ks := NewKeySet(payload.Keys, payload.HMACSecret)
	if ks.signingKey == nil && len(ks.hmacSecret) == 0 {
		return nil, errors.New("key set has no active signing key or hmac secret")
	}
	return ks, nil
}

func parsePrivateKey(k *SigningKey) (any, error) {
	block, _ := pem.Decode([]byte(k.Private))
	if block == nil {
		return nil, errors.New("failed to decode private key PEM")
	}
	switch k.Algorithm {
	case "ES256":
		return x509.ParseECPrivateKey(block.Bytes)
	case "RS256":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	return nil, fmt.Errorf("unsupported algorithm: %s", k.Algorithm)
}

func parsePublicKey(k *SigningKey) (any, error) {
	block, _ := pem.Decode([]byte(k.Public))
	if block == nil {
		return nil, errors.New("failed to decode public key PEM")
	}
	return x509.ParsePKIXPublicKey(block.Bytes)
}

func signingKeyToJWK(k *SigningKey) (map[string]any, error) {
	block, _ := pem.Decode([]byte(k.Public))
	if block == nil {
		return nil, errors.New("failed to decode PEM")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	jwk := map[string]any{
		"kid": k.ID,
		"use": "sig",
	}

	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		// Use elliptic.Marshal to get uncompressed point, then extract X,Y
		marshalled := elliptic.Marshal(key.Curve, key.X, key.Y)
		if len(marshalled) >= 65 {
			jwk["kty"] = "EC"
			jwk["crv"] = "P-256"
			jwk["x"] = base64URLEncode(marshalled[1:33])
			jwk["y"] = base64URLEncode(marshalled[33:65])
			jwk["alg"] = "ES256"
		}
	case *rsa.PublicKey:
		jwk["kty"] = "RSA"
		jwk["n"] = base64URLEncode(key.N.Bytes())
		jwk["e"] = base64URLEncode(big.NewInt(int64(key.E)).Bytes())
		jwk["alg"] = "RS256"
	}

	return jwk, nil
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}
