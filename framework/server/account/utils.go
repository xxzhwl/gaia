// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import (
	"github.com/xxzhwl/gaia"
	"golang.org/x/crypto/bcrypt"
)

type EncryptPasswordFunc func(pwd string) (string, error)
type MatchPasswordFunc func(originPwd, storePwd string) bool

var encryptPasswordFunc EncryptPasswordFunc
var matchPasswordFunc MatchPasswordFunc

// SetEncryptPasswordFunc 设置加密方法
func SetEncryptPasswordFunc(f EncryptPasswordFunc) {
	encryptPasswordFunc = f
}

// SetPasswordMatchFunc 设置密码匹配方法
func SetPasswordMatchFunc(f MatchPasswordFunc) {
	matchPasswordFunc = f
}

// EncryptPassword 密码加密
func EncryptPassword(password string) (string, error) {
	if encryptPasswordFunc != nil {
		return encryptPasswordFunc(password)
	}

	storePwd, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(storePwd), err
}

// PasswordMatch 密码对比
func PasswordMatch(originPwd, storePwd string) bool {
	if matchPasswordFunc != nil {
		return matchPasswordFunc(originPwd, storePwd)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storePwd), []byte(originPwd)); err != nil {
		gaia.ErrorF("password not match: %s", err.Error)
		return false
	}
	return true
}
