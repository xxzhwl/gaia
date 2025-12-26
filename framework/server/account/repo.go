// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import "context"

type UserRepo struct {
	dao UserDao
}

func NewUserRepo(ctx context.Context) UserRepo {
	return UserRepo{dao: *NewUserDao(ctx)}
}

func (u UserRepo) FindUserById(id int64) (user UserVo, err error) {
	return u.dao.FindUserById(id)
}

func (u UserRepo) FindUserByUserName(username string) (user UserVo, err error) {
	return u.dao.FindUserByUserName(username)
}

func (u UserRepo) FindUserByMail(mail string) (user UserVo, err error) {
	return u.dao.FindUserByMail(mail)
}

func (u UserRepo) FindUserByPhoneNum(phoneNum string, phoneCode int64) (user UserVo, err error) {
	return u.dao.FindUserByPhoneNum(phoneNum, phoneCode)
}

func (u UserRepo) UpdateUserInfo(user UserBaseVo) (err error) {
	_, err = u.dao.UpdateUserById(UserVo{UserBaseVo: user})
	return
}

func (u UserRepo) UpdateUserPwdById(id int64, pwd string) (err error) {
	password, err := EncryptPassword(pwd)
	if err != nil {
		return
	}
	_, err = u.dao.UpdateUserById(UserVo{UserBaseVo: UserBaseVo{Id: id}, Password: password})
	return
}

func (u UserRepo) AddUser(vo UserVo) (userInfo UserVo, err error) {
	return u.dao.AddUser(vo)
}

type TokenRepo struct {
	dao TokenDao
}

func NewTokenRepo(ctx context.Context) TokenRepo {
	return TokenRepo{dao: *NewTokenDao(ctx)}
}

func (t TokenRepo) AddUserToken(token UserTokenVo) (userToken UserTokenVo, err error) {
	return t.dao.AddUserToken(token)
}

func (t TokenRepo) FindTokenByRefreshToken(refreshToken string) (token UserTokenVo, err error) {
	return t.dao.FindTokenByRefreshToken(refreshToken)
}

func (t TokenRepo) FindActiveTokensByUserId(userId int64) (tokens []UserTokenVo, err error) {
	return t.dao.FindActiveTokensByUserId(userId)
}

func (t TokenRepo) UpdateTokenActiveStatus(tokenId int64, isActive int) (affected int64, err error) {
	return t.dao.UpdateTokenActiveStatus(tokenId, isActive)
}

func (t TokenRepo) UpdateTokenLastActiveTime(tokenId int64) (affected int64, err error) {
	return t.dao.UpdateTokenLastActiveTime(tokenId)
}

func (t TokenRepo) DeleteTokenByRefreshToken(refreshToken string) (affected int64, err error) {
	return t.dao.DeleteTokenByRefreshToken(refreshToken)
}

func (t TokenRepo) DeleteAllTokensByUserId(userId int64) (affected int64, err error) {
	return t.dao.DeleteAllTokensByUserId(userId)
}
