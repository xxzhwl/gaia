package account

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Organization 组织/部门模型，支持树形层级结构。
type Organization struct {
	ID          string         `json:"id" gorm:"size:36;primaryKey"`
	TenantID    string         `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_orgs_code,priority:1;index:idx_acct_orgs_tenant_parent,priority:1"`
	Code        string         `json:"code" gorm:"size:100;not null;uniqueIndex:uniq_acct_orgs_code,priority:2"`
	Name        string         `json:"name" gorm:"size:200;not null"`
	Description string         `json:"description" gorm:"size:500"`
	ParentID    *string        `json:"parent_id" gorm:"size:36;index:idx_acct_orgs_tenant_parent,priority:2"`
	Status      string         `json:"status" gorm:"size:20;not null;default:enabled"`
	Version     int64          `json:"version" gorm:"not null;default:1"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Organization) TableName() string { return "acct_organizations" }

// OrgNode 组织树的节点。
type OrgNode struct {
	Organization
	Children []*OrgNode `json:"children,omitempty"`
}

// OrgService 组织/部门管理服务。
type OrgService struct {
	m *Manager
}

// CreateOrgRequest 创建组织的请求参数。
type CreateOrgRequest struct {
	TenantID    string
	Code        string
	Name        string
	Description string
	ParentID    string // 空字符串表示根组织
}

// CreateOrg 创建新组织。
func (s *OrgService) CreateOrg(ctx context.Context, req CreateOrgRequest) (*Organization, error) {
	tenantID := s.m.tenantID(req.TenantID)
	var parent *string
	if req.ParentID != "" {
		parent = &req.ParentID
		// 验证父组织存在
		var p Organization
		if err := s.m.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", req.ParentID, tenantID).First(&p).Error; err != nil {
			return nil, fmt.Errorf("parent org not found: %w", err)
		}
	}
	org := &Organization{
		ID:          newID(),
		TenantID:    tenantID,
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
		ParentID:    parent,
		Status:      "enabled",
		Version:     1,
	}
	if err := s.m.db.WithContext(ctx).Create(org).Error; err != nil {
		return nil, err
	}
	return org, nil
}

// UpdateOrg 更新组织信息。
func (s *OrgService) UpdateOrg(ctx context.Context, id, name, description string) error {
	updates := map[string]any{}
	if name != "" {
		updates["name"] = name
	}
	if description != "" {
		updates["description"] = description
	}
	if len(updates) == 0 {
		return nil
	}
	return s.m.db.WithContext(ctx).Model(&Organization{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteOrg 删除组织及其子组织，同时移除所有关联的用户角色。
func (s *OrgService) DeleteOrg(ctx context.Context, id string) error {
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 收集所有子组织 ID（递归）
		ids := s.collectOrgIDs(tx, id)
		ids = append(ids, id)

		// 删除用户-组织角色关联
		if err := tx.Where("scope_type = ? AND scope_id IN ?", "org", ids).Delete(&UserRole{}).Error; err != nil {
			return err
		}
		// 软删除组织
		if err := tx.Where("id IN ?", ids).Delete(&Organization{}).Error; err != nil {
			return err
		}
		return nil
	})
}

// GetOrg 获取组织详情。
func (s *OrgService) GetOrg(ctx context.Context, id string) (*Organization, error) {
	var org Organization
	if err := s.m.db.WithContext(ctx).Where("id = ?", id).First(&org).Error; err != nil {
		return nil, err
	}
	return &org, nil
}

// ListOrgs 列出租户下所有组织。
func (s *OrgService) ListOrgs(ctx context.Context, tenantID string) ([]Organization, error) {
	var orgs []Organization
	if err := s.m.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", s.m.tenantID(tenantID), "enabled").
		Order("created_at ASC").
		Find(&orgs).Error; err != nil {
		return nil, err
	}
	return orgs, nil
}

// GetOrgTree 返回组织树形结构。
func (s *OrgService) GetOrgTree(ctx context.Context, tenantID string) ([]*OrgNode, error) {
	orgs, err := s.ListOrgs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return buildOrgTree(orgs), nil
}

// OrgAncestors 返回组织的所有祖先（从根到直接父级）。
func (s *OrgService) OrgAncestors(ctx context.Context, orgID string) ([]Organization, error) {
	org, err := s.GetOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return s.collectAncestors(ctx, org)
}

func (s *OrgService) collectAncestors(ctx context.Context, org *Organization) ([]Organization, error) {
	var ancestors []Organization
	current := org
	for current.ParentID != nil {
		var parent Organization
		if err := s.m.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", *current.ParentID, current.TenantID).First(&parent).Error; err != nil {
			return ancestors, nil // 父组织可能已被删除
		}
		ancestors = append([]Organization{parent}, ancestors...)
		current = &parent
	}
	return ancestors, nil
}

// AssignOrgRole 为用户分配组织级角色。
func (s *OrgService) AssignOrgRole(ctx context.Context, userID, orgID, roleID string) error {
	var org Organization
	if err := s.m.db.WithContext(ctx).Where("id = ?", orgID).First(&org).Error; err != nil {
		return fmt.Errorf("org not found: %w", err)
	}
	userRole := UserRole{
		ID:        newID(),
		TenantID:  org.TenantID,
		UserID:    userID,
		RoleID:    roleID,
		ScopeType: "org",
		ScopeID:   orgID,
	}
	if err := s.m.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&userRole).Error; err != nil {
		return err
	}
	// 增加角色的版本以刷新缓存
	if err := s.m.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Update("roles_version", gorm.Expr("roles_version + 1")).Error; err != nil {
		return err
	}
	_ = s.m.authorizer.invalidatePermissions(ctx, userID)
	s.m.auth.invalidateUserPrincipalCaches(ctx, userID)
	return nil
}

// RemoveOrgRole 移除用户的组织级角色。
func (s *OrgService) RemoveOrgRole(ctx context.Context, userID, orgID, roleID string) error {
	err := s.m.db.WithContext(ctx).
		Where("user_id = ? AND scope_type = ? AND scope_id = ? AND role_id = ?", userID, "org", orgID, roleID).
		Delete(&UserRole{}).Error
	if err != nil {
		return err
	}
	if err := s.m.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Update("roles_version", gorm.Expr("roles_version + 1")).Error; err != nil {
		return err
	}
	_ = s.m.authorizer.invalidatePermissions(ctx, userID)
	s.m.auth.invalidateUserPrincipalCaches(ctx, userID)
	return nil
}

// ListOrgMembers 列出组织的所有成员及其角色。
func (s *OrgService) ListOrgMembers(ctx context.Context, orgID string) ([]UserRole, error) {
	var members []UserRole
	if err := s.m.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ?", "org", orgID).
		Find(&members).Error; err != nil {
		return nil, err
	}
	return members, nil
}

// ListUserOrgs 列出用户加入的所有组织。
func (s *OrgService) ListUserOrgs(ctx context.Context, userID string) ([]Organization, error) {
	var orgs []Organization
	if err := s.m.db.WithContext(ctx).
		Joins("JOIN acct_user_roles ON acct_user_roles.scope_id = acct_organizations.id").
		Where("acct_user_roles.user_id = ? AND acct_user_roles.scope_type = ? AND acct_organizations.status = ?", userID, "org", "enabled").
		Distinct().
		Find(&orgs).Error; err != nil {
		return nil, err
	}
	return orgs, nil
}

// collectOrgIDs 递归收集所有子组织 ID。
func (s *OrgService) collectOrgIDs(tx *gorm.DB, parentID string) []string {
	var children []Organization
	tx.Where("parent_id = ?", parentID).Find(&children)
	var ids []string
	for _, child := range children {
		ids = append(ids, child.ID)
		ids = append(ids, s.collectOrgIDs(tx, child.ID)...)
	}
	return ids
}

// OrgScopeIDs 返回组织及其所有祖先的作用域 ID 列表，
// 用于权限评估时向上继承。
func (s *OrgService) OrgScopeIDs(ctx context.Context, orgID string) ([]string, error) {
	org, err := s.GetOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	ancestors, err := s.collectAncestors(ctx, org)
	if err != nil {
		return nil, err
	}
	ids := []string{orgID}
	for _, a := range ancestors {
		ids = append(ids, a.ID)
	}
	return ids, nil
}

// buildOrgTree 将组织列表转换为树形结构。
func buildOrgTree(orgs []Organization) []*OrgNode {
	nodes := make(map[string]*OrgNode)
	var roots []*OrgNode
	for i := range orgs {
		nodes[orgs[i].ID] = &OrgNode{Organization: orgs[i]}
	}
	for _, node := range nodes {
		if node.ParentID != nil {
			if parent, ok := nodes[*node.ParentID]; ok {
				parent.Children = append(parent.Children, node)
			}
		} else {
			roots = append(roots, node)
		}
	}
	return roots
}
