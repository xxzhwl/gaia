package accountService

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/server"
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
	salt := generateSalt()
	passwordHash := hashPassword(req.Password, salt)

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

	// 保存用户
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	// 为用户分配默认角色
	if err := s.assignDefaultRole(&user); err != nil {
		// 如果分配角色失败，删除刚创建的用户
		s.db.WithContext(ctx).Delete(&user)
		return nil, fmt.Errorf("分配默认角色失败: %w", err)
	}

	return &user, nil
}

// Login 用户登录
func (s *UserService) Login(req LoginRequest, ip string, userAgent string) (*User, *TokenResponse, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 验证请求参数
	checker := gaia.NewDataChecker()
	if err := checker.CheckStructDataValid(req); err != nil {
		return nil, nil, fmt.Errorf("参数验证失败: %w", err)
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
	if user.Status == UserStatusLocked {
		return nil, nil, errors.New("用户已被锁定")
	}

	// 验证密码
	if !verifyPassword(req.Password, user.Salt, user.PasswordHash) {
		// 记录登录失败日志
		_ = s.recordLoginLog(user, req.LoginType, ip, userAgent, 0, "密码错误")
		return nil, nil, errors.New("用户名或密码错误")
	}

	// 获取用户角色
	roles, err := s.getUserRoles(user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("获取用户角色失败: %w", err)
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
func (s *UserService) GetUserList(page, pageSize int) ([]User, int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var total int64
	if err := s.db.WithContext(ctx).Model(&User{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询用户总数失败: %w", err)
	}

	var users []User
	offset := (page - 1) * pageSize
	if err := s.db.WithContext(ctx).Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		return nil, 0, fmt.Errorf("查询用户列表失败: %w", err)
	}

	return users, total, nil
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
	newSalt := generateSalt()
	newPasswordHash := hashPassword(newPassword, newSalt)

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
func generateSalt() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// hashPassword 对密码进行哈希处理
func hashPassword(password, salt string) string {
	hash := sha256.New()
	hash.Write([]byte(password + salt))
	return hex.EncodeToString(hash.Sum(nil))
}

// verifyPassword 验证密码
func verifyPassword(password, salt, hash string) bool {
	return hashPassword(password, salt) == hash
}

// generateUUID 生成UUID
func generateUUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("生成UUID失败: %w", err)
	}
	return id.String(), nil
}
