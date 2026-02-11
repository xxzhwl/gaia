package accountService

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"gorm.io/gorm"
)

// AccountManager 账号服务管理器
type AccountManager struct {
	db *gorm.DB

	// 各个服务实例
	userService         *UserService
	roleService         *RoleService
	tokenService        *TokenService
	verificationService *VerificationService
	oauthService        *OAuthService
	logService          *LogService
}

// NewAccountManager 创建账号服务管理器
func NewAccountManager(db *gorm.DB) (*AccountManager, error) {
	if db == nil {
		return nil, errors.New("数据库连接不能为空")
	}

	manager := &AccountManager{
		db: db,
	}

	// 初始化各个服务实例
	manager.userService = NewUserService(db)
	manager.roleService = NewRoleService(db)
	manager.tokenService = NewTokenService(db)
	manager.verificationService = NewVerificationService(db)
	manager.oauthService = NewOAuthService(db)
	manager.logService = NewLogService(db)

	return manager, nil
}

// NewAccountManagerWithSchema 根据配置schema创建账号服务管理器
func NewAccountManagerWithSchema(schema string) (*AccountManager, error) {
	db, err := gaia.NewMysqlWithSchema(schema)
	if err != nil {
		return nil, fmt.Errorf("创建数据库连接失败: %w", err)
	}

	return NewAccountManager(db.GetGormDb())
}

// NewFrameworkAccountManager 框架层控制
func NewFrameworkAccountManager() (*AccountManager, error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return nil, fmt.Errorf("创建数据库连接失败: %w", err)
	}

	return NewAccountManager(db.GetGormDb())
}

// GetUserService 获取用户服务实例
func (m *AccountManager) GetUserService() *UserService {
	return m.userService
}

// GetRoleService 获取角色服务实例
func (m *AccountManager) GetRoleService() *RoleService {
	return m.roleService
}

// GetTokenService 获取Token服务实例
func (m *AccountManager) GetTokenService() *TokenService {
	return m.tokenService
}

// GetVerificationService 获取验证码服务实例
func (m *AccountManager) GetVerificationService() *VerificationService {
	return m.verificationService
}

// GetOAuthService 获取第三方登录服务实例
func (m *AccountManager) GetOAuthService() *OAuthService {
	return m.oauthService
}

// GetLogService 获取日志服务实例
func (m *AccountManager) GetLogService() *LogService {
	return m.logService
}

// InitDatabase 初始化数据库表
func (m *AccountManager) InitDatabase() error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 自动迁移数据库表
	err := m.db.WithContext(ctx).AutoMigrate(
		&User{},
		&Role{},
		&Permission{},
		&UserRole{},
		&RolePermission{},
		&UserToken{},
		&LoginLog{},
		&VerificationCode{},
		&OperationLog{},
		&OAuthBinding{},
	)
	if err != nil {
		return errwrap.Error(500, fmt.Errorf("数据库迁移失败: %w", err))
	}

	// 添加表和字段注释
	if err := m.initTableComments(); err != nil {
		return errwrap.Error(500, fmt.Errorf("添加表注释失败: %w", err))
	}

	// 初始化默认数据
	return m.initDefaultData()
}

// initTableComments 初始化表和字段注释
func (m *AccountManager) initTableComments() error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 定义所有需要添加注释的模型
	models := []Commentable{
		User{},
		Role{},
		Permission{},
		UserRole{},
		RolePermission{},
		UserToken{},
		LoginLog{},
		VerificationCode{},
		OperationLog{},
		OAuthBinding{},
	}

	for _, model := range models {
		tableName := model.TableName()
		tableComment := model.TableComment()

		// 添加表注释
		sql := fmt.Sprintf("ALTER TABLE `%s` COMMENT = '%s'", tableName, tableComment)
		if err := m.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("为表 %s 添加注释失败: %w", tableName, err)
		}
	}

	return nil
}

// initDefaultData 初始化默认数据
func (m *AccountManager) initDefaultData() error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 检查是否已存在默认角色
	var count int64
	if err := m.db.WithContext(ctx).Model(&Role{}).Count(&count).Error; err != nil {
		return fmt.Errorf("检查角色数据失败: %w", err)
	}

	// 如果还没有角色数据，初始化默认角色和权限
	if count == 0 {
		return m.initDefaultRolesAndPermissions()
	}

	return nil
}

// initDefaultRolesAndPermissions 初始化默认角色和权限
func (m *AccountManager) initDefaultRolesAndPermissions() error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 创建默认角色
	defaultRoles := []Role{
		{
			Name:        "超级管理员",
			Code:        "super_admin",
			Description: "拥有系统所有权限",
			IsSystem:    1,
			Status:      RoleStatusEnabled,
			SortOrder:   1,
		},
		{
			Name:        "管理员",
			Code:        "admin",
			Description: "系统管理员",
			IsSystem:    1,
			Status:      RoleStatusEnabled,
			SortOrder:   2,
		},
		{
			Name:        "普通用户",
			Code:        "user",
			Description: "普通注册用户",
			IsSystem:    1,
			Status:      RoleStatusEnabled,
			SortOrder:   3,
		},
		{
			Name:        "访客",
			Code:        "guest",
			Description: "游客/未登录用户",
			IsSystem:    1,
			Status:      RoleStatusEnabled,
			SortOrder:   4,
		},
	}

	for _, role := range defaultRoles {
		if err := m.db.WithContext(ctx).Create(&role).Error; err != nil {
			return fmt.Errorf("创建默认角色失败: %w", err)
		}
	}

	// 创建默认权限
	defaultPermissions := []Permission{
		// 系统管理菜单
		{
			Name:      "系统管理",
			Code:      "system",
			Type:      PermissionTypeMenu,
			ParentID:  0,
			Path:      "/system",
			SortOrder: 1,
			Status:    RoleStatusEnabled,
		},
		// 用户管理
		{
			Name:      "用户管理",
			Code:      "system:user",
			Type:      PermissionTypeMenu,
			ParentID:  1,
			Path:      "/system/user",
			SortOrder: 1,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "用户查看",
			Code:      "system:user:view",
			Type:      PermissionTypeButton,
			ParentID:  2,
			SortOrder: 1,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "用户新增",
			Code:      "system:user:add",
			Type:      PermissionTypeButton,
			ParentID:  2,
			SortOrder: 2,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "用户编辑",
			Code:      "system:user:edit",
			Type:      PermissionTypeButton,
			ParentID:  2,
			SortOrder: 3,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "用户删除",
			Code:      "system:user:delete",
			Type:      PermissionTypeButton,
			ParentID:  2,
			SortOrder: 4,
			Status:    RoleStatusEnabled,
		},
		// 角色管理
		{
			Name:      "角色管理",
			Code:      "system:role",
			Type:      PermissionTypeMenu,
			ParentID:  1,
			Path:      "/system/role",
			SortOrder: 2,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "角色查看",
			Code:      "system:role:view",
			Type:      PermissionTypeButton,
			ParentID:  7,
			SortOrder: 1,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "角色新增",
			Code:      "system:role:add",
			Type:      PermissionTypeButton,
			ParentID:  7,
			SortOrder: 2,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "角色编辑",
			Code:      "system:role:edit",
			Type:      PermissionTypeButton,
			ParentID:  7,
			SortOrder: 3,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "角色删除",
			Code:      "system:role:delete",
			Type:      PermissionTypeButton,
			ParentID:  7,
			SortOrder: 4,
			Status:    RoleStatusEnabled,
		},
		// 权限管理
		{
			Name:      "权限管理",
			Code:      "system:permission",
			Type:      PermissionTypeMenu,
			ParentID:  1,
			Path:      "/system/permission",
			SortOrder: 3,
			Status:    RoleStatusEnabled,
		},
		// 日志管理
		{
			Name:      "日志管理",
			Code:      "system:log",
			Type:      PermissionTypeMenu,
			ParentID:  1,
			Path:      "/system/log",
			SortOrder: 4,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "登录日志",
			Code:      "system:log:login",
			Type:      PermissionTypeMenu,
			ParentID:  13,
			Path:      "/system/log/login",
			SortOrder: 1,
			Status:    RoleStatusEnabled,
		},
		{
			Name:      "操作日志",
			Code:      "system:log:operation",
			Type:      PermissionTypeMenu,
			ParentID:  13,
			Path:      "/system/log/operation",
			SortOrder: 2,
			Status:    RoleStatusEnabled,
		},
	}

	for _, permission := range defaultPermissions {
		if err := m.db.WithContext(ctx).Create(&permission).Error; err != nil {
			return fmt.Errorf("创建默认权限失败: %w", err)
		}
	}

	// 为超级管理员分配所有权限
	var superAdminRole Role
	if err := m.db.WithContext(ctx).Where("code = ?", "super_admin").First(&superAdminRole).Error; err != nil {
		return fmt.Errorf("查找超级管理员角色失败: %w", err)
	}

	var permissions []Permission
	if err := m.db.WithContext(ctx).Find(&permissions).Error; err != nil {
		return fmt.Errorf("查找权限列表失败: %w", err)
	}

	for _, permission := range permissions {
		rolePermission := RolePermission{
			RoleID:       superAdminRole.ID,
			PermissionID: permission.ID,
		}
		if err := m.db.WithContext(ctx).Create(&rolePermission).Error; err != nil {
			return fmt.Errorf("为超级管理员分配权限失败: %w", err)
		}
	}

	return nil
}

// Cleanup 清理资源
func (m *AccountManager) Cleanup() error {
	// 清理过期的Token
	if _, err := m.tokenService.CleanExpiredTokens(); err != nil {
		return fmt.Errorf("清理过期Token失败: %w", err)
	}

	// 清理过期的验证码
	if _, err := m.verificationService.CleanExpiredCodes(); err != nil {
		return fmt.Errorf("清理过期验证码失败: %w", err)
	}

	return nil
}

// GetSystemStats 获取系统统计信息
func (m *AccountManager) GetSystemStats() (map[string]interface{}, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	stats := make(map[string]interface{})

	// 用户统计
	var totalUsers int64
	if err := m.db.WithContext(ctx).Model(&User{}).Count(&totalUsers).Error; err != nil {
		return nil, fmt.Errorf("获取用户总数失败: %w", err)
	}
	stats["total_users"] = totalUsers

	var activeUsers int64
	todayStart := time.Now().Truncate(24 * time.Hour)
	if err := m.db.WithContext(ctx).Model(&LoginLog{}).
		Distinct("user_id").
		Where("created_at >= ? AND status = ?", todayStart, 1).
		Count(&activeUsers).Error; err != nil {
		return nil, fmt.Errorf("获取今日活跃用户数失败: %w", err)
	}
	stats["today_active_users"] = activeUsers

	// 角色统计
	var totalRoles int64
	if err := m.db.WithContext(ctx).Model(&Role{}).Count(&totalRoles).Error; err != nil {
		return nil, fmt.Errorf("获取角色总数失败: %w", err)
	}
	stats["total_roles"] = totalRoles

	// 权限统计
	var totalPermissions int64
	if err := m.db.WithContext(ctx).Model(&Permission{}).Count(&totalPermissions).Error; err != nil {
		return nil, fmt.Errorf("获取权限总数失败: %w", err)
	}
	stats["total_permissions"] = totalPermissions

	// Token统计
	tokenStats, err := m.tokenService.GetTokenStats()
	if err != nil {
		return nil, fmt.Errorf("获取Token统计信息失败: %w", err)
	}
	stats["token_stats"] = tokenStats

	// 验证码统计
	codeStats, err := m.verificationService.GetCodeStats()
	if err != nil {
		return nil, fmt.Errorf("获取验证码统计信息失败: %w", err)
	}
	stats["code_stats"] = codeStats

	// 第三方登录统计
	oauthStats, err := m.oauthService.GetOAuthStats()
	if err != nil {
		return nil, fmt.Errorf("获取第三方登录统计信息失败: %w", err)
	}
	stats["oauth_stats"] = oauthStats

	// 日志摘要
	logSummary, err := m.logService.GetLogSummary()
	if err != nil {
		return nil, fmt.Errorf("获取日志摘要失败: %w", err)
	}
	stats["log_summary"] = logSummary

	return stats, nil
}

// HealthCheck 健康检查
func (m *AccountManager) HealthCheck(ctx context.Context) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	// 检查数据库连接
	sqlDB, err := m.db.WithContext(ctx).DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("数据库连接异常: %w", err)
	}
	result["database"] = "healthy"

	// 检查各表是否存在
	tables := []string{"users", "roles", "permissions", "user_roles", "role_permissions", "user_tokens", "login_logs", "verification_codes", "operation_logs", "oauth_bindings"}
	for _, table := range tables {
		var exists bool
		err := m.db.WithContext(ctx).Raw("SELECT 1 FROM " + table + " LIMIT 1").Scan(&exists).Error
		if err != nil {
			result[table] = "missing"
		} else {
			result[table] = "exists"
		}
	}

	return result, nil
}
