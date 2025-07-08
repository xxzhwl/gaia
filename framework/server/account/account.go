// Package account 包注释
// @author wanlizhan
// @created 2024-11-29
package account

import (
	"context"
	"errors"
	"fmt"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/redis"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/server"
	"strconv"
	"strings"
	"time"
)

const (
	LoginMailKey     = "Login-Mail-%s"
	RegisterMailKey  = "Register-Mail-%s"
	LoginPhoneKey    = "Login-Phone-%s"
	RegisterPhoneKey = "Register-Phone-%s"
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
		To:         []string{mailBox},
		Subject:    "登录验证码",
		Body:       fmt.Sprintf("您的登录验证码是%s\n", code),
		Attachment: nil,
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
	token, err := server.NewJwtAuth(jwtConf).
		GenerateToken(strconv.FormatInt(userInfo.Id, 10))
	if err != nil {
		return
	}
	//生成refreshToken
	refreshConf := server.NewJwtConf("JwtRefreshConf")
	refreshToken, err := server.NewJwtAuth(refreshConf).
		GenerateRefreshToken(strconv.FormatInt(userInfo.Id, 10))
	if err != nil {
		return
	}

	client := redis.NewFrameworkClient().WithCtx(req.Ctx)
	if err = client.SetEx(fmt.Sprintf("token-%s", token), userInfo.Id, time.Duration(jwtConf.DurationMinute)*time.
		Minute); err != nil {
		return
	}
	if err = client.SetEx(fmt.Sprintf("refreshToken-%s", refreshToken), userInfo.Id,
		time.Duration(refreshConf.DurationMinute)*time.Minute); err != nil {
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
type RegisterRequest LoginRequest

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
	token, err := server.NewJwtAuth(jwtConf).
		GenerateToken(strconv.FormatInt(userInfo.Id, 10))
	if err != nil {
		return
	}
	//生成refreshToken
	refreshConf := server.NewJwtConf("JwtRefreshConf")
	refreshToken, err := server.NewJwtAuth(refreshConf).
		GenerateRefreshToken(strconv.FormatInt(userInfo.Id, 10))
	if err != nil {
		return
	}

	client := redis.NewFrameworkClient().WithCtx(req.Ctx)
	if err = client.SetEx(fmt.Sprintf("token-%s", token), userInfo.Id, time.Duration(jwtConf.DurationMinute)*time.
		Minute); err != nil {
		return
	}
	if err = client.SetEx(fmt.Sprintf("refreshToken-%s", refreshToken), userInfo.Id,
		time.Duration(refreshConf.DurationMinute)*time.Minute); err != nil {
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
	if userVo.Id != 0 {
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
	if userVo.Id != 0 {
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
	UserName     string `memo:"用户名" require:"1" json:"userName"`
	Ctx          context.Context
}

type RefreshTokenResponse struct {
	NewToken string `json:"newToken"`
}

func (a Account) RefreshToken(request RefreshTokenRequest) (resp RefreshTokenResponse, err error) {
	//1.从Token中获取对应的用户Id
	refreshAuthUtil := server.NewJwtAuth(server.NewJwtConf("JwtRefreshConf"))
	ck, err := refreshAuthUtil.GetCk(request.RefreshToken)
	if err != nil {
		return
	}
	uid := server.GetUserKeyFromCk(ck)
	if len(uid) == 0 {
		err = errors.New("refreshToken不符合预期")
		return
	}
	//2.根据用户名查出对应的用户Id，先对比是否是一个用户
	user, err := NewUserRepo(request.Ctx).FindUserByUserName(request.UserName)
	if err != nil {
		return
	}
	//3.如果不是就要报错
	if strconv.FormatInt(user.Id, 10) != uid {
		err = errors.New("refreshToken不符合预期")
		return
	}
	if user.IsBan == 1 {
		err = fmt.Errorf("用户名%s被封禁", request.UserName)
		return
	}
	if user.IsLogOut == 1 {
		err = fmt.Errorf("用户名%s已注销", request.UserName)
		return
	}
	//4.如果是，返回新Token
	jwtConf := server.NewJwtConf("JwtConf")
	authUtil := server.NewJwtAuth(jwtConf)
	token, err := authUtil.GenerateToken(strconv.FormatInt(user.Id, 10))
	if err != nil {
		return
	}
	client := redis.NewFrameworkClient().WithCtx(request.Ctx)
	if err = client.SetEx(fmt.Sprintf("token-%s", token), uid, time.Duration(jwtConf.DurationMinute)*time.
		Minute); err != nil {
		return
	}

	return RefreshTokenResponse{token}, nil
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
