package server

import (
	"errors"
	"os"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/golang-jwt/jwt/v5"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
)

type JwtConf struct {
	Method                     string
	SigningKey                 string
	Issuer                     string
	Subject                    string
	AccessTokenDurationMinute  int64
	RefreshTokenDurationMinute int64
}

type GaiaClaims struct {
	jwt.RegisteredClaims
	ClaimsKey string `json:"ck"`
}

func DefaultJwtConf() JwtConf {
	return JwtConf{
		Method: "Hmac256",
		// 从环境变量或配置文件读取密钥
		SigningKey: os.Getenv("GAIA_JWT_SIGNING_KEY"),
		Issuer:     "gaia-framework",
		Subject:    "gaia-sub",
		// 增加默认有效期
		AccessTokenDurationMinute:  10,
		RefreshTokenDurationMinute: 60 * 24 * 7,
	}
}

type JwtAuth struct {
	conf          JwtConf
	SigningMethod jwt.SigningMethod
}

func NewJwtConf(schema string) JwtConf {
	conf := DefaultJwtConf()
	gaia.LoadConfToObjWith(schema, &conf)
	return conf
}

func NewJwtAuth(conf JwtConf) *JwtAuth {
	var jwtMethod jwt.SigningMethod
	switch conf.Method {
	case "Hmac256":
		jwtMethod = jwt.SigningMethodHS256
	case "Hmac512":
		jwtMethod = jwt.SigningMethodHS512
	case "Rs256":
		jwtMethod = jwt.SigningMethodRS256
	case "Rs512":
		jwtMethod = jwt.SigningMethodRS512
	default:
		jwtMethod = jwt.SigningMethodHS256
	}
	return &JwtAuth{conf: conf, SigningMethod: jwtMethod}
}

// GenerateToken 生成Token
func (j *JwtAuth) GenerateToken(ckv string) (string, string, error) {
	token, err := j.GenerateAccessToken(ckv)
	if err != nil {
		return "", "", errors.New("failed to generate access token")
	}
	refreshToken, err := j.GenerateRefreshToken(ckv)
	if err != nil {
		return "", "", errors.New("failed to generate refresh token")
	}

	return token, refreshToken, nil
}

func (j *JwtAuth) GenerateAccessToken(ckv string) (string, error) {
	// 添加验证确保密钥不为空
	if j.conf.SigningKey == "" {
		return "", errors.New("JWT signing key cannot be empty")
	}

	claims := jwt.NewWithClaims(j.SigningMethod, GaiaClaims{jwt.RegisteredClaims{
		Issuer:    j.conf.Issuer,
		Subject:   j.conf.Subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(j.conf.AccessTokenDurationMinute) * time.Minute)),
		IssuedAt:  &jwt.NumericDate{Time: time.Now()},
		ID:        gaia.GetUUID(),
	}, ckv})

	return claims.SignedString([]byte(j.conf.SigningKey))
}

func (j *JwtAuth) GenerateRefreshToken(ckv string) (string, error) {
	// 添加验证确保密钥不为空
	if j.conf.SigningKey == "" {
		return "", errors.New("JWT signing key cannot be empty")
	}

	claims := jwt.NewWithClaims(j.SigningMethod, GaiaClaims{jwt.RegisteredClaims{
		Issuer:    j.conf.Issuer,
		Subject:   j.conf.Subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(j.conf.RefreshTokenDurationMinute) * time.Minute)),
		IssuedAt:  &jwt.NumericDate{Time: time.Now()},
		ID:        gaia.GetUUID(),
	}, ckv})

	return claims.SignedString([]byte(j.conf.SigningKey))
}

func Auth(j *JwtAuth) app.HandlerFunc {
	return MakePlugin(j.auth)
}

func (j *JwtAuth) auth(c Request) (err error) {
	// 添加验证确保密钥不为空
	if j.conf.SigningKey == "" {
		return errors.New("JWT signing key cannot be empty")
	}
	//鉴权token，通过继续，否则失败
	auth := string(c.c.GetHeader("Authorization"))
	if len(auth) == 0 {
		return errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	}

	ck, err := j.GetCk(auth)
	if err != nil {
		return err
	}

	c.c.Set("userKey", ck)
	return nil
}

// GetCk 获取jwt中的用户信息
func (j *JwtAuth) GetCk(token string) (ck string, err error) {
	// 添加验证确保密钥不为空
	if j.conf.SigningKey == "" {
		return "", errors.New("JWT signing key cannot be empty")
	}
	parse, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		return []byte(j.conf.SigningKey), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", errwrap.Error(errwrap.EcAuthErr, errors.New("token过期"))
		}
		return "", errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	}

	if v, ok := parse.Claims.(jwt.MapClaims)["ck"].(string); !ok {
		return "", errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	} else {
		return v, nil
	}
}
