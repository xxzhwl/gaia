// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import (
	"time"
)

const TUser = "t_user"

// UserVo 用户vo
type UserVo struct {
	Password   string
	CreateTime time.Time `gorm:"autoCreateTime"`
	UpdateTime time.Time `gorm:"autoUpdateTime"`
	UserBaseVo
}

// UserBaseVo 带原始密码的用户vo
type UserBaseVo struct {
	Id              int64  `json:"id"`
	UserName        string `json:"userName"`
	Mail            string `json:"mail"`
	PhoneRegionNum  int64  `json:"phoneRegionNum"`
	PhoneNum        string `json:"phoneNum"`
	IsBan           int    `json:"isBan"`    //被禁
	IsLogOut        int    `json:"isLogOut"` //用户注销
	LogOutTime      string `json:"logOutTime"`
	CreateTime      string `json:"createTime"`
	UpdateTime      string `json:"updateTime"`
	CreateTimeStamp int64  `gorm:"autoCreateTime:milli" json:"createTimeStamp"`
	UpdateTimeStamp int64  `gorm:"autoUpdateTime:milli" json:"updateTimeStamp"`
	LogOutTimeStamp int64  `gorm:"autoCreateTime:milli" json:"logOutTimeStamp"`
}

// GetBaseFromVo 获取用户基本信息，用于脱敏密码
func GetBaseFromVo(userVo UserVo) UserBaseVo {
	return UserBaseVo{
		Id:              userVo.Id,
		UserName:        userVo.UserName,
		Mail:            userVo.Mail,
		PhoneRegionNum:  userVo.PhoneRegionNum,
		PhoneNum:        userVo.PhoneNum,
		IsBan:           userVo.IsBan,
		IsLogOut:        userVo.IsLogOut,
		LogOutTime:      userVo.LogOutTime,
		CreateTime:      userVo.CreateTime.Format(time.DateTime),
		UpdateTime:      userVo.UpdateTime.Format(time.DateTime),
		CreateTimeStamp: userVo.CreateTimeStamp,
		UpdateTimeStamp: userVo.UpdateTimeStamp,
		LogOutTimeStamp: userVo.LogOutTimeStamp,
	}
}
