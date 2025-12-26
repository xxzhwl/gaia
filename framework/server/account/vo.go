// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import (
	"time"
)

const TUser = "t_user"
const TUserToken = "t_user_token"

// UserVo 用户vo
type UserVo struct {
	Password   string
	CreateTime time.Time `gorm:"autoCreateTime"`
	UpdateTime time.Time `gorm:"autoUpdateTime"`
	UserBaseVo
}

// UserBaseVo 带原始密码的用户vo
type UserBaseVo struct {
	Id              int64   `json:"id"`
	UserName        string  `json:"userName"`
	Mail            string  `json:"mail"`
	PhoneRegionNum  int64   `json:"phoneRegionNum"`
	PhoneNum        string  `json:"phoneNum"`
	IsBan           int     `json:"isBan"`    //被禁
	IsLogOut        int     `json:"isLogOut"` //用户注销
	LogOutTime      *string `json:"logOutTime"`
	CreateTime      string  `json:"createTime"`
	UpdateTime      string  `json:"updateTime"`
	CreateTimeStamp int64   `gorm:"autoCreateTime:milli" json:"createTimeStamp"`
	UpdateTimeStamp int64   `gorm:"autoUpdateTime:milli" json:"updateTimeStamp"`
	LogOutTimeStamp int64   `gorm:"autoCreateTime:milli" json:"logOutTimeStamp"`
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

// DeviceInfoVo 设备信息vo
type DeviceInfoVo struct {
	DeviceType string `json:"deviceType"` // 设备类型
	DeviceId   string `json:"deviceId"`   // 设备唯一标识
	DeviceName string `json:"deviceName"` // 设备名称
	OsVersion  string `json:"osVersion"`  // 操作系统版本
	AppVersion string `json:"appVersion"` // 应用版本号
	IpAddress  string `json:"ipAddress"`  // 登录IP地址
	Location   string `json:"location"`   // 登录地理位置
	UserAgent  string `json:"userAgent"`  // 用户代理信息
}

// UserTokenVo 用户令牌vo
type UserTokenVo struct {
	Id              int64     `json:"id"`
	UserId          int64     `json:"userId"`                           // 用户id
	RefreshToken    string    `json:"refreshToken"`                     // 刷新令牌(加密存储)
	DeviceType      string    `json:"deviceType"`                       // 设备类型
	DeviceId        string    `json:"deviceId"`                         // 设备唯一标识
	DeviceName      string    `json:"deviceName"`                       // 设备名称
	OsVersion       string    `json:"osVersion"`                        // 操作系统版本
	AppVersion      string    `json:"appVersion"`                       // 应用版本号
	IpAddress       string    `json:"ipAddress"`                        // 登录IP地址
	Location        string    `json:"location"`                         // 登录地理位置
	UserAgent       string    `json:"userAgent"`                        // 用户代理信息
	IsActive        int       `json:"isActive"`                         // 是否活跃
	ExpiredTime     time.Time `json:"expiredTime"`                      // 过期时间
	LastActiveTime  time.Time `json:"lastActiveTime"`                   // 最后活跃时间
	CreateTime      time.Time `gorm:"autoCreateTime" json:"createTime"` // 创建时间
	UpdateTime      time.Time `gorm:"autoUpdateTime" json:"updateTime"` // 更新时间
	CreateTimeStamp int64     `gorm:"autoCreateTime:milli" json:"createTimeStamp"`
	UpdateTimeStamp int64     `gorm:"autoUpdateTime:milli" json:"updateTimeStamp"`
}

// UserTokenBaseVo 用户令牌基础vo
type UserTokenBaseVo struct {
	Id              int64  `json:"id"`
	UserId          int64  `json:"userId"`
	RefreshToken    string `json:"refreshToken"`
	DeviceType      string `json:"deviceType"`
	DeviceId        string `json:"deviceId"`
	DeviceName      string `json:"deviceName"`
	OsVersion       string `json:"osVersion"`
	AppVersion      string `json:"appVersion"`
	IpAddress       string `json:"ipAddress"`
	Location        string `json:"location"`
	UserAgent       string `json:"userAgent"`
	IsActive        int    `json:"isActive"`
	ExpiredTime     string `json:"expiredTime"`
	LastActiveTime  string `json:"lastActiveTime"`
	CreateTime      string `json:"createTime"`
	UpdateTime      string `json:"updateTime"`
	CreateTimeStamp int64  `json:"createTimeStamp"`
	UpdateTimeStamp int64  `json:"updateTimeStamp"`
}
