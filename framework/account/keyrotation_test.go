package account

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateES256Key(t *testing.T) {
	key, err := GenerateES256Key()
	if err != nil {
		t.Fatalf("GenerateES256Key failed: %v", err)
	}
	if key.Algorithm != "ES256" {
		t.Fatalf("expected ES256, got %s", key.Algorithm)
	}
	if key.ID == "" {
		t.Fatal("expected non-empty key ID")
	}
	if key.Private == "" || key.Public == "" {
		t.Fatal("expected non-empty PEM keys")
	}
	if !key.IsActive {
		t.Fatal("expected new key to be active")
	}
}

func TestGenerateRS256Key(t *testing.T) {
	key, err := GenerateRS256Key()
	if err != nil {
		t.Fatalf("GenerateRS256Key failed: %v", err)
	}
	if key.Algorithm != "RS256" {
		t.Fatalf("expected RS256, got %s", key.Algorithm)
	}
	if key.ID == "" {
		t.Fatal("expected non-empty key ID")
	}
	if key.Private == "" || key.Public == "" {
		t.Fatal("expected non-empty PEM keys")
	}
}

func TestKeySetSignAndVerify(t *testing.T) {
	key, err := GenerateES256Key()
	if err != nil {
		t.Fatalf("GenerateES256Key failed: %v", err)
	}
	ks := NewKeySet([]*SigningKey{key}, "")

	claims := jwt.MapClaims{
		"sub": "user123",
		"iss": "test",
		"exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
	}

	signed, err := ks.Sign(claims)
	if err != nil {
		t.Fatalf("KeySet.Sign failed: %v", err)
	}

	parsedClaims := jwt.MapClaims{}
	token, err := ks.ParseWithKeySet(signed, parsedClaims, jwt.WithIssuer("test"))
	if err != nil {
		t.Fatalf("KeySet.ParseWithKeySet failed: %v", err)
	}
	if !token.Valid {
		t.Fatal("expected valid token")
	}
	if parsedClaims["sub"] != "user123" {
		t.Fatalf("expected sub=user123, got %v", parsedClaims["sub"])
	}
}

func TestKeySetHMACFallback(t *testing.T) {
	ks := NewKeySet(nil, "my-hmac-secret-key-at-least-32-bytes!!")

	claims := jwt.MapClaims{
		"sub": "user123",
		"exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
	}

	signed, err := ks.Sign(claims)
	if err != nil {
		t.Fatalf("KeySet.Sign HMAC failed: %v", err)
	}

	parsedClaims := jwt.MapClaims{}
	_, err = ks.ParseWithKeySet(signed, parsedClaims)
	if err != nil {
		t.Fatalf("KeySet.ParseWithKeySet HMAC failed: %v", err)
	}
}

func TestKeySetRotate(t *testing.T) {
	key1, err := GenerateES256Key()
	if err != nil {
		t.Fatalf("GenerateES256Key failed: %v", err)
	}
	ks := NewKeySet([]*SigningKey{key1}, "")

	// Rotate to a new key
	key2, err := ks.Rotate("ES256")
	if err != nil {
		t.Fatalf("KeySet.Rotate failed: %v", err)
	}
	if !key2.IsActive {
		t.Fatal("expected new key after rotate to be active")
	}

	// Old key should be deactivated but still available for verification
	claims := jwt.MapClaims{
		"sub": "user123",
		"exp": jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
	}

	// Sign with new key
	signed, err := ks.Sign(claims)
	if err != nil {
		t.Fatalf("KeySet.Sign after rotate failed: %v", err)
	}

	parsedClaims := jwt.MapClaims{}
	token, err := ks.ParseWithKeySet(signed, parsedClaims)
	if err != nil {
		t.Fatalf("KeySet.ParseWithKeySet after rotate failed: %v", err)
	}
	if !token.Valid {
		t.Fatal("expected valid token after rotate")
	}
}

func TestKeySetJWKS(t *testing.T) {
	key1, err := GenerateES256Key()
	if err != nil {
		t.Fatalf("GenerateES256Key failed: %v", err)
	}
	key2, err := GenerateRS256Key()
	if err != nil {
		t.Fatalf("GenerateRS256Key failed: %v", err)
	}
	ks := NewKeySet([]*SigningKey{key1, key2}, "")

	jwks, err := ks.JWKS()
	if err != nil {
		t.Fatalf("KeySet.JWKS failed: %v", err)
	}
	if len(jwks) == 0 {
		t.Fatal("expected non-empty JWKS output")
	}
}

func TestKeySetNoKeysError(t *testing.T) {
	ks := NewKeySet(nil, "")
	claims := jwt.MapClaims{"sub": "test"}
	_, err := ks.Sign(claims)
	if err == nil {
		t.Fatal("expected error when no keys configured")
	}
}

func TestKeySetParseInvalidToken(t *testing.T) {
	key, _ := GenerateES256Key()
	ks := NewKeySet([]*SigningKey{key}, "")
	claims := jwt.MapClaims{}
	_, err := ks.ParseWithKeySet("invalid.jwt.token", claims)
	if err == nil {
		t.Fatal("expected error when parsing invalid token")
	}
}

func TestKeySetActiveKeyID(t *testing.T) {
	ks := NewKeySet(nil, "")
	if id := ks.ActiveKeyID(); id != "" {
		t.Fatalf("expected empty active key ID, got %s", id)
	}

	key, _ := GenerateES256Key()
	ks2 := NewKeySet([]*SigningKey{key}, "")
	if id := ks2.ActiveKeyID(); id != key.ID {
		t.Fatalf("expected active key ID %s, got %s", key.ID, id)
	}
}

func TestSigningKeyToJWK(t *testing.T) {
	key, err := GenerateES256Key()
	if err != nil {
		t.Fatalf("GenerateES256Key failed: %v", err)
	}

	jwk, err := signingKeyToJWK(key)
	if err != nil {
		t.Fatalf("signingKeyToJWK failed: %v", err)
	}
	if jwk["kid"] != key.ID {
		t.Fatalf("expected kid %s, got %v", key.ID, jwk["kid"])
	}
	if jwk["use"] != "sig" {
		t.Fatalf("expected use sig, got %v", jwk["use"])
	}
	if jwk["kty"] != "EC" {
		t.Fatalf("expected kty EC for ES256 key, got %v", jwk["kty"])
	}
}

// Test that signed tokens include a kid in the header
func TestTokenHasKID(t *testing.T) {
	key, _ := GenerateES256Key()
	ks := NewKeySet([]*SigningKey{key}, "")
	claims := jwt.MapClaims{"sub": "test"}
	signed, _ := ks.Sign(claims)

	// Parse without verification to check header
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, _ := parser.ParseUnverified(signed, jwt.MapClaims{})
	if token.Header["kid"] != key.ID {
		t.Fatalf("expected kid %s in token header, got %v", key.ID, token.Header["kid"])
	}
}
