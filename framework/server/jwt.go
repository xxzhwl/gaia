package server

import (
	"errors"
	"fmt"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/golang-jwt/jwt/v5"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/redis"
	"github.com/xxzhwl/gaia/errwrap"
	"strings"
	"time"
)

type JwtConf struct {
	Method         string
	SigningKey     string
	Issuer         string
	Subject        string
	DurationMinute int64
}

type GaiaClaims struct {
	jwt.RegisteredClaims
	ClaimsKey string `json:"ck"`
}

func DefaultJwtConf() JwtConf {
	return JwtConf{
		Method:         "Hmac256",
		SigningKey:     "github.com/xxzhwl/gaia",
		Issuer:         "gaia-framework",
		Subject:        "gaia-sub",
		DurationMinute: 5,
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
func (j *JwtAuth) GenerateToken(ckv string) (string, error) {
	claims := jwt.NewWithClaims(j.SigningMethod, GaiaClaims{jwt.RegisteredClaims{
		Issuer:    j.conf.Issuer,
		Subject:   j.conf.Subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(j.conf.DurationMinute) * time.Minute)),
		IssuedAt:  &jwt.NumericDate{Time: time.Now()},
		ID:        gaia.GetUUID(),
	}, ckv})

	token, err := claims.SignedString([]byte(j.conf.SigningKey))
	if err != nil {
		return "", err
	}
	return token, nil
}

// GenerateRefreshToken 生成Refresh-Token
func (j *JwtAuth) GenerateRefreshToken(ckv string) (string, error) {
	return j.GenerateToken(ckv + "-refresh")
}

func (j *JwtAuth) Auth() app.HandlerFunc {
	return MakePlugin(j.auth)
}

func (j *JwtAuth) auth(c Request) (err error) {
	//鉴权token，通过继续，否则失败
	auth := string(c.c.GetHeader("Authorization"))
	if len(auth) == 0 {
		return errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	}

	client := redis.NewFrameworkClient().WithCtx(c.TraceContext)
	uid, err := client.GetString(fmt.Sprintf("token-%s", auth))
	if err != nil {
		return errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	}
	if len(uid) == 0 {
		return errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	}

	ck, err := j.GetCk(auth)
	if err != nil {
		return err
	}

	if IsRefreshTokenKey(ck) {
		return errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	}

	c.c.Set("userKey", ck)
	return nil
}

// GetCk 获取jwt中的用户信息
func (j *JwtAuth) GetCk(token string) (ck string, err error) {
	parse, err := jwt.Parse(token, func(token *jwt.Token) (any, error) {
		return []byte(j.conf.SigningKey), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return "", errwrap.Error(errwrap.EcTokenExpired, errors.New("token过期"))
		}
		return "", errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	}

	if v, ok := parse.Claims.(jwt.MapClaims)["ck"].(string); !ok {
		return "", errwrap.Error(errwrap.EcAuthErr, errors.New("鉴权失败"))
	} else {
		return v, nil
	}
}

// IsRefreshTokenKey 判断是不是refreshToken的用户信息
func IsRefreshTokenKey(ck string) bool {
	if strings.HasSuffix(ck, "-refresh") {
		return true
	}
	return false
}

// GetUserKeyFromCk 判断是不是refreshToken的用户信息
func GetUserKeyFromCk(ck string) string {
	if IsRefreshTokenKey(ck) {
		return strings.ReplaceAll(ck, "-refresh", "")
	}
	return ""
}
