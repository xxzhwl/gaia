// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/redis"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/server"
)

const (
	LoginMailKey     = "Login-Mail-%s"
	RegisterMailKey  = "Register-Mail-%s"
	LoginPhoneKey    = "Login-Phone-%s"
	RegisterPhoneKey = "Register-Phone-%s"

	CurrentLoginUserNumKey = "CurrentLoginUserNum"
)

type Account struct {
}

/**
发送验证码
这里登录和注册都可以发送验证码，并且验证码一般发给手机或者邮箱，我们这里实现一下邮箱的验证码逻辑
*/

type SendCodeRequest struct {
	CodeType    int    `memo:"验证码类型(1.登录邮箱使用;2.注册邮箱使用;3.登录手机号使用;4.注册手机号使用)"  require:"1" json:"codeType"`
	Destination string `memo:"目标：邮箱或者手机号" require:"1" validator:"Mail" json:"destination"`
}

func (a Account) SendCode(req SendCodeRequest) (err error) {
	//不管是登录还是注册，都要看一下是否有该用户
	//根据类型生成对应的key和code
	code := gaia.GetRandString(6)
	key := ""
	switch req.CodeType {
	case 1:
		user, err := UserRepo{}.FindUserByMail(req.Destination)
		if err != nil {
			return err
		}
		if user.Id == 0 {
			return fmt.Errorf("%s未注册", req.Destination)
		}
		key = fmt.Sprintf(LoginMailKey, req.Destination)
		err = sendMailCode(req.Destination, code)
	case 2:
		user, err := UserRepo{}.FindUserByMail(req.Destination)
		if err != nil {
			return err
		}
		if user.Id != 0 {
			return fmt.Errorf("%s已注册", req.Destination)
		}
		key = fmt.Sprintf(RegisterMailKey, req.Destination)
		err = sendMailCode(req.Destination, code)
	case 3:
		split := strings.Split(req.Destination, "-")
		if len(split) != 2 {
			return errors.New("手机号码格式不正确")
		}
		phoneCode, err := strconv.Atoi(split[0])
		if err != nil {
			return errors.New("地区格式不正确")
		}
		user, err := UserRepo{}.FindUserByPhoneNum(split[1], int64(phoneCode))
		if err != nil {
			return err
		}
		if user.Id == 0 {
			return fmt.Errorf("%s未注册", req.Destination)
		}
		key = fmt.Sprintf(LoginPhoneKey, req.Destination)
		err = sendPhoneCode(req.Destination, code)
	case 4:
		split := strings.Split(req.Destination, "-")
		if len(split) != 2 {
			return errors.New("手机号码格式不正确")
		}
		phoneCode, err := strconv.Atoi(split[0])
		if err != nil {
			return errors.New("地区格式不正确")
		}
		user, err := UserRepo{}.FindUserByPhoneNum(split[1], int64(phoneCode))
		if err != nil {
			return err
		}
		if user.Id != 0 {
			return fmt.Errorf("%s已注册", req.Destination)
		}
		key = fmt.Sprintf(RegisterPhoneKey, req.Destination)
		err = sendPhoneCode(req.Destination, code)
	default:
		err = errors.New("错误的验证码发送方式")
		return
	}
	//成功则缓存数据
	client := redis.NewFrameworkClient()
	return client.Set(key, code, time.Minute*time.Duration(
		gaia.GetSafeConfInt64WithDefault("RedisAccountCodeDuration", 1)))
}

func sendMailCode(mailBox, code string) error {
	mail, err := gaia.NewDefaultMailConf()
	if err != nil {
		return err
	}

	return mail.SendMail(gaia.MailMessage{
		To:          []string{mailBox},
		Subject:     "登录验证码",
		Body:        fmt.Sprintf("您的登录验证码是%s\n", code),
		Attachments: nil,
	})
}

func sendPhoneCode(phone, code string) error {
	return nil
}

/**
登录
*/

// LoginRequest
// 登录有很多种方式，我们支持最简单的即可
// 1.用户名+密码
// 2.手机号+验证码
// 3.邮箱+验证码
type LoginRequest struct {
	IdentityType int    `memo:"验证方式(1:用户名+密码;2:手机号+验证码;3:邮箱+验证码)" require:"1" range:"1,2,3" json:"identityType"`
	Identity     string `memo:"身份验证:用户名、手机号（区号+手机号）、邮箱" require:"1" json:"identity"`
	Secret       string `memo:"登录验证密钥:密码、验证码" require:"1" json:"secret"`

	// 设备信息
	DeviceType string `memo:"设备类型[web:网页端;ios:iOS;android:安卓;小程序:mini_program]" json:"deviceType"`
	DeviceId   string `memo:"设备唯一标识" json:"deviceId"`
	DeviceName string `memo:"设备名称(如:iPhone 14 Pro)" json:"deviceName"`
	OsVersion  string `memo:"操作系统版本" json:"osVersion"`
	AppVersion string `memo:"应用版本号" json:"appVersion"`
	IpAddress  string `memo:"登录IP地址" json:"ipAddress"`
	Location   string `memo:"登录地理位置" json:"location"`
	UserAgent  string `memo:"用户代理信息" json:"userAgent"`

	Ctx context.Context
}

// LoginResponse 登录返回信息包括Token和用户信息
type LoginResponse struct {
	RefreshToken string     `json:"refreshToken"`
	Token        string     `json:"token"`
	UserInfo     UserBaseVo `json:"userInfo"`
}

func (a Account) Login(req LoginRequest) (resp LoginResponse, err error) {
	userInfo := UserBaseVo{}
	switch req.IdentityType {
	case 1:
		userInfo, err = loginByPwd(req.Ctx, req.Identity, req.Secret)
	case 2:
		userInfo, err = loginByPhoneCode(req.Ctx, req.Identity, req.Secret)
	case 3:
		userInfo, err = loginByMailCode(req.Ctx, req.Identity, req.Secret)
	default:
		err = errors.New("错误的登录方式")
		return
	}
	if err != nil {
		return
	}

	//比对成功,生成Token返回信息
	jwtConf := server.NewJwtConf("JwtConf")
	token, refreshToken, err := server.NewJwtAuth(jwtConf).
		GenerateToken(strconv.FormatInt(userInfo.Id, 10))
	if err != nil {
		return
	}

	// 持久化 RefreshToken 到数据库
	tokenRepo := NewTokenRepo(req.Ctx)
	now := time.Now()
	expiredTime := now.Add(time.Duration(jwtConf.RefreshTokenDurationMinute) * time.Minute) // 默认7天过期
	tokenVo := UserTokenVo{
		UserId:         userInfo.Id,
		RefreshToken:   refreshToken,
		DeviceType:     req.DeviceType,
		DeviceId:       req.DeviceId,
		DeviceName:     req.DeviceName,
		OsVersion:      req.OsVersion,
		AppVersion:     req.AppVersion,
		IpAddress:      req.IpAddress,
		Location:       req.Location,
		UserAgent:      req.UserAgent,
		IsActive:       1,
		ExpiredTime:    expiredTime,
		LastActiveTime: now,
	}
	if _, err = tokenRepo.AddUserToken(tokenVo); err != nil {
		return
	}

	client := redis.NewFrameworkClient().WithCtx(req.Ctx)
	if _, err = client.Incr(CurrentLoginUserNumKey); err != nil {
		return
	}
	return LoginResponse{
		RefreshToken: refreshToken,
		Token:        token,
		UserInfo:     userInfo,
	}, nil
}

func loginByPwd(ctx context.Context, userName, pwd string) (user UserBaseVo, err error) {
	//根据用户名查找用户
	userVo, err := NewUserRepo(ctx).FindUserByUserName(userName)
	if err != nil {
		return
	}
	//没查到
	if userVo.Id == 0 {
		err = fmt.Errorf("根据用户名%s未查找到相关用户信息", userName)
		return
	}
	if userVo.IsBan == 1 {
		err = fmt.Errorf("用户名%s被封禁", userName)
		return
	}
	if userVo.IsLogOut == 1 {
		err = fmt.Errorf("用户名%s已注销", userName)
		return
	}
	//查到了对比密码
	matchRes := PasswordMatch(pwd, userVo.Password)
	if !matchRes {
		err = errors.New("用户名或密码错误")
		return
	}

	return GetBaseFromVo(userVo), nil
}

func loginByPhoneCode(ctx context.Context, phone, code string) (user UserBaseVo, err error) {
	split := strings.Split(phone, "-")
	if len(split) != 2 {
		return user, errors.New("手机号码格式不正确")
	}
	loc := split[0]
	locCode, err := strconv.Atoi(loc)
	if err != nil {
		return user, errors.New("手机号码格式不正确")
	}
	phone = split[1]

	//拿到phone拼起key
	key := fmt.Sprintf(LoginPhoneKey, phone)
	client := redis.NewFrameworkClient()
	storeCode, err := client.GetString(key)
	if err != nil {
		return
	}
	if storeCode != code {
		return user, errors.New("验证码无效")
	}
	//根据手机号查找用户
	userVo, err := NewUserRepo(ctx).FindUserByPhoneNum(phone, int64(locCode))
	if err != nil {
		return
	}
	//没查到
	if userVo.Id == 0 {
		err = fmt.Errorf("根据手机号%s未查找到相关用户信息", phone)
		return
	}
	if userVo.IsBan == 1 {
		err = fmt.Errorf("用户%s被封禁", phone)
		return
	}
	if userVo.IsLogOut == 1 {
		err = fmt.Errorf("用户%s已注销", phone)
		return
	}
	//一切顺利 说明ok
	return GetBaseFromVo(userVo), client.Del(key)
}

func loginByMailCode(ctx context.Context, mailBox, code string) (user UserBaseVo, err error) {
	//拿到phone拼起key
	key := fmt.Sprintf(LoginMailKey, mailBox)
	client := redis.NewFrameworkClient()
	storeCode, err := client.GetString(key)
	if err != nil {
		return
	}
	if storeCode != code {
		return user, errors.New("验证码无效")
	}
	//根据邮箱查找用户
	userVo, err := NewUserRepo(ctx).FindUserByMail(mailBox)
	if err != nil {
		return
	}
	//没查到
	if userVo.Id == 0 {
		err = fmt.Errorf("根据邮箱%s未查找到相关用户信息", mailBox)
		return
	}
	if userVo.IsBan == 1 {
		err = fmt.Errorf("用户%s被封禁", mailBox)
		return
	}
	if userVo.IsLogOut == 1 {
		err = fmt.Errorf("用户%s已注销", mailBox)
		return
	}
	//一切顺利 说明ok
	return GetBaseFromVo(userVo), client.Del(key)
}

/**
注册
*/

// RegisterRequest
// 注册请求,注册我们一般采用用户名+密码，或者手机号+验证码，或者邮箱+验证码
type RegisterRequest struct {
	IdentityType int    `memo:"验证方式(1:用户名+密码;2:手机号+验证码;3:邮箱+验证码)" require:"1" range:"1,2,3" json:"identityType"`
	Identity     string `memo:"身份验证:用户名、手机号（区号+手机号）、邮箱" require:"1" json:"identity"`
	Secret       string `memo:"登录验证密钥:密码、验证码" require:"1" json:"secret"`

	// 设备信息
	DeviceType string `memo:"设备类型[web:网页端;ios:iOS;android:安卓;小程序:mini_program]" json:"deviceType"`
	DeviceId   string `memo:"设备唯一标识" json:"deviceId"`
	DeviceName string `memo:"设备名称(如:iPhone 14 Pro)" json:"deviceName"`
	OsVersion  string `memo:"操作系统版本" json:"osVersion"`
	AppVersion string `memo:"应用版本号" json:"appVersion"`
	IpAddress  string `memo:"登录IP地址" json:"ipAddress"`
	Location   string `memo:"登录地理位置" json:"location"`
	UserAgent  string `memo:"用户代理信息" json:"userAgent"`

	Ctx context.Context
}

type RegisterResponse LoginResponse

// Register 注册
func (a Account) Register(req RegisterRequest) (resp RegisterResponse, err error) {
	userInfo := UserBaseVo{}
	switch req.IdentityType {
	case 1:
		userInfo, err = registerByPwd(req.Ctx, req.Identity, req.Secret)
	case 2:
		userInfo, err = registerByPhoneCode(req.Ctx, req.Identity, req.Secret)
	case 3:
		userInfo, err = registerByMailCode(req.Ctx, req.Identity, req.Secret)
	default:
		err = errors.New("错误的注册方式")
		return
	}
	if err != nil {
		return
	}

	jwtConf := server.NewJwtConf("JwtConf")
	token, refreshToken, err := server.NewJwtAuth(jwtConf).
		GenerateToken(strconv.FormatInt(userInfo.Id, 10))
	if err != nil {
		return
	}

	// 持久化 RefreshToken 到数据库
	tokenRepo := NewTokenRepo(req.Ctx)
	now := time.Now()
	expiredTime := now.Add(time.Duration(jwtConf.RefreshTokenDurationMinute) * time.Minute) // 默认7天过期
	tokenVo := UserTokenVo{
		UserId:         userInfo.Id,
		RefreshToken:   refreshToken,
		DeviceType:     req.DeviceType,
		DeviceId:       req.DeviceId,
		DeviceName:     req.DeviceName,
		OsVersion:      req.OsVersion,
		AppVersion:     req.AppVersion,
		IpAddress:      req.IpAddress,
		Location:       req.Location,
		UserAgent:      req.UserAgent,
		IsActive:       1,
		ExpiredTime:    expiredTime,
		LastActiveTime: now,
	}
	if _, err = tokenRepo.AddUserToken(tokenVo); err != nil {
		return
	}

	client := redis.NewFrameworkClient().WithCtx(req.Ctx)
	if _, err = client.Incr(CurrentLoginUserNumKey); err != nil {
		return
	}
	return RegisterResponse{
		UserInfo:     userInfo,
		Token:        token,
		RefreshToken: refreshToken,
	}, nil

}

func registerByMailCode(ctx context.Context, mailBox string, code string) (user UserBaseVo, err error) {
	//拿到phone拼起key
	key := fmt.Sprintf(RegisterMailKey, mailBox)
	client := redis.NewFrameworkClient()
	storeCode, err := client.GetString(key)
	if err != nil {
		return
	}
	gaia.InfoF("redis:%s-%s", key, storeCode)
	if storeCode != code {
		return user, errors.New("验证码无效")
	}
	//根据邮箱查找用户
	userVo, err := NewUserRepo(ctx).FindUserByMail(mailBox)
	if err != nil {
		return
	}
	//查到
	if userVo.Id != 0 && userVo.IsLogOut == 0 {
		err = fmt.Errorf("该邮箱%s已绑定用户信息", mailBox)
		return
	}
	//没查到就注册一个
	userInfo, err := NewUserRepo(ctx).AddUser(UserVo{
		UserBaseVo: UserBaseVo{
			UserName: gaia.GetRandString(9),
			Mail:     mailBox,
		},
	})
	if err != nil {
		return
	}

	//一切顺利 说明ok
	return GetBaseFromVo(userInfo), client.Del(key)
}

func registerByPhoneCode(ctx context.Context, phone string, code string) (user UserBaseVo, err error) {
	split := strings.Split(phone, "-")
	if len(split) != 2 {
		return user, errors.New("手机号码格式不正确")
	}
	loc := split[0]
	locCode, err := strconv.Atoi(loc)
	if err != nil {
		return user, errors.New("手机号码格式不正确")
	}
	phone = split[1]

	//拿到phone拼起key
	key := fmt.Sprintf(LoginPhoneKey, phone)
	client := redis.NewFrameworkClient()
	storeCode, err := client.GetString(key)
	if err != nil {
		return
	}
	if storeCode != code {
		return user, errors.New("验证码无效")
	}
	//根据手机号查找用户
	userVo, err := NewUserRepo(ctx).FindUserByPhoneNum(phone, int64(locCode))
	if err != nil {
		return
	}
	//查到说明绑定过
	if userVo.Id != 0 && userVo.IsLogOut == 0 {
		err = fmt.Errorf("手机号%s已绑定用户", phone)
		return
	}
	//没查到就注册一个
	userInfo, err := NewUserRepo(ctx).AddUser(UserVo{
		UserBaseVo: UserBaseVo{
			UserName:       gaia.GetRandString(9),
			PhoneRegionNum: int64(locCode),
			PhoneNum:       phone,
		},
	})
	if err != nil {
		return
	}

	//一切顺利 说明ok
	return GetBaseFromVo(userInfo), client.Del(key)
}

func registerByPwd(ctx context.Context, identity, secret string) (UserBaseVo, error) {
	repo := NewUserRepo(ctx)
	//首先去查是否有该账号已被注册
	//如果有了就返回错误
	//如果没有才可以注册
	//注册完毕可以直接返回新增的用户信息
	user, err := repo.FindUserByUserName(identity)
	if err != nil {
		return UserBaseVo{}, err
	}
	//说明已注册注册
	if user.Id != 0 {
		return UserBaseVo{}, errors.New("该用户名已注册")
	}
	storePwd, err := EncryptPassword(secret)
	if err != nil {
		return UserBaseVo{}, err
	}
	userInfo, err := repo.AddUser(UserVo{
		Password: storePwd,
		UserBaseVo: UserBaseVo{
			UserName: identity,
		},
	})
	return GetBaseFromVo(userInfo), err
}

/**
刷新Token
*/

type RefreshTokenRequest struct {
	RefreshToken string `memo:"刷新Token" require:"1" json:"refreshToken"`
	DeviceId     string `memo:"设备唯一标识" require:"1" json:"deviceId"`
	Ctx          context.Context
}

type RefreshTokenResponse struct {
	NewToken     string `json:"newToken"`
	RefreshToken string `json:"refreshToken"`
}

func (a Account) RefreshToken(request RefreshTokenRequest) (resp RefreshTokenResponse, err error) {
	//1.从Token中获取对应的用户Id
	refreshAuthUtil := server.NewJwtAuth(server.NewJwtConf("JwtConf"))
	uid, err := refreshAuthUtil.GetCk(request.RefreshToken)
	if err != nil {
		if errwrap.GetCode(err) == errwrap.EcAuthErr {
			// refresh token过期，返回特定错误码
			err = errwrap.Error(20002, errors.New("refresh token expired"))
		}
		return
	}
	if len(uid) == 0 {
		err = errors.New("refreshToken不符合预期")
		return
	}

	//2.验证 RefreshToken 是否存在于数据库中且有效
	tokenRepo := NewTokenRepo(request.Ctx)
	tokenRecord, err := tokenRepo.FindTokenByRefreshToken(request.RefreshToken)
	if err != nil {
		return
	}

	// 检查 token 是否存在且活跃
	if tokenRecord.Id == 0 {
		err = errors.New("refreshToken无效或已失效")
		return
	}
	if tokenRecord.IsActive != 1 {
		err = errors.New("refreshToken已失效")
		return
	}

	// 验证 token 是否匹配设备
	if request.DeviceId != "" && tokenRecord.DeviceId != "" && tokenRecord.DeviceId != request.DeviceId {
		err = errors.New("设备不匹配")
		return
	}

	//3.检查 token 是否过期
	if time.Now().After(tokenRecord.ExpiredTime) {
		err = errwrap.Error(20002, errors.New("refreshToken已过期"))
		return
	}

	//4.根据用户Id查出用户信息，验证用户状态
	userId, _ := strconv.ParseInt(uid, 10, 64)
	user, err := NewUserRepo(request.Ctx).FindUserById(userId)
	if err != nil {
		return
	}

	// 验证 token 中的 userId 是否与数据库中的 userId 一致
	if user.Id != tokenRecord.UserId {
		err = errors.New("refreshToken不符合预期")
		return
	}

	if user.IsBan == 1 {
		err = fmt.Errorf("用户%s被封禁", user.UserName)
		return
	}
	if user.IsLogOut == 1 {
		err = fmt.Errorf("用户%s已注销", user.UserName)
		return
	}

	//5.生成新的 Token 和 RefreshToken
	jwtConf := server.NewJwtConf("JwtConf")
	authUtil := server.NewJwtAuth(jwtConf)
	newToken, newRefreshToken, err := authUtil.GenerateToken(strconv.FormatInt(user.Id, 10))
	if err != nil {
		return
	}

	//6.更新数据库中的 RefreshToken（删除旧的，添加新的）
	if _, err = tokenRepo.DeleteTokenByRefreshToken(request.RefreshToken); err != nil {
		return
	}

	// 添加新的 RefreshToken 记录
	now := time.Now()
	expiredTimeNew := now.Add(time.Duration(jwtConf.RefreshTokenDurationMinute) * time.Minute) // 默认7天过期
	newTokenVo := UserTokenVo{
		UserId:         user.Id,
		RefreshToken:   newRefreshToken,
		DeviceType:     tokenRecord.DeviceType,
		DeviceId:       tokenRecord.DeviceId,
		DeviceName:     tokenRecord.DeviceName,
		OsVersion:      tokenRecord.OsVersion,
		AppVersion:     tokenRecord.AppVersion,
		IpAddress:      tokenRecord.IpAddress,
		Location:       tokenRecord.Location,
		UserAgent:      tokenRecord.UserAgent,
		IsActive:       1,
		ExpiredTime:    expiredTimeNew,
		LastActiveTime: now,
	}
	if _, err = tokenRepo.AddUserToken(newTokenVo); err != nil {
		return
	}

	return RefreshTokenResponse{
		NewToken:     newToken,
		RefreshToken: newRefreshToken,
	}, nil
}

// LogOutAccountRequest 注销账户参数
type LogOutAccountRequest struct {
	UserId int64  `json:"userId" require:"1" memo:"用户id"`
	Token  string `json:"token" require:"1" memo:"token"`
	Ctx    context.Context
}

// LogOutAccount 注销账户
func (a Account) LogOutAccount(req LogOutAccountRequest) (err error) {
	repo := NewUserRepo(req.Ctx)
	logOutTime := gaia.Date(gaia.DateTimeFormat)
	u := UserBaseVo{Id: req.UserId, IsLogOut: 1, LogOutTime: &logOutTime,
		LogOutTimeStamp: time.Now().UnixMilli()}
	if errTemp := repo.UpdateUserInfo(u); errTemp != nil {
		return errTemp
	}
	return
}

// ExitAccountRequest 退出账户参数
type ExitAccountRequest struct {
	RefreshToken string `json:"refreshToken" require:"1" memo:"刷新Token"`
	DeviceId     string `json:"deviceId" require:"1" memo:"设备唯一标识"`
	Ctx          context.Context
}

// ExitAccount 退出账户（仅退出当前设备）
func (a Account) ExitAccount(req ExitAccountRequest) (err error) {
	tokenRepo := NewTokenRepo(req.Ctx)

	// 根据 RefreshToken 删除对应的 token 记录
	if _, err = tokenRepo.DeleteTokenByRefreshToken(req.RefreshToken); err != nil {
		return
	}

	return nil
}

// GetLoginDevicesRequest 获取登录设备列表参数
type GetLoginDevicesRequest struct {
	UserId int64 `json:"userId" require:"1" memo:"用户id"`
	Ctx    context.Context
}

// GetLoginDevicesResponse 获取登录设备列表响应
type GetLoginDevicesResponse struct {
	Devices []UserTokenBaseVo `json:"devices"`
}

// GetLoginDevices 获取用户的登录设备列表
func (a Account) GetLoginDevices(req GetLoginDevicesRequest) (resp GetLoginDevicesResponse, err error) {
	tokenRepo := NewTokenRepo(req.Ctx)
	tokens, err := tokenRepo.FindActiveTokensByUserId(req.UserId)
	if err != nil {
		return
	}

	devices := make([]UserTokenBaseVo, 0, len(tokens))
	for _, token := range tokens {
		devices = append(devices, UserTokenBaseVo{
			Id:             token.Id,
			UserId:         token.UserId,
			DeviceType:     token.DeviceType,
			DeviceId:       token.DeviceId,
			DeviceName:     token.DeviceName,
			OsVersion:      token.OsVersion,
			AppVersion:     token.AppVersion,
			IpAddress:      token.IpAddress,
			Location:       token.Location,
			UserAgent:      token.UserAgent,
			LastActiveTime: token.LastActiveTime.Format(time.DateTime),
			CreateTime:     token.CreateTime.Format(time.DateTime),
			UpdateTime:     token.UpdateTime.Format(time.DateTime),
		})
	}

	return GetLoginDevicesResponse{Devices: devices}, nil
}

// ForceLogoutDeviceRequest 强制下线设备参数
type ForceLogoutDeviceRequest struct {
	TokenId int64 `json:"tokenId" require:"1" memo:"token记录id"`
	Ctx     context.Context
}

// ForceLogoutDevice 强制下线指定设备
func (a Account) ForceLogoutDevice(req ForceLogoutDeviceRequest) (err error) {
	tokenRepo := NewTokenRepo(req.Ctx)
	if _, err = tokenRepo.UpdateTokenActiveStatus(req.TokenId, 0); err != nil {
		return
	}
	return nil
}

// ForceLogoutAllDevicesRequest 强制下线所有设备参数
type ForceLogoutAllDevicesRequest struct {
	UserId int64 `json:"userId" require:"1" memo:"用户id"`
	Ctx    context.Context
}

// ForceLogoutAllDevices 强制下线用户的所有设备
func (a Account) ForceLogoutAllDevices(req ForceLogoutAllDevicesRequest) (err error) {
	tokenRepo := NewTokenRepo(req.Ctx)
	if _, err = tokenRepo.DeleteAllTokensByUserId(req.UserId); err != nil {
		return
	}
	return nil
}

func SuperAdmin(arg server.Request) bool {
	user, err := GetCurrentUser(arg)
	if err != nil {
		return false
	}
	if !IsSuperAdminByUserName(user.UserName) {
		return false
	}
	return true
}

func IsSuperAdminByUserName(userName string) bool {
	return userName == gaia.GetSafeConfString("SuperAdmin")
}

func GetCurrentUser(arg server.Request) (UserVo, error) {
	s := arg.C().GetString("userKey")
	if len(s) == 0 {
		return UserVo{}, errwrap.Newf("", errwrap.EcMajorMinErr, "权限校验失败")
	}
	atoi, err2 := strconv.Atoi(s)
	if err2 != nil {
		return UserVo{}, errwrap.Newf("", errwrap.EcMajorMinErr, "权限校验失败")
	}
	return NewUserRepo(arg.TraceContext).FindUserById(int64(atoi))
}
