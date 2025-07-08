// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import (
	"context"
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
