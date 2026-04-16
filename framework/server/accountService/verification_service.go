package accountService

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"gorm.io/gorm"
)

// VerificationService 验证码服务
type VerificationService struct {
	db *gorm.DB
}

// NewVerificationService 创建验证码服务实例
func NewVerificationService(db *gorm.DB) *VerificationService {
	return &VerificationService{db: db}
}

// GenerateCode 生成验证码
func (s *VerificationService) GenerateCode(target, codeType string, expireMinutes int) (string, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if target == "" {
		return "", errwrap.Error(400, errors.New("目标不能为空"))
	}

	// 验证验证码类型
	validTypes := []string{VerificationTypeRegister, VerificationTypeLogin, VerificationTypeResetPass, VerificationTypeBind}
	if !contains(validTypes, codeType) {
		return "", errwrap.Error(400, errors.New("验证码类型不合法"))
	}

	// 检查是否在短时间内已发送过验证码
	var recentCode VerificationCode
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	if err := s.db.WithContext(ctx).Where("target = ? AND type = ? AND created_at > ?", target, codeType, fiveMinutesAgo).First(&recentCode).Error; err == nil {
		return "", errwrap.Error(400, errors.New("验证码发送过于频繁，请稍后再试"))
	}

	// 生成6位数字验证码
	code, err := generateNumericCode(6)
	if err != nil {
		return "", errwrap.Error(500, fmt.Errorf("生成验证码失败: %w", err))
	}

	// 设置过期时间
	expiresAt := time.Now().Add(time.Duration(expireMinutes) * time.Minute)

	// 保存验证码到数据库
	verificationCode := VerificationCode{
		Target:    target,
		Code:      code,
		Type:      codeType,
		ExpiresAt: expiresAt,
	}

	if err := s.db.WithContext(ctx).Create(&verificationCode).Error; err != nil {
		return "", errwrap.Error(500, fmt.Errorf("保存验证码失败: %w", err))
	}

	// TODO: 实际项目中这里应该调用短信或邮件服务发送验证码
	// 这里只是返回生成的验证码，实际生产环境不应该返回

	return code, nil
}

// VerifyCode 验证验证码
func (s *VerificationService) VerifyCode(target, code, codeType string) (bool, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if target == "" || code == "" {
		return false, errwrap.Error(400, errors.New("目标和验证码不能为空"))
	}

	// 查找未使用的验证码
	var verificationCode VerificationCode
	if err := s.db.WithContext(ctx).Where("target = ? AND code = ? AND type = ? AND used = ?",
		target, code, codeType, 0).First(&verificationCode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, errwrap.Error(400, errors.New("验证码错误或已过期"))
		}
		return false, errwrap.Error(500, fmt.Errorf("查询验证码失败: %w", err))
	}

	// 检查验证码是否已过期
	if verificationCode.ExpiresAt.Before(time.Now()) {
		// 标记为已使用
		s.db.WithContext(ctx).Model(&verificationCode).Update("used", 1)
		return false, errwrap.Error(400, errors.New("验证码已过期"))
	}

	// 标记验证码为已使用
	if err := s.db.WithContext(ctx).Model(&verificationCode).Update("used", 1).Error; err != nil {
		return false, errwrap.Error(500, fmt.Errorf("更新验证码状态失败: %w", err))
	}

	return true, nil
}

// CleanExpiredCodes 清理过期的验证码
func (s *VerificationService) CleanExpiredCodes() (int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	result := s.db.WithContext(ctx).Where("expires_at < ? OR used = ?", time.Now(), 1).Delete(&VerificationCode{})
	if result.Error != nil {
		return 0, errwrap.Error(500, fmt.Errorf("清理过期验证码失败: %w", result.Error))
	}

	return result.RowsAffected, nil
}

// GetCodeStats 获取验证码统计信息
func (s *VerificationService) GetCodeStats() (map[string]interface{}, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	stats := make(map[string]interface{})

	// 获取总验证码数
	var totalCodes int64
	if err := s.db.WithContext(ctx).Model(&VerificationCode{}).Count(&totalCodes).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取总验证码数失败: %w", err))
	}
	stats["total_codes"] = totalCodes

	// 获取已使用的验证码数
	var usedCodes int64
	if err := s.db.WithContext(ctx).Model(&VerificationCode{}).Where("used = ?", 1).Count(&usedCodes).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取已使用验证码数失败: %w", err))
	}
	stats["used_codes"] = usedCodes

	// 获取过期的验证码数
	var expiredCodes int64
	if err := s.db.WithContext(ctx).Model(&VerificationCode{}).Where("expires_at < ?", time.Now()).Count(&expiredCodes).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取过期验证码数失败: %w", err))
	}
	stats["expired_codes"] = expiredCodes

	// 按类型统计
	var typeStats []struct {
		Type  string
		Count int64
	}
	if err := s.db.WithContext(ctx).Model(&VerificationCode{}).
		Select("type, COUNT(*) as count").
		Group("type").
		Scan(&typeStats).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("按类型统计失败: %w", err))
	}
	stats["type_stats"] = typeStats

	return stats, nil
}

// OAuthService 第三方登录服务
type OAuthService struct {
	db *gorm.DB
}

// BindOAuthAccountRequest 绑定第三方账号请求参数
type BindOAuthAccountRequest struct {
	UserID       int64
	Provider     string
	OpenID       string
	UnionID      string
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	Nickname     string
	AvatarURL    string
	RawData      string
}

// OAuthLoginRequest 第三方登录请求参数
type OAuthLoginRequest struct {
	Provider     string
	OpenID       string
	UnionID      string
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	Nickname     string
	AvatarURL    string
	RawData      string
	IP           string
	UserAgent    string
}

// NewOAuthService 创建第三方登录服务实例
func NewOAuthService(db *gorm.DB) *OAuthService {
	return &OAuthService{db: db}
}

// BindOAuthAccount 绑定第三方账号
func (s *OAuthService) BindOAuthAccount(req *BindOAuthAccountRequest) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if req.Provider == "" || req.OpenID == "" {
		return errwrap.Error(400, errors.New("第三方平台和OpenID不能为空"))
	}

	// 检查用户是否存在
	var user User
	if err := s.db.WithContext(ctx).Where("id = ?", req.UserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("用户不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询用户失败: %w", err))
	}

	// 检查该第三方账号是否已被其他用户绑定
	var existingBinding OAuthBinding
	if err := s.db.WithContext(ctx).Where("provider = ? AND open_id = ?", req.Provider, req.OpenID).First(&existingBinding).Error; err == nil {
		if existingBinding.UserID != req.UserID {
			return errwrap.Error(400, errors.New("该第三方账号已被其他用户绑定"))
		}
		// 如果已绑定，更新绑定信息
		updates := map[string]interface{}{
			"access_token":  req.AccessToken,
			"refresh_token": req.RefreshToken,
			"expires_at":    req.ExpiresAt,
			"nickname":      req.Nickname,
			"avatar_url":    req.AvatarURL,
			"raw_data":      req.RawData,
		}
		if err := s.db.WithContext(ctx).Model(&existingBinding).Updates(updates).Error; err != nil {
			return errwrap.Error(500, fmt.Errorf("更新第三方绑定信息失败: %w", err))
		}
		return nil
	}

	// 创建新的绑定记录
	binding := OAuthBinding{
		UserID:       req.UserID,
		Provider:     req.Provider,
		OpenID:       req.OpenID,
		UnionID:      &req.UnionID,
		AccessToken:  &req.AccessToken,
		RefreshToken: &req.RefreshToken,
		ExpiresAt:    req.ExpiresAt,
		Nickname:     &req.Nickname,
		AvatarURL:    &req.AvatarURL,
		RawData:      &req.RawData,
	}

	if err := s.db.WithContext(ctx).Create(&binding).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("绑定第三方账号失败: %w", err))
	}

	return nil
}

// UnbindOAuthAccount 解绑第三方账号
func (s *OAuthService) UnbindOAuthAccount(userID int64, provider string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if provider == "" {
		return errwrap.Error(400, errors.New("第三方平台不能为空"))
	}

	// 检查绑定是否存在
	var binding OAuthBinding
	if err := s.db.WithContext(ctx).Where("user_id = ? AND provider = ?", userID, provider).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("未找到该第三方绑定"))
		}
		return errwrap.Error(500, fmt.Errorf("查询第三方绑定失败: %w", err))
	}

	// 删除绑定记录
	if err := s.db.WithContext(ctx).Delete(&binding).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("解绑第三方账号失败: %w", err))
	}

	return nil
}

// GetUserOAuthBindings 获取用户的第三方绑定列表
func (s *OAuthService) GetUserOAuthBindings(userID int64) ([]OAuthBinding, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var bindings []OAuthBinding
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Find(&bindings).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("查询用户第三方绑定失败: %w", err))
	}

	return bindings, nil
}

// FindUserByOAuth 通过第三方账号查找用户
func (s *OAuthService) FindUserByOAuth(provider, openID string) (*User, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if provider == "" || openID == "" {
		return nil, errwrap.Error(400, errors.New("第三方平台和OpenID不能为空"))
	}

	var binding OAuthBinding
	if err := s.db.WithContext(ctx).Where("provider = ? AND open_id = ?", provider, openID).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // 未找到绑定，返回nil
		}
		return nil, errwrap.Error(500, fmt.Errorf("查询第三方绑定失败: %w", err))
	}

	var user User
	if err := s.db.WithContext(ctx).Where("id = ?", binding.UserID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errwrap.Error(404, errors.New("绑定的用户不存在"))
		}
		return nil, errwrap.Error(500, fmt.Errorf("查询用户失败: %w", err))
	}

	return &user, nil
}

// OAuthLogin 第三方登录
func (s *OAuthService) OAuthLogin(req *OAuthLoginRequest) (*User, *TokenResponse, error) {
	// 查找是否已绑定该第三方账号
	user, err := s.FindUserByOAuth(req.Provider, req.OpenID)
	if err != nil {
		return nil, nil, err
	}

	if user == nil {
		// 未绑定，需要先注册或绑定
		return nil, nil, errwrap.Error(401, errors.New("该第三方账号未绑定任何用户"))
	}

	// 更新绑定信息（Token等可能会过期）
	bindReq := &BindOAuthAccountRequest{
		UserID:       user.ID,
		Provider:     req.Provider,
		OpenID:       req.OpenID,
		UnionID:      req.UnionID,
		AccessToken:  req.AccessToken,
		RefreshToken: req.RefreshToken,
		ExpiresAt:    req.ExpiresAt,
		Nickname:     req.Nickname,
		AvatarURL:    req.AvatarURL,
		RawData:      req.RawData,
	}
	if err := s.BindOAuthAccount(bindReq); err != nil {
		return nil, nil, err
	}

	// 使用用户服务进行登录
	userService := NewUserService(s.db)
	loginReq := LoginRequest{
		Username:  user.Username,
		LoginType: LoginTypeOAuth,
	}

	return userService.Login(loginReq, req.IP, req.UserAgent)
}

// GetOAuthStats 获取第三方登录统计信息
func (s *OAuthService) GetOAuthStats() (map[string]interface{}, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()
	stats := make(map[string]interface{})

	// 获取总绑定数
	var totalBindings int64
	if err := s.db.WithContext(ctx).Model(&OAuthBinding{}).Count(&totalBindings).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取总绑定数失败: %w", err))
	}
	stats["total_bindings"] = totalBindings

	// 按平台统计
	var providerStats []struct {
		Provider string
		Count    int64
	}
	if err := s.db.WithContext(ctx).Model(&OAuthBinding{}).
		Select("provider, COUNT(*) as count").
		Group("provider").
		Scan(&providerStats).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("按平台统计失败: %w", err))
	}
	stats["provider_stats"] = providerStats

	// 获取活跃绑定数（最近30天有更新的）
	var activeBindings int64
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	if err := s.db.WithContext(ctx).Model(&OAuthBinding{}).
		Where("updated_at > ?", thirtyDaysAgo).
		Count(&activeBindings).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取活跃绑定数失败: %w", err))
	}
	stats["active_bindings"] = activeBindings

	return stats, nil
}

// 辅助方法

// generateNumericCode 生成数字验证码
func generateNumericCode(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("验证码长度必须大于0")
	}

	code := ""
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		code += n.String()
	}

	return code, nil
}

// contains 检查字符串是否在切片中
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, str) {
			return true
		}
	}
	return false
}
