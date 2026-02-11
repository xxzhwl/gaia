package accountService

import (
	"errors"
	"fmt"
	"sort"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"gorm.io/gorm"
)

// RoleService 角色服务
type RoleService struct {
	db *gorm.DB
}

// CreateRoleRequest 创建角色请求参数
type CreateRoleRequest struct {
	Name        string
	Code        string
	Description string
	IsSystem    bool
}

// CreatePermissionRequest 创建权限请求参数
type CreatePermissionRequest struct {
	Name           string
	Code           string
	PermissionType string
	Path           string
	Icon           string
	ParentID       int64
}

// NewRoleService 创建角色服务实例
func NewRoleService(db *gorm.DB) *RoleService {
	return &RoleService{db: db}
}

// CreateRole 创建角色
func (s *RoleService) CreateRole(req *CreateRoleRequest) (*Role, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 检查角色代码是否已存在
	var existingRole Role
	if err := s.db.WithContext(ctx).Where("code = ?", req.Code).First(&existingRole).Error; err == nil {
		return nil, errwrap.Error(400, errors.New("角色代码已存在"))
	}

	isSystemVal := int8(0)
	if req.IsSystem {
		isSystemVal = 1
	}

	role := Role{
		Name:        req.Name,
		Code:        req.Code,
		Description: req.Description,
		IsSystem:    isSystemVal,
		Status:      RoleStatusEnabled,
	}

	if err := s.db.WithContext(ctx).Create(&role).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("创建角色失败: %w", err))
	}

	return &role, nil
}

// UpdateRole 更新角色
func (s *RoleService) UpdateRole(roleID int64, updates map[string]interface{}) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var role Role
	if err := s.db.WithContext(ctx).Where("id = ?", roleID).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("角色不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询角色失败: %w", err))
	}

	// 系统角色不允许修改某些字段
	if role.IsSystem == 1 {
		protectedFields := []string{"code", "is_system"}
		for _, field := range protectedFields {
			delete(updates, field)
		}
	}

	// 检查角色代码是否被其他角色使用
	if code, ok := updates["code"].(string); ok {
		var existingRole Role
		if err := s.db.WithContext(ctx).Where("code = ? AND id != ?", code, roleID).First(&existingRole).Error; err == nil {
			return errwrap.Error(400, errors.New("角色代码已被其他角色使用"))
		}
	}

	if err := s.db.WithContext(ctx).Model(&role).Updates(updates).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("更新角色失败: %w", err))
	}

	return nil
}

// DeleteRole 删除角色
func (s *RoleService) DeleteRole(roleID int64) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var role Role
	if err := s.db.WithContext(ctx).Where("id = ?", roleID).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("角色不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询角色失败: %w", err))
	}

	// 系统角色不允许删除
	if role.IsSystem == 1 {
		return errwrap.Error(403, errors.New("系统角色不允许删除"))
	}

	// 检查是否有用户使用该角色
	var userCount int64
	if err := s.db.WithContext(ctx).Model(&UserRole{}).Where("role_id = ?", roleID).Count(&userCount).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("检查角色使用情况失败: %w", err))
	}

	if userCount > 0 {
		return errwrap.Error(400, errors.New("该角色已被用户使用，无法删除"))
	}

	// 删除角色权限关联
	if err := s.db.WithContext(ctx).Where("role_id = ?", roleID).Delete(&RolePermission{}).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("删除角色权限关联失败: %w", err))
	}

	// 删除角色
	if err := s.db.WithContext(ctx).Delete(&role).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("删除角色失败: %w", err))
	}

	return nil
}

// GetRoleList 获取角色列表
func (s *RoleService) GetRoleList(page, pageSize int) ([]Role, int64, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var roles []Role
	var total int64

	// 计算偏移量
	offset := (page - 1) * pageSize

	// 查询总数
	if err := s.db.WithContext(ctx).Model(&Role{}).Count(&total).Error; err != nil {
		return nil, 0, errwrap.Error(500, fmt.Errorf("查询角色总数失败: %w", err))
	}

	// 查询角色列表
	if err := s.db.WithContext(ctx).Order("sort_order ASC, id ASC").Offset(offset).Limit(pageSize).Find(&roles).Error; err != nil {
		return nil, 0, errwrap.Error(500, fmt.Errorf("查询角色列表失败: %w", err))
	}

	return roles, total, nil
}

// AssignUserRole 为用户分配角色
func (s *RoleService) AssignUserRole(userID, roleID int64) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 检查用户是否存在
	var user User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("用户不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询用户失败: %w", err))
	}

	// 检查角色是否存在且启用
	var role Role
	if err := s.db.WithContext(ctx).Where("id = ? AND status = ?", roleID, RoleStatusEnabled).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("角色不存在或未启用"))
		}
		return errwrap.Error(500, fmt.Errorf("查询角色失败: %w", err))
	}

	// 检查是否已经分配过该角色
	var existingUserRole UserRole
	if err := s.db.WithContext(ctx).Where("user_id = ? AND role_id = ?", userID, roleID).First(&existingUserRole).Error; err == nil {
		return errwrap.Error(400, errors.New("用户已拥有该角色"))
	}

	// 创建用户角色关联
	userRole := UserRole{
		UserID: userID,
		RoleID: roleID,
	}

	if err := s.db.WithContext(ctx).Create(&userRole).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("分配角色失败: %w", err))
	}

	return nil
}

// RemoveUserRole 移除用户的角色
func (s *RoleService) RemoveUserRole(userID, roleID int64) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 检查用户角色关联是否存在
	var userRole UserRole
	if err := s.db.WithContext(ctx).Where("user_id = ? AND role_id = ?", userID, roleID).First(&userRole).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("用户角色关联不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询用户角色关联失败: %w", err))
	}

	// 检查是否为系统角色（某些系统角色可能不允许移除）
	var role Role
	if err := s.db.WithContext(ctx).Where("id = ?", roleID).First(&role).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("查询角色失败: %w", err))
	}

	if role.IsSystem == 1 && role.Code == "super_admin" {
		// 检查是否是最后一个超级管理员
		var superAdminCount int64
		if err := s.db.WithContext(ctx).Model(&UserRole{}).
			Joins("JOIN roles ON roles.id = user_roles.role_id").
			Where("roles.code = ?", "super_admin").
			Count(&superAdminCount).Error; err != nil {
			return errwrap.Error(500, fmt.Errorf("检查超级管理员数量失败: %w", err))
		}

		if superAdminCount <= 1 {
			return errwrap.Error(403, errors.New("系统必须至少保留一个超级管理员"))
		}
	}

	// 删除用户角色关联
	if err := s.db.WithContext(ctx).Delete(&userRole).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("移除角色失败: %w", err))
	}

	return nil
}

// GetUserRoles 获取用户的角色列表
func (s *RoleService) GetUserRoles(userID int64) ([]Role, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var roles []Role
	err := s.db.WithContext(ctx).Model(&Role{}).
		Joins("JOIN user_roles ON user_roles.role_id = roles.id").
		Where("user_roles.user_id = ? AND roles.status = ?", userID, RoleStatusEnabled).
		Order("roles.sort_order ASC").
		Find(&roles).Error

	if err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("查询用户角色失败: %w", err))
	}

	return roles, nil
}

// CreatePermission 创建权限
func (s *RoleService) CreatePermission(req *CreatePermissionRequest) (*Permission, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 检查权限代码是否已存在
	var existingPermission Permission
	if err := s.db.WithContext(ctx).Where("code = ?", req.Code).First(&existingPermission).Error; err == nil {
		return nil, errwrap.Error(400, errors.New("权限代码已存在"))
	}

	// 验证权限类型
	if req.PermissionType != PermissionTypeMenu && req.PermissionType != PermissionTypeButton && req.PermissionType != PermissionTypeAPI {
		return nil, errwrap.Error(400, errors.New("权限类型不合法"))
	}

	permission := Permission{
		Name:     req.Name,
		Code:     req.Code,
		Type:     req.PermissionType,
		ParentID: req.ParentID,
		Path:     req.Path,
		Icon:     req.Icon,
		Status:   RoleStatusEnabled,
	}

	if err := s.db.WithContext(ctx).Create(&permission).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("创建权限失败: %w", err))
	}

	return &permission, nil
}

// UpdatePermission 更新权限
func (s *RoleService) UpdatePermission(permissionID int64, updates map[string]interface{}) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var permission Permission
	if err := s.db.WithContext(ctx).Where("id = ?", permissionID).First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("权限不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询权限失败: %w", err))
	}

	// 检查权限代码是否被其他权限使用
	if code, ok := updates["code"].(string); ok {
		var existingPermission Permission
		if err := s.db.WithContext(ctx).Where("code = ? AND id != ?", code, permissionID).First(&existingPermission).Error; err == nil {
			return errwrap.Error(400, errors.New("权限代码已被其他权限使用"))
		}
	}

	if err := s.db.WithContext(ctx).Model(&permission).Updates(updates).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("更新权限失败: %w", err))
	}

	return nil
}

// DeletePermission 删除权限
func (s *RoleService) DeletePermission(permissionID int64) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var permission Permission
	if err := s.db.WithContext(ctx).Where("id = ?", permissionID).First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("权限不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询权限失败: %w", err))
	}

	// 检查是否有子权限
	var childCount int64
	if err := s.db.WithContext(ctx).Model(&Permission{}).Where("parent_id = ?", permissionID).Count(&childCount).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("检查子权限失败: %w", err))
	}

	if childCount > 0 {
		return errwrap.Error(400, errors.New("该权限下有子权限，无法删除"))
	}

	// 检查是否有角色使用该权限
	var rolePermissionCount int64
	if err := s.db.WithContext(ctx).Model(&RolePermission{}).Where("permission_id = ?", permissionID).Count(&rolePermissionCount).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("检查权限使用情况失败: %w", err))
	}

	if rolePermissionCount > 0 {
		return errwrap.Error(400, errors.New("该权限已被角色使用，无法删除"))
	}

	// 删除权限
	if err := s.db.WithContext(ctx).Delete(&permission).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("删除权限失败: %w", err))
	}

	return nil
}

// GetPermissionTree 获取权限树
func (s *RoleService) GetPermissionTree() ([]*PermissionInfo, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var permissions []Permission
	if err := s.db.WithContext(ctx).Where("status = ?", RoleStatusEnabled).Order("sort_order ASC, id ASC").Find(&permissions).Error; err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("查询权限列表失败: %w", err))
	}

	// 构建权限树
	permissionMap := make(map[int64]*PermissionInfo)
	var rootPermissions []*PermissionInfo

	// 第一遍遍历，创建所有权限节点
	for _, p := range permissions {
		permissionInfo := &PermissionInfo{
			ID:        p.ID,
			Name:      p.Name,
			Code:      p.Code,
			Type:      p.Type,
			ParentID:  p.ParentID,
			Path:      p.Path,
			Icon:      p.Icon,
			SortOrder: p.SortOrder,
			Children:  []*PermissionInfo{},
		}
		permissionMap[p.ID] = permissionInfo
	}

	// 第二遍遍历，构建父子关系
	for _, p := range permissions {
		permissionInfo := permissionMap[p.ID]
		if p.ParentID == 0 {
			rootPermissions = append(rootPermissions, permissionInfo)
		} else {
			if parent, exists := permissionMap[p.ParentID]; exists {
				parent.Children = append(parent.Children, permissionInfo)
			}
		}
	}

	// 对根权限进行排序
	sort.Slice(rootPermissions, func(i, j int) bool {
		return rootPermissions[i].SortOrder < rootPermissions[j].SortOrder
	})

	// 对每个节点的子权限进行排序
	for _, root := range rootPermissions {
		s.sortPermissionChildren(root)
	}

	return rootPermissions, nil
}

// AssignRolePermission 为角色分配权限
func (s *RoleService) AssignRolePermission(roleID, permissionID int64) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 检查角色是否存在
	var role Role
	if err := s.db.WithContext(ctx).Where("id = ?", roleID).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("角色不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询角色失败: %w", err))
	}

	// 检查权限是否存在
	var permission Permission
	if err := s.db.WithContext(ctx).Where("id = ?", permissionID).First(&permission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("权限不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询权限失败: %w", err))
	}

	// 检查是否已经分配过该权限
	var existingRolePermission RolePermission
	if err := s.db.WithContext(ctx).Where("role_id = ? AND permission_id = ?", roleID, permissionID).First(&existingRolePermission).Error; err == nil {
		return errwrap.Error(400, errors.New("角色已拥有该权限"))
	}

	// 创建角色权限关联
	rolePermission := RolePermission{
		RoleID:       roleID,
		PermissionID: permissionID,
	}

	if err := s.db.WithContext(ctx).Create(&rolePermission).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("分配权限失败: %w", err))
	}

	return nil
}

// RemoveRolePermission 移除角色的权限
func (s *RoleService) RemoveRolePermission(roleID, permissionID int64) error {
	ctx := gaia.NewContextTrace().GetParentCtx()

	// 检查角色权限关联是否存在
	var rolePermission RolePermission
	if err := s.db.WithContext(ctx).Where("role_id = ? AND permission_id = ?", roleID, permissionID).First(&rolePermission).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errwrap.Error(404, errors.New("角色权限关联不存在"))
		}
		return errwrap.Error(500, fmt.Errorf("查询角色权限关联失败: %w", err))
	}

	// 删除角色权限关联
	if err := s.db.WithContext(ctx).Delete(&rolePermission).Error; err != nil {
		return errwrap.Error(500, fmt.Errorf("移除权限失败: %w", err))
	}

	return nil
}

// GetRolePermissions 获取角色的权限列表
func (s *RoleService) GetRolePermissions(roleID int64) ([]Permission, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var permissions []Permission
	err := s.db.WithContext(ctx).Model(&Permission{}).
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Where("role_permissions.role_id = ? AND permissions.status = ?", roleID, RoleStatusEnabled).
		Order("permissions.sort_order ASC").
		Find(&permissions).Error

	if err != nil {
		return nil, errwrap.Error(500, fmt.Errorf("查询角色权限失败: %w", err))
	}

	return permissions, nil
}

// CheckUserPermission 检查用户是否拥有指定权限
func (s *RoleService) CheckUserPermission(userID int64, permissionCode string) (bool, error) {
	ctx := gaia.NewContextTrace().GetParentCtx()

	var count int64
	err := s.db.WithContext(ctx).Model(&Permission{}).
		Joins("JOIN role_permissions ON role_permissions.permission_id = permissions.id").
		Joins("JOIN user_roles ON user_roles.role_id = role_permissions.role_id").
		Where("user_roles.user_id = ? AND permissions.code = ? AND permissions.status = ?",
			userID, permissionCode, RoleStatusEnabled).
		Count(&count).Error

	if err != nil {
		return false, errwrap.Error(500, fmt.Errorf("检查用户权限失败: %w", err))
	}

	return count > 0, nil
}

// 辅助方法

// sortPermissionChildren 递归排序权限子节点
func (s *RoleService) sortPermissionChildren(permission *PermissionInfo) {
	if len(permission.Children) == 0 {
		return
	}

	sort.Slice(permission.Children, func(i, j int) bool {
		return permission.Children[i].SortOrder < permission.Children[j].SortOrder
	})

	for _, child := range permission.Children {
		s.sortPermissionChildren(child)
	}
}
