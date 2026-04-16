package server

import (
	"net/http"
	"strings"
	"sync"
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

// sync.Once 缓存单例
var defaultJWTManagerOnce sync.Once
var defaultJWTManagerInstance *JWTManager

func DefaultJWTManager() *JWTManager {
	defaultJWTManagerOnce.Do(func() {
		secretKey := gaia.GetSafeConfString("JWT.SecretKey")
		if strings.TrimSpace(secretKey) == "" {
			gaia.Warn("JWT.SecretKey 未配置，JWT 功能将不可用")
		}

		defaultJWTManagerInstance = NewJWTManager(
			secretKey,
			gaia.GetSafeConfStringWithDefault("JWT.Issuer", "gaia-jwt-issuer"),
			time.Minute*time.Duration(gaia.GetSafeConfInt64WithDefault("JWT.AccessTokenExp", 1440)), // 1440分钟=24小时
			time.Minute*time.Duration(gaia.GetSafeConfInt64WithDefault("JWT.RefreshTokenExp", 24*30*60)),
		)
	})
	return defaultJWTManagerInstance
}

// GenerateAccessToken 生成访问Token
func (m *JWTManager) GenerateAccessToken(userID int64, uuid, username string, roles []string) (string, time.Time, error) {
	if err := m.validateSecretKey(); err != nil {
		return "", time.Time{}, err
	}

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
	if err := m.validateSecretKey(); err != nil {
		return "", time.Time{}, err
	}

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
	if err := m.validateSecretKey(); err != nil {
		return nil, err
	}

	parseOpts := []jwt.ParserOption{jwt.WithExpirationRequired()}
	if strings.TrimSpace(m.Issuer) != "" {
		parseOpts = append(parseOpts, jwt.WithIssuer(m.Issuer))
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("invalid signing method")
		}
		return []byte(m.SecretKey), nil
	}, parseOpts...)

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

func (m *JWTManager) validateSecretKey() error {
	if strings.TrimSpace(m.SecretKey) == "" {
		return errors.New("jwt secret key is empty")
	}
	return nil
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
		if claims.ExpiresAt == nil || claims.ExpiresAt.Before(time.Now()) {
			return errwrap.Error(http.StatusUnauthorized, errors.New("token expired"))
		}

		// 将用户信息存入context
		arg.C().Set("user", claims)
		return nil
	})
}
