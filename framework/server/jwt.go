package server

import (
	"net/http"
	"strings"
	"time"

	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/golang-jwt/jwt/v5"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
)

// Claims JWT声明
type Claims struct {
	UserID   int64    `json:"user_id"`
	UUID     string   `json:"uuid"`
	Username string   `json:"username"`
	Roles    []string `json:"roles,omitempty"`
	jwt.RegisteredClaims
}

// JWTManager JWT管理器
type JWTManager struct {
	SecretKey          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	Issuer             string
}

// NewJWTManager 创建JWT管理器
func NewJWTManager(secretKey, issuer string, accessTokenExpiry, refreshTokenExpiry time.Duration) *JWTManager {
	return &JWTManager{
		SecretKey:          secretKey,
		AccessTokenExpiry:  accessTokenExpiry,
		RefreshTokenExpiry: refreshTokenExpiry,
		Issuer:             issuer,
	}
}

func DefaultJWTManager() *JWTManager {
	return NewJWTManager(
		gaia.GetSafeConfStringWithDefault("JWT.SecretKey", "gaia-jwt-secret-key"),
		gaia.GetSafeConfStringWithDefault("JWT.Issuer", "gaia-jwt-issuer"),
		time.Minute*time.Duration(gaia.GetSafeConfInt64WithDefault("JWT.AccessTokenExp", 24)),
		time.Minute*time.Duration(gaia.GetSafeConfInt64WithDefault("JWT.RefreshTokenExp", 24*30*60)),
	)
}

// GenerateAccessToken 生成访问Token
func (m *JWTManager) GenerateAccessToken(userID int64, uuid, username string, roles []string) (string, time.Time, error) {
	expiresAt := time.Now().Add(m.AccessTokenExpiry)

	claims := &Claims{
		UserID:   userID,
		UUID:     uuid,
		Username: username,
		Roles:    roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    m.Issuer,
			Subject:   uuid,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(m.SecretKey))

	return tokenString, expiresAt, err
}

// GenerateRefreshToken 生成刷新Token
func (m *JWTManager) GenerateRefreshToken(userID int64, uuid string) (string, time.Time, error) {
	expiresAt := time.Now().Add(m.RefreshTokenExpiry)

	claims := &Claims{
		UserID: userID,
		UUID:   uuid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    m.Issuer,
			Subject:   uuid,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(m.SecretKey))

	return tokenString, expiresAt, err
}

// ValidateToken 验证Token
func (m *JWTManager) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return []byte(m.SecretKey), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

func (m *JWTManager) Authenticate() app.HandlerFunc {
	return MakePlugin(func(arg Request) error {
		// 获取Authorization头
		authHeader := string(arg.C().GetHeader("Authorization"))
		if authHeader == "" {
			return errwrap.Error(http.StatusUnauthorized, errors.New("missing authorization header"))
		}

		// 检查Bearer前缀
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			return errwrap.Error(http.StatusUnauthorized, errors.New("invalid authorization format"))
		}

		// 验证Token
		claims, err := m.ValidateToken(parts[1])
		if err != nil {
			return errwrap.Error(http.StatusUnauthorized, errors.New("invalid token"))
		}

		// 检查refreshToken是否过期
		if claims.ExpiresAt.Before(time.Now()) {
			return errwrap.Error(http.StatusUnauthorized, errors.New("token expired"))
		}

		// 将用户信息存入context
		arg.C().Set("user", claims)
		return nil
	})
}
