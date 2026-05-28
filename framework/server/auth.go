// Package server 包注释
// @author wanlizhan
// @created 2024/5/23
package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/xxzhwl/gaia"
)

type SysAuthModel struct {
	Id         int64
	SysId      string
	SysKey     string
	Enable     bool
	Duty       string
	CreateTime time.Time
	UpdateTime time.Time
}

func SysAuth(c *app.RequestContext) {
	path := c.Request.RequestURI()
	auth := c.GetHeader("Authorization")
	method := c.Request.Method()
	if err := resolveAuth(string(auth), string(path), string(method)); err != nil {
		c.Abort()
		NewRequest(c).resp(nil, err)
		return
	}
}

// 系统ID(三位)算法256|512(三位)之后为秒级时间戳后面加_签名
func resolveAuth(auth, path, method string) error {
	split := strings.Split(auth, "_")
	if len(split) != 2 {
		return errors.New("签名校验失败")
	}

	pre, sig := split[0], split[1]
	if len(pre) < 6 {
		return errors.New("签名校验失败")
	}
	currentTimeStamp := time.Now().Unix()
	systemId, cryMethod, reqTimeStampStr := pre[0:3], pre[3:6], pre[6:]

	//根据系统id获取对应的系统密钥
	key, err := getSystemKeyById(systemId)
	if err != nil {
		return err
	}
	if len(key) == 0 {
		return errors.New("签名校验失败")
	}

	// 使用配置而不是硬编码时间
	var allowedTimeWindow = gaia.GetSafeConfInt64("Auth.AllowedTimeWindow")
	if allowedTimeWindow == 0 {
		allowedTimeWindow = 60 // 默认1分钟
	}

	reqTimeStamp, err := strconv.ParseInt(reqTimeStampStr, 10, 64)
	if err != nil {
		return errors.New("签名校验失败")
	}
	timeDiff := currentTimeStamp - reqTimeStamp
	if absInt64(timeDiff) > allowedTimeWindow {
		// 区分过期与时间戳异常，便于排查（仍统一返回"签名已过期"，避免泄漏服务器时间方向）
		return errors.New("签名已过期")
	}
	res := false
	// 对密钥和msg进行hmac加密，校验和传入的sig是否一致
	// 消息中包含时间戳，避免签名被重放后仅替换时间戳即可绕过校验
	msg := fmt.Sprintf("%s%s%s", method, path, reqTimeStampStr)
	switch cryMethod {
	case "256":
		temp := HmacSha256([]byte(key), msg, false)
		if subtle.ConstantTimeCompare([]byte(temp), []byte(sig)) == 1 {
			res = true
			break
		}
	case "512":
		temp := HmacSha512([]byte(key), msg, false)
		if subtle.ConstantTimeCompare([]byte(temp), []byte(sig)) == 1 {
			res = true
			break
		}
	}
	if !res {
		return errors.New("签名校验失败")
	}
	return nil
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// getSystemKeyById 从 sys_auth 表中查询 systemId 对应的密钥。
// 高 QPS 接口每次签名校验都会调到这里，因此使用 gaia.CacheLoad 进行 5 分钟缓存。
// 缓存 TTL 取决于密钥变更频率：项目侧若需要立即生效，可在管理后台改密钥后清缓存。
func getSystemKeyById(systemId string) (string, error) {
	return gaia.CacheLoad("sys_auth_key_"+systemId, time.Minute*5, func() (string, error) {
		db, err := gaia.NewFrameworkMysql()
		if err != nil {
			return "", err
		}
		sysAuth := &SysAuthModel{}
		tx := db.GetGormDb().Table("sys_auth").
			Where("sys_id=? and enable=?", systemId, 1).
			First(&sysAuth)
		if tx.Error != nil {
			return "", tx.Error
		}
		return sysAuth.SysKey, nil
	})
}

// HmacSha256 通过hmac_sha256算法生成一个签名值，最终的签名值为16进制数据
// secretKey为 []byte类型，通常是 hex.DecodeString() 返回的[]byte类型
// isRetBase64 最终结果是否以base-64编码的形式返回，如果传入false，则以16进制的形式返回
func HmacSha256(secretKey []byte, message string, isRetBase64 bool) string {
	return _hmac(sha256.New, secretKey, message, isRetBase64)
}

// HmacSha512 通过hmac_sha512算法生成一个签名值，最终的签名值为16进制数据
// secretKey为 []byte类型，通常是 hex.DecodeString() 返回的[]byte类型
// isRetBase64 最终结果是否以base-64编码的形式返回，如果传入false，则以16进制的形式返回
func HmacSha512(secretKey []byte, message string, isRetBase64 bool) string {
	return _hmac(sha512.New, secretKey, message, isRetBase64)
}

// secretKey为 []byte类型，通常是 hex.DecodeString() 返回的[]byte类型
// isRetBase64 最终结果是否以base-64编码的形式返回，如果传入false，则以16进制的形式返回
func _hmac(h func() hash.Hash, secretKey []byte, message string, isRetBase64 bool) string {
	mac := hmac.New(h, secretKey)
	mac.Write([]byte(message))
	sig := mac.Sum(nil)
	if isRetBase64 {
		return base64.StdEncoding.EncodeToString(sig)
	} else {
		return hex.EncodeToString(sig)
	}
}
