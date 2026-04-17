package accountService

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/server"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserService 用户服务
type UserService struct {
	db *gorm.DB
}

// SaveUserTokenRequest 保存用户Token请求参数
type SaveUserTokenRequest struct {
	UserID    int64
	TokenType string
	Token     string
	ExpiresAt time.Time
	DeviceID  string
}

// NewUserService 创建用户服务实例
func NewUserService(db *gorm.DB) *UserService {
	return &UserService{db: db}
}

// Register 用户注册
func (s *UserService) Register(req RegisterRequest) (*User, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()
	// 验证请求参数
	checker := gaia.NewDataChecker()
	if err := checker.CheckStructDataValid(req); err != nil {
		return nil, fmt.Errorf("参数验证失败: %w", err)
	}

	// 验证密码强度
	if err := validatePasswordStrength(req.Password); err != nil {
		return nil, err
	}

	// 检查用户名是否已存在
	var existingUser User
	if err := s.db.WithContext(ctx).Where("username = ?", req.Username).First(&existingUser).Error; err == nil {
		return nil, errors.New("用户名已存在")
	}

	// 检查邮箱是否已存在
	if req.Email != "" {
		var emailUser User
		if err := s.db.WithContext(ctx).Where("email = ?", req.Email).First(&emailUser).Error; err == nil {
			return nil, errors.New("邮箱已被注册")
		}
	}

	// 检查手机号是否已存在
	if req.Phone != "" {
		var phoneUser User
		if err := s.db.WithContext(ctx).Where("phone = ?", req.Phone).First(&phoneUser).Error; err == nil {
			return nil, errors.New("手机号已被注册")
		}
	}

	// 生成UUID
	uuid, err := generateUUID()
	if err != nil {
		return nil, fmt.Errorf("生成UUID失败: %w", err)
	}

	// 生成密码盐值和哈希
	salt, err := generateSalt()
	if err != nil {
		return nil, fmt.Errorf("生成密码盐值失败: %w", err)
	}
	passwordHash, err := hashPassword(req.Password, salt)
	if err != nil {
		return nil, fmt.Errorf("密码哈希失败: %w", err)
	}

	// 创建用户
	user := User{
		UUID:         uuid,
		Username:     req.Username,
		PasswordHash: passwordHash,
		Salt:         salt,
		Status:       UserStatusNormal,
	}

	// 设置可选字段
	if req.Email != "" {
		user.Email = &req.Email
	}
	if req.Phone != "" {
		user.Phone = &req.Phone
	}
	if req.Nickname != "" {
		user.Nickname = &req.Nickname
	}

	// 在事务中创建用户并分配默认角色
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("创建用户失败: %w", err)
		}

		// 为用户分配默认角色
		var role Role
		if err := tx.WithContext(ctx).Where("code = ? AND status = ?", "user", RoleStatusEnabled).First(&role).Error; err != nil {
			return fmt.Errorf("未找到默认角色: %w", err)
		}

		userRole := UserRole{
			UserID: user.ID,
			RoleID: role.ID,
		}
		if err := tx.Create(&userRole).Error; err != nil {
			return fmt.Errorf("分配默认角色失败: %w", err)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &user, nil
}

// Login 用户登录
func (s *UserService) Login(req LoginRequest, ip string, userAgent string) (*User, *TokenResponse, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// OAuth 登录跳过参数校验
	if req.LoginType != LoginTypeOAuth {
		checker := gaia.NewDataChecker()
		if err := checker.CheckStructDataValid(req); err != nil {
			return nil, nil, fmt.Errorf("参数验证失败: %w", err)
		}
	}

	// 查找用户
	var user User
	query := s.db.WithContext(ctx).Where("username = ?", req.Username)
	if err := query.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, errors.New("未查找到该用户")
		}
		return nil, nil, fmt.Errorf("查询用户失败: %w", err)
	}

	if user.Status == UserStatusDisabled {
		return nil, nil, errors.New("用户已被禁用")
	}

	// 检查锁定状态，锁定过期自动解锁
	if user.Status == UserStatusLocked && user.LockedUntil != nil && user.LockedUntil.Before(time.Now()) {
		s.db.WithContext(ctx).Model(&user).Updates(map[string]interface{}{
			"status":                UserStatusNormal,
			"failed_login_attempts": 0,
			"locked_until":          nil,
		})
		user.Status = UserStatusNormal
		user.FailedLoginAttempts = 0
	}

	if user.Status == UserStatusLocked {
		return nil, nil, errors.New("用户已被锁定")
	}

	// 非 OAuth 登录需要验证密码
	if req.LoginType != LoginTypeOAuth {
		if !verifyPassword(req.Password, user.Salt, user.PasswordHash) {
			// 增加失败计数
			user.FailedLoginAttempts++
			updates := map[string]interface{}{
				"failed_login_attempts": user.FailedLoginAttempts,
			}

			// 连续失败 N 次后锁定
			maxAttempts := gaia.GetSafeConfInt64WithDefault("Account.MaxLoginAttempts", 5)
			lockMinutes := gaia.GetSafeConfInt64WithDefault("Account.LockDuration", 30)
			if int(user.FailedLoginAttempts) >= int(maxAttempts) {
				lockUntil := time.Now().Add(time.Duration(lockMinutes) * time.Minute)
				updates["locked_until"] = lockUntil
				updates["status"] = UserStatusLocked
				user.Status = UserStatusLocked
			}

			s.db.WithContext(ctx).Model(&user).Updates(updates)
			_ = s.recordLoginLog(user, req.LoginType, ip, userAgent, 0, "密码错误")
			return nil, nil, errors.New("用户名或密码错误")
		}
	}

	// 登录成功，重置失败计数
	if user.FailedLoginAttempts > 0 {
		s.db.WithContext(ctx).Model(&user).Updates(map[string]interface{}{
			"failed_login_attempts": 0,
			"locked_until":          nil,
		})
		user.FailedLoginAttempts = 0
	}

	// 兼容历史 SHA256 哈希，并在登录成功后自动升级到 bcrypt。
	if req.LoginType != LoginTypeOAuth && isLegacyPasswordHash(user.PasswordHash) {
		newSalt, saltErr := generateSalt()
		newHash, hashErr := hashPassword(req.Password, newSalt)
		if saltErr == nil && hashErr == nil {
			_ = s.db.WithContext(ctx).Model(&user).Updates(map[string]interface{}{
				"password_hash": newHash,
				"salt":          newSalt,
			}).Error
			user.PasswordHash = newHash
			user.Salt = newSalt
		}
	}

	// 获取用户角色
	roles, err := s.getUserRoles(user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("获取用户角色失败: %w", err)
	}

	// 清理同设备的旧 Token
	if req.DeviceID != "" {
		s.db.WithContext(ctx).Where("user_id = ? AND device_id = ?", user.ID, req.DeviceID).Delete(&UserToken{})
	}

	// 生成Token
	jwtManager := server.DefaultJWTManager()
	accessToken, expiresAt, err := jwtManager.GenerateAccessToken(
		user.ID, user.UUID, user.Username, roles,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("生成访问Token失败: %w", err)
	}

	refreshToken, refreshExpiresAt, err := jwtManager.GenerateRefreshToken(user.ID, user.UUID)
	if err != nil {
		return nil, nil, fmt.Errorf("生成刷新Token失败: %w", err)
	}

	// 保存Token到数据库
	saveTokenReq1 := SaveUserTokenRequest{
		UserID:    user.ID,
		TokenType: TokenTypeAccess,
		Token:     accessToken,
		ExpiresAt: expiresAt,
		DeviceID:  req.DeviceID,
	}
	if err := s.saveUserToken(saveTokenReq1); err != nil {
		return nil, nil, fmt.Errorf("保存访问Token失败: %w", err)
	}

	saveTokenReq2 := SaveUserTokenRequest{
		UserID:    user.ID,
		TokenType: TokenTypeRefresh,
		Token:     refreshToken,
		ExpiresAt: refreshExpiresAt,
		DeviceID:  req.DeviceID,
	}
	if err := s.saveUserToken(saveTokenReq2); err != nil {
		return nil, nil, fmt.Errorf("保存刷新Token失败: %w", err)
	}

	// 更新用户登录信息
	now := time.Now()
	user.LastLoginAt = &now
	user.LastLoginIP = &ip
	user.LoginCount++
	if err := s.db.WithContext(ctx).Save(&user).Error; err != nil {
		return nil, nil, fmt.Errorf("更新用户登录信息失败: %w", err)
	}

	// 记录登录成功日志
	_ = s.recordLoginLog(user, req.LoginType, ip, userAgent, 1, "")

	// 返回Token响应
	tokenResp := &TokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		TokenType:    "Bearer",
	}

	return &user, tokenResp, nil
}

// GetUserByID 根据ID获取用户信息
func (s *UserService) GetUserByID(userID int64) (*UserInfoResponse, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var user User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}

	// 获取用户角色
	roles, err := s.getUserRoles(userID)
	if err != nil {
		return nil, fmt.Errorf("获取用户角色失败: %w", err)
	}

	// 获取用户权限
	permissions, err := s.getUserPermissions(userID)
	if err != nil {
		return nil, fmt.Errorf("获取用户权限失败: %w", err)
	}

	return &UserInfoResponse{
		ID:            user.ID,
		UUID:          user.UUID,
		Username:      user.Username,
		Email:         user.Email,
		Phone:         user.Phone,
		Nickname:      user.Nickname,
		Status:        user.Status,
		EmailVerified: user.EmailVerified,
		PhoneVerified: user.PhoneVerified,
		LastLoginAt:   user.LastLoginAt,
		LoginCount:    user.LoginCount,
		Roles:         roles,
		Permissions:   permissions,
		CreatedAt:     user.CreatedAt,
	}, nil
}

// GetUserByUsername 根据用户名获取用户信息
func (s *UserService) GetUserByUsername(username string) (*User, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var user User
	if err := s.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		return nil, errwrap.Error(500, fmt.Errorf("查询用户失败: %w", err))
	}
	return &user, nil
}

// GetUserList 获取用户列表
func (s *UserService) GetUserList(req UserListRequest) ([]User, int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	query := s.db.WithContext(ctx).Model(&User{})

	// 模糊搜索用户名（转义通配符）
	if req.Username != "" {
		username := strings.ReplaceAll(req.Username, "%", "\\%")
		username = strings.ReplaceAll(username, "_", "\\_")
		query = query.Where("username LIKE ?", "%"+username+"%")
	}

	// 精确搜索邮箱
	if req.Email != "" {
		query = query.Where("email = ?", req.Email)
	}

	// 精确搜索手机号
	if req.Phone != "" {
		query = query.Where("phone = ?", req.Phone)
	}

	// 状态筛选
	if req.Status != nil {
		query = query.Where("status = ?", *req.Status)
	}

	// 角色筛选（JOIN 查询）
	if req.RoleCode != "" {
		query = query.Joins("JOIN user_roles ON user_roles.user_id = users.id").
			Joins("JOIN roles ON roles.id = user_roles.role_id").
			Where("roles.code = ?", req.RoleCode)
	}

	// 时间范围
	if req.StartAt != "" {
		if t, err := time.Parse("2006-01-02", req.StartAt); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if req.EndAt != "" {
		if t, err := time.Parse("2006-01-02", req.EndAt); err == nil {
			query = query.Where("created_at < ?", t.Add(24*time.Hour))
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询用户总数失败: %w", err)
	}

	var users []User
	offset := (req.Page - 1) * req.PageSize
	if err := query.Offset(offset).Limit(req.PageSize).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("查询用户列表失败: %w", err)
	}

	return users, total, nil
}

// ResetPasswordByCode 通过验证码重置密码
func (s *UserService) ResetPasswordByCode(req ResetPasswordRequest) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 验证密码强度
	if err := validatePasswordStrength(req.NewPassword); err != nil {
		return err
	}

	// 通过邮箱或手机号查找用户
	var user User
	query := s.db.WithContext(ctx)
	if strings.Contains(req.Target, "@") {
		query = query.Where("email = ?", req.Target)
	} else {
		query = query.Where("phone = ?", req.Target)
	}
	if err := query.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}

	// 验证验证码
	verificationService := NewVerificationService(s.db)
	_, err := verificationService.VerifyCode(req.Target, req.Code, VerificationTypeResetPass)
	if err != nil {
		return err
	}

	// 生成新密码哈希
	salt, err := generateSalt()
	if err != nil {
		return fmt.Errorf("生成密码盐值失败: %w", err)
	}
	passwordHash, err := hashPassword(req.NewPassword, salt)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}

	// 重置密码并解锁
	updates := map[string]interface{}{
		"password_hash":        passwordHash,
		"salt":                 salt,
		"failed_login_attempts": 0,
		"locked_until":          nil,
		"status":               UserStatusNormal,
	}
	if err := s.db.WithContext(ctx).Model(&user).Updates(updates).Error; err != nil {
		return fmt.Errorf("重置密码失败: %w", err)
	}

	return nil
}

// BatchUpdateStatus 批量修改用户状态
func (s *UserService) BatchUpdateStatus(req BatchUserStatusRequest) (int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if len(req.UserIDs) == 0 {
		return 0, errors.New("用户ID列表不能为空")
	}

	result := s.db.WithContext(ctx).Model(&User{}).
		Where("id IN ?", req.UserIDs).
		Update("status", req.Status)

	if result.Error != nil {
		return 0, fmt.Errorf("批量修改用户状态失败: %w", result.Error)
	}

	return result.RowsAffected, nil
}

// BatchAssignRole 批量分配角色
func (s *UserService) BatchAssignRole(req BatchAssignRoleRequest) (int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	if len(req.UserIDs) == 0 {
		return 0, errors.New("用户ID列表不能为空")
	}

	// 验证角色是否存在
	var role Role
	if err := s.db.WithContext(ctx).Where("id = ?", req.RoleID).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, errors.New("角色不存在")
		}
		return 0, fmt.Errorf("查询角色失败: %w", err)
	}

	var assigned int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, userID := range req.UserIDs {
			// 检查用户是否存在
			var user User
			if err := tx.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
				continue
			}

			// 检查是否已有该角色关联
			var existing UserRole
			if err := tx.WithContext(ctx).
				Where("user_id = ? AND role_id = ?", userID, req.RoleID).
				First(&existing).Error; err == nil {
				continue // 已有，跳过
			}

			// 创建关联
			userRole := UserRole{
				UserID: userID,
				RoleID: req.RoleID,
			}
			if err := tx.Create(&userRole).Error; err == nil {
				assigned++
			}
		}
		return nil
	})

	if err != nil {
		return assigned, err
	}

	return assigned, nil
}

// GetUserDevices 获取用户的登录设备列表
func (s *UserService) GetUserDevices(userID int64) ([]DeviceInfo, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var tokens []UserToken
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("查询用户设备失败: %w", err)
	}

	var devices []DeviceInfo
	for _, token := range tokens {
		device := DeviceInfo{
			DeviceID:  "",
			TokenType: token.TokenType,
			LoginAt:   token.CreatedAt,
		}
		if token.DeviceID != nil {
			device.DeviceID = *token.DeviceID
		}
		devices = append(devices, device)
	}

	return devices, nil
}

// KickDevice 踢出指定设备
func (s *UserService) KickDevice(userID int64, deviceID string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	result := s.db.WithContext(ctx).
		Where("user_id = ? AND device_id = ?", userID, deviceID).
		Delete(&UserToken{})

	if result.Error != nil {
		return fmt.Errorf("踢出设备失败: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return errors.New("设备不存在或已离线")
	}

	return nil
}

// UpdateUserProfile 更新用户资料
func (s *UserService) UpdateUserProfile(userID int64, updates map[string]interface{}) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var user User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("用户不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询用户失败: %w", err))
	}

	// 不允许更新的字段
	protectedFields := []string{"id", "uuid", "username", "password_hash", "salt", "created_at"}
	for _, field := range protectedFields {
		delete(updates, field)
	}

	// 检查邮箱是否被其他用户使用
	if email, ok := updates["email"].(string); ok && email != "" {
		var existingUser User
		if err := s.db.WithContext(ctx).Where("email = ? AND id != ?", email, userID).First(&existingUser).Error; err == nil {
			return errors.New("邮箱已被其他用户使用")
		}
	}

	// 检查手机号是否被其他用户使用
	if phone, ok := updates["phone"].(string); ok && phone != "" {
		var existingUser User
		if err := s.db.WithContext(ctx).Where("phone = ? AND id != ?", phone, userID).First(&existingUser).Error; err == nil {
			return errors.New("手机号已被其他用户使用")
		}
	}

	if err := s.db.WithContext(ctx).Model(&user).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新用户资料失败: %w", err)
	}

	return nil
}

// ChangePassword 修改密码
func (s *UserService) ChangePassword(userID int64, oldPassword, newPassword string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 验证新密码强度
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}

	var user User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("用户不存在")
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}

	// 验证旧密码
	if !verifyPassword(oldPassword, user.Salt, user.PasswordHash) {
		return errors.New("旧密码错误")
	}

	// 生成新的密码盐值和哈希
	newSalt, err := generateSalt()
	if err != nil {
		return errwrap.Error(500, fmt.Errorf("生成密码盐值失败: %w", err))
	}
	newPasswordHash, err := hashPassword(newPassword, newSalt)
	if err != nil {
		return errwrap.Error(500, fmt.Errorf("生成密码哈希失败: %w", err))
	}

	// 更新密码
	if err := s.db.WithContext(ctx).Model(&user).Updates(map[string]interface{}{
		"password_hash": newPasswordHash,
		"salt":          newSalt,
	}).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("修改密码失败: %w", err))
	}

	return nil
}

// 辅助方法

// assignDefaultRole 为用户分配默认角色
func (s *UserService) assignDefaultRole(user *User) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 查找普通用户角色
	var role Role
	if err := s.db.WithContext(ctx).Where("code = ? AND status = ?", "user", RoleStatusEnabled).First(&role).Error; err != nil {
		return fmt.Errorf("未找到默认角色: %w", err)
	}

	// 创建用户角色关联
	userRole := UserRole{
		UserID: user.ID,
		RoleID: role.ID,
	}

	if err := s.db.WithContext(ctx).Create(&userRole).Error; err != nil {
		return fmt.Errorf("创建用户角色关联失败: %w", err)
	}

	return nil
}

// getUserRoles 获取用户角色列表
func (s *UserService) getUserRoles(userID int64) ([]string, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var roles []string
	err := s.db.WithContext(ctx).Model(&Role{}).
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ? AND roles.status = ?", userID, RoleStatusEnabled).
		Pluck("roles.code", &roles).Error

	if err != nil {
		return nil, fmt.Errorf("查询用户角色失败: %w", err)
	}

	return roles, nil
}

// getUserPermissions 获取用户权限列表
func (s *UserService) getUserPermissions(userID int64) ([]string, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var permissions []string
	err := s.db.WithContext(ctx).Model(&Permission{}).
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Where("user_roles.user_id = ? AND permissions.status = ?", userID, RoleStatusEnabled).
		Pluck("permissions.code", &permissions).Error

	if err != nil {
		return nil, fmt.Errorf("查询用户权限失败: %w", err)
	}

	return permissions, nil
}

// saveUserToken 保存用户Token
func (s *UserService) saveUserToken(req SaveUserTokenRequest) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	userToken := UserToken{
		UserID:    req.UserID,
		TokenType: req.TokenType,
		Token:     req.Token,
		ExpiresAt: req.ExpiresAt,
	}

	if req.DeviceID != "" {
		userToken.DeviceID = &req.DeviceID
	}

	if err := s.db.WithContext(ctx).Create(&userToken).Error; err != nil {
		return fmt.Errorf("保存用户Token失败: %w", err)
	}

	return nil
}

// recordLoginLog 记录登录日志
func (s *UserService) recordLoginLog(user User, loginType, ip, userAgent string, status int8, failReason string) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	log := LoginLog{
		UserID:    &user.ID,
		Username:  &user.Username,
		LoginType: loginType,
		IPAddress: &ip,
		UserAgent: &userAgent,
		Status:    status,
	}

	if status == 0 {
		log.FailReason = &failReason
	}

	if err := s.db.WithContext(ctx).Create(&log).Error; err != nil {
		return fmt.Errorf("记录登录日志失败: %w", err)
	}

	return nil
}

// 密码相关辅助函数

// generateSalt 生成密码盐值
func generateSalt() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// hashPassword 对密码进行哈希处理
func hashPassword(password, salt string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password+salt), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

// verifyPassword 验证密码
func verifyPassword(password, salt, hash string) bool {
	if strings.HasPrefix(hash, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password+salt)) == nil
	}

	legacyHash := sha256.New()
	legacyHash.Write([]byte(password + salt))
	legacyHex := hex.EncodeToString(legacyHash.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(legacyHex), []byte(hash)) == 1
}

func isLegacyPasswordHash(hash string) bool {
	return !strings.HasPrefix(hash, "$2")
}

// generateUUID 生成UUID
func generateUUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("生成UUID失败: %w", err)
	}
	return id.String(), nil
}

// validatePasswordStrength 验证密码强度
func validatePasswordStrength(password string) error {
	if len(password) < 8 {
		return errors.New("密码长度不能少于8位")
	}
	if len(password) > 128 {
		return errors.New("密码长度不能超过128位")
	}
	hasUpper := regexp.MustCompile(`[A-Z]`).MatchString(password)
	hasLower := regexp.MustCompile(`[a-z]`).MatchString(password)
	hasDigit := regexp.MustCompile(`[0-9]`).MatchString(password)
	if !hasUpper || !hasLower || !hasDigit {
		return errors.New("密码必须包含大写字母、小写字母和数字")
	}
	return nil
}
