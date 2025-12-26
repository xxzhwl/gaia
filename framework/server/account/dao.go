// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import (
	"context"
	"time"

	"github.com/xxzhwl/gaia"
)

type IUserDao interface {
	FindUserById(id int64) (user UserVo, err error)
	FindUserByUserName(userName string) (user UserVo, err error)
	FindUserByPhoneNum(phoneNum string, phoneCode int64) (user UserVo, err error)
	FindUserByMail(mail string) (user UserVo, err error)
	AddUser(user UserVo) (userInfo UserVo, err error)
	UpdateUserById(user UserVo) (affected int64, err error)
}

type ITokenDao interface {
	AddUserToken(token UserTokenVo) (userToken UserTokenVo, err error)
	FindTokenByRefreshToken(refreshToken string) (token UserTokenVo, err error)
	FindActiveTokensByUserId(userId int64) (tokens []UserTokenVo, err error)
	UpdateTokenActiveStatus(tokenId int64, isActive int) (affected int64, err error)
	UpdateTokenLastActiveTime(tokenId int64) (affected int64, err error)
	DeleteTokenByRefreshToken(refreshToken string) (affected int64, err error)
	DeleteAllTokensByUserId(userId int64) (affected int64, err error)
}

type UserDao struct {
	ctx context.Context
}

func NewUserDao(ctx context.Context) *UserDao {
	return &UserDao{ctx: ctx}
}

func (u UserDao) FindUserByMail(mail string) (user UserVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(u.ctx).Table(TUser).Where("mail = ?", mail).Find(&user)
	if tx.Error != nil {
		err = tx.Error
	}
	return
}

func (u UserDao) FindUserByMailAndNotLogOut(mail string) (user UserVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(u.ctx).Table(TUser).Where("mail = ?", mail).
		Where("is_log_out = ?", 0).Find(&user)
	if tx.Error != nil {
		err = tx.Error
	}
	return
}

func (u UserDao) AddUser(user UserVo) (userInfo UserVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(u.ctx).Table(TUser).Create(&user)
	if tx.Error != nil {
		err = tx.Error
	}
	userInfo = user
	return
}

func (u UserDao) FindUserByPhoneNum(phoneNum string, phoneCode int64) (user UserVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(u.ctx).Table(TUser).Where("phone_num = ? and phone_region_num = ?",
		phoneNum, phoneCode).Find(&user)
	if tx.Error != nil {
		err = tx.Error
	}
	return
}

func (u UserDao) UpdateUserById(user UserVo) (affected int64, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(u.ctx).Table(TUser).Updates(&user).Where("id = ?", user.Id)
	if tx.Error != nil {
		err = tx.Error
	}
	affected = tx.RowsAffected
	return
}

func (u UserDao) FindUserByUserName(userName string) (user UserVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(u.ctx).Table(TUser).Where("user_name = ?", userName).Find(&user)
	if tx.Error != nil {
		err = tx.Error
	}
	return
}

func (u UserDao) FindUserById(id int64) (user UserVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(u.ctx).Table(TUser).Where("id = ?", id).Find(&user)
	if tx.Error != nil {
		err = tx.Error
	}
	return
}

type TokenDao struct {
	ctx context.Context
}

func NewTokenDao(ctx context.Context) *TokenDao {
	return &TokenDao{ctx: ctx}
}

func (t TokenDao) AddUserToken(token UserTokenVo) (userToken UserTokenVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(t.ctx).Table(TUserToken).Create(&token)
	if tx.Error != nil {
		err = tx.Error
	}
	userToken = token
	return
}

func (t TokenDao) FindTokenByRefreshToken(refreshToken string) (token UserTokenVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(t.ctx).Table(TUserToken).Where("refresh_token = ?", refreshToken).Find(&token)
	if tx.Error != nil {
		err = tx.Error
	}
	return
}

func (t TokenDao) FindActiveTokensByUserId(userId int64) (tokens []UserTokenVo, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(t.ctx).Table(TUserToken).Where("user_id = ? and is_active = ?", userId, 1).Find(&tokens)
	if tx.Error != nil {
		err = tx.Error
	}
	return
}

func (t TokenDao) UpdateTokenActiveStatus(tokenId int64, isActive int) (affected int64, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(t.ctx).Table(TUserToken).Where("id = ?", tokenId).Update("is_active", isActive)
	if tx.Error != nil {
		err = tx.Error
	}
	affected = tx.RowsAffected
	return
}

func (t TokenDao) UpdateTokenLastActiveTime(tokenId int64) (affected int64, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(t.ctx).Table(TUserToken).Where("id = ?", tokenId).Update("last_active_time", time.Now())
	if tx.Error != nil {
		err = tx.Error
	}
	affected = tx.RowsAffected
	return
}

func (t TokenDao) DeleteTokenByRefreshToken(refreshToken string) (affected int64, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(t.ctx).Table(TUserToken).Where("refresh_token = ?", refreshToken).Delete(&UserTokenVo{})
	if tx.Error != nil {
		err = tx.Error
	}
	affected = tx.RowsAffected
	return
}

func (t TokenDao) DeleteAllTokensByUserId(userId int64) (affected int64, err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(t.ctx).Table(TUserToken).Where("user_id = ?", userId).Delete(&UserTokenVo{})
	if tx.Error != nil {
		err = tx.Error
	}
	affected = tx.RowsAffected
	return
}
