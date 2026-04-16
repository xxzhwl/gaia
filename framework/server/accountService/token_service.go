package accountService

import (
	"errors"
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/server"
	"gorm.io/gorm"
)

// TokenService Token服务
type TokenService struct {
	db *gorm.DB
}

// NewTokenService 创建Token服务实例
func NewTokenService(db *gorm.DB) *TokenService {
	return &TokenService{db: db}
}

// RefreshToken 刷新Token
func (s *TokenService) RefreshToken(refreshToken, deviceID string) (*TokenResponse, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if refreshToken == "" {
		return nil, errwrap.Error(401, errors.New("刷新Token不能为空"))
	}

	// 验证刷新Token
	jwtManager := server.DefaultJWTManager()
	claims, err := jwtManager.ValidateToken(refreshToken)
	if err != nil {
		return nil, errwrap.Error(401, errors.New("无效的刷新Token"))
	}

	// 检查Token是否已过期
	if claims.ExpiresAt.Before(time.Now()) {
		return nil, errwrap.Error(401, errors.New("刷新Token已过期"))
	}

	// 检查数据库中是否存在该Token
	var userToken UserToken
	if err := s.db.WithContext(ctx).Where("token = ? AND token_type = ?", refreshToken, TokenTypeRefresh).First(&userToken).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errwrap.Error(401, errors.New("刷新Token不存在"))
		}
		return nil, errwrap.Error(500, fmt.Errorf("查询Token失败: %w", err))
	}

	// 检查Token是否已过期
	if userToken.ExpiresAt.Before(time.Now()) {
		// 删除过期的Token
		s.db.WithContext(ctx).Delete(&userToken)
		return nil, errwrap.Error(401, errors.New("刷新Token已过期"))
	}

	// 获取用户信息
	var user User
	if err := s.db.WithContext(ctx).Where("id = ? AND status = ?", userToken.UserID, UserStatusNormal).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errwrap.Error(404, errors.New("用户不存在或已被禁用"))
		}
		return nil, errwrap.Error(500, fmt.Errorf("查询用户失败: %w", err))
	}

	// 获取用户角色
	roleService := NewRoleService(s.db)
	roles, err := roleService.GetUserRoles(user.ID)
	if err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取用户角色失败: %w", err))
	}

	roleNames := []string{}
	for _, role := range roles {
		roleNames = append(roleNames, role.Name)
	}

	// 生成新的访问Token
	newAccessToken, expiresAt, err := jwtManager.GenerateAccessToken(
		user.ID, user.UUID, user.Username, roleNames,
	)
	if err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("生成访问Token失败: %w", err))
	}

	// 生成新的刷新Token
	newRefreshToken, refreshExpiresAt, err := jwtManager.GenerateRefreshToken(user.ID, user.UUID)
	if err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("生成刷新Token失败: %w", err))
	}

	// 保存新的Token到数据库
	if err := s.saveUserToken(user.ID, TokenTypeAccess, newAccessToken, expiresAt, deviceID); err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("保存访问Token失败: %w", err))
	}

	if err := s.saveUserToken(user.ID, TokenTypeRefresh, newRefreshToken, refreshExpiresAt, deviceID); err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("保存刷新Token失败: %w", err))
	}

	// 删除旧的刷新Token
	if err := s.db.WithContext(ctx).Delete(&userToken).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("删除旧Token失败: %w", err))
	}

	// 返回新的Token响应
	tokenResp := &TokenResponse{
		AccessToken:  newAccessToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    expiresAt,
		TokenType:    "Bearer",
	}

	return tokenResp, nil
}

// RevokeToken 吊销Token
func (s *TokenService) RevokeToken(token string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if token == "" {
		return errwrap.Error(400, errors.New("Token不能为空"))
	}

	// 删除Token
	if err := s.db.WithContext(ctx).Where("token = ?", token).Delete(&UserToken{}).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("吊销Token失败: %w", err))
	}

	return nil
}

// RevokeUserTokens 吊销用户的所有Token
func (s *TokenService) RevokeUserTokens(userID int64) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&UserToken{}).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("吊销用户Token失败: %w", err))
	}

	return nil
}

// RevokeUserDeviceTokens 吊销用户指定设备的Token
func (s *TokenService) RevokeUserDeviceTokens(userID int64, deviceID string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if deviceID == "" {
		return errwrap.Error(400, errors.New("设备ID不能为空"))
	}

	if err := s.db.WithContext(ctx).Where("user_id = ? AND device_id = ?", userID, deviceID).Delete(&UserToken{}).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("吊销用户设备Token失败: %w", err))
	}

	return nil
}

// GetUserTokens 获取用户的Token列表
func (s *TokenService) GetUserTokens(userID int64) ([]UserToken, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var tokens []UserToken
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&tokens).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("查询用户Token失败: %w", err))
	}

	return tokens, nil
}

// ValidateAccessToken 验证访问Token
func (s *TokenService) ValidateAccessToken(accessToken string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if accessToken == "" {
		return errwrap.Error(401, errors.New("访问Token不能为空"))
	}

	// 验证Token
	jwtManager := server.DefaultJWTManager()
	claims, err := jwtManager.ValidateToken(accessToken)
	if err != nil {
		return errwrap.Error(401, errors.New("无效的访问Token"))
	}

	// 检查Token是否已过期
	if claims.ExpiresAt.Before(time.Now()) {
		return errwrap.Error(401, errors.New("访问Token已过期"))
	}

	// 检查数据库中是否存在该Token
	var userToken UserToken
	if err := s.db.WithContext(ctx).Where("token = ? AND token_type = ?", accessToken, TokenTypeAccess).First(&userToken).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(401, errors.New("访问Token不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询Token失败: %w", err))
	}

	// 检查Token是否已过期
	if userToken.ExpiresAt.Before(time.Now()) {
		// 删除过期的Token
		s.db.WithContext(ctx).Delete(&userToken)
		return errwrap.Error(401, errors.New("访问Token已过期"))
	}

	return nil
}

// CleanExpiredTokens 清理过期的Token
func (s *TokenService) CleanExpiredTokens() (int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	result := s.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&UserToken{})
	if result.Error != nil {
		return 0, errwrap.Error(500, fmt.Errorf("清理过期Token失败: %w", result.Error))
	}

	return result.RowsAffected, nil
}

// GetTokenStats 获取Token统计信息
func (s *TokenService) GetTokenStats() (map[string]interface{}, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	stats := make(map[string]interface{})

	// 获取总Token数
	var totalTokens int64
	if err := s.db.WithContext(ctx).Model(&UserToken{}).Count(&totalTokens).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取总Token数失败: %w", err))
	}
	stats["total_tokens"] = totalTokens

	// 获取访问Token数
	var accessTokens int64
	if err := s.db.WithContext(ctx).Model(&UserToken{}).Where("token_type = ?", TokenTypeAccess).Count(&accessTokens).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取访问Token数失败: %w", err))
	}
	stats["access_tokens"] = accessTokens

	// 获取刷新Token数
	var refreshTokens int64
	if err := s.db.WithContext(ctx).Model(&UserToken{}).Where("token_type = ?", TokenTypeRefresh).Count(&refreshTokens).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取刷新Token数失败: %w", err))
	}
	stats["refresh_tokens"] = refreshTokens

	// 获取过期Token数
	var expiredTokens int64
	if err := s.db.WithContext(ctx).Model(&UserToken{}).Where("expires_at < ?", time.Now()).Count(&expiredTokens).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取过期Token数失败: %w", err))
	}
	stats["expired_tokens"] = expiredTokens

	// 获取活跃用户数（最近7天有Token的用户）
	var activeUsers int64
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	if err := s.db.WithContext(ctx).Model(&UserToken{}).
		Distinct("user_id").
		Where("created_at > ?", sevenDaysAgo).
		Count(&activeUsers).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("获取活跃用户数失败: %w", err))
	}
	stats["active_users"] = activeUsers

	return stats, nil
}

// 辅助方法

// saveUserToken 保存用户Token
func (s *TokenService) saveUserToken(userID int64, tokenType, token string, expiresAt time.Time, deviceID string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	userToken := UserToken{
		UserID:    userID,
		TokenType: tokenType,
		Token:     token,
		ExpiresAt: expiresAt,
	}

	if deviceID != "" {
		userToken.DeviceID = &deviceID
	}

	if err := s.db.WithContext(ctx).Create(&userToken).Error; err != nil {
		return fmt.Errorf("保存用户Token失败: %w", err)
	}

	return nil
}
