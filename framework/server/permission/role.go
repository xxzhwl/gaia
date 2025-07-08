package permission

import (
	"context"
	"errors"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/server"
	"gorm.io/gorm"
)

type AddRoleRequest struct {
	RoleName string `json:"role_name" require:"1"`
	RoleDesc string `json:"role_desc" require:"1"`

	ResourceIds []int64 `json:"resource_ids"`
	Ctx         context.Context
}

func AddRole(req AddRoleRequest) (err error) {
	req.ResourceIds = gaia.UniqueList(req.ResourceIds)
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	return db.GetGormDb().Transaction(func(tx *gorm.DB) error {
		newRole := RoleVo{
			RoleName: req.RoleName,
			RoleDesc: req.RoleDesc,
		}

		tx = db.GetGormDb().WithContext(req.Ctx)

		createTx := tx.Table(TRole).Create(&newRole)
		if createTx.Error != nil {
			return createTx.Error
		}

		resources := []ResourceVo{}
		findTx := tx.Table(TResource).Where("id in ?", req.ResourceIds).Find(&resources)
		if findTx.Error != nil {
			return findTx.Error
		}
		relations := []RoleResourceVo{}
		for _, item := range resources {
			relations = append(relations, RoleResourceVo{
				RoleId:     newRole.Id,
				ResourceId: item.Id,
			})
		}

		batchTx := tx.Table(TRoleResource).Create(&relations)
		if batchTx.Error != nil {
			return batchTx.Error
		}
		return nil
	})
}

type UpdateRoleRequest struct {
	RoleId   int64  `json:"role_id" require:"1"`
	RoleName string `json:"role_name"`
	RoleDesc string `json:"role_desc"`

	ResourceIds []int64 `json:"resource_ids"`
	Ctx         context.Context
}

func UpdateRole(req UpdateRoleRequest) (err error) {
	req.ResourceIds = gaia.UniqueList(req.ResourceIds)

	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	return db.GetGormDb().Transaction(func(tx *gorm.DB) error {
		newRole := RoleVo{}

		tx = db.GetGormDb().WithContext(req.Ctx)

		roleTx := tx.Table(TRole).Find(&newRole, "id=?", req.RoleId)
		if roleTx.Error != nil {
			return roleTx.Error
		}
		if newRole.Id == 0 {
			return errors.New("no role found")
		}

		updateRoleTx := tx.Table(TRole).Where("id = ?", req.RoleId).Updates(map[string]any{"role_name": req.RoleName, "role_desc": req.RoleDesc})
		if updateRoleTx.Error != nil {
			return updateRoleTx.Error
		}

		oldRelationIds := []int64{}
		oldFindTx := tx.Table(TRoleResource).Select("resource_id").Find(&oldRelationIds, "role_id=?", req.RoleId)
		if oldFindTx.Error != nil {
			return oldFindTx.Error
		}

		resources := []ResourceVo{}
		findTx := tx.Table(TResource).Where("id in ?", req.ResourceIds).Find(&resources)
		if findTx.Error != nil {
			return findTx.Error
		}
		addRelations := []RoleResourceVo{}
		newIds := []int64{}
		for _, item := range resources {
			newIds = append(newIds, item.Id)
		}

		needDelete, needAdd := gaia.DifferenceList(oldRelationIds, newIds)
		for _, item := range needAdd {
			addRelations = append(addRelations, RoleResourceVo{
				RoleId:     newRole.Id,
				ResourceId: item,
			})
		}

		deleteTx := tx.Table(TRoleResource).Delete(&RoleResourceVo{}, "role_id=? and resource_id in ?", req.RoleId, needDelete)
		if deleteTx.Error != nil {
			return deleteTx.Error
		}
		batchTx := tx.Table(TRoleResource).CreateInBatches(&addRelations, 500)

		return batchTx.Error
	})
}

type DeleteRoleRequest struct {
	RoleId int64 `json:"role_id" require:"1"`
	Ctx    context.Context
}

func DeleteRole(req DeleteRoleRequest) (err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	return db.GetGormDb().Transaction(func(tx *gorm.DB) error {
		newRole := RoleVo{}
		tx = tx.WithContext(req.Ctx)

		findRoleTx := tx.Table(TRole).Find(&newRole, "id=?", req.RoleId)
		if findRoleTx.Error != nil {
			return findRoleTx.Error
		}
		if newRole.Id == 0 {
			return errors.New("no role found")
		}
		deleteRoleTx := tx.Table(TRole).Delete(&RoleVo{}, "id = ?", req.RoleId)
		if deleteRoleTx.Error != nil {
			return deleteRoleTx.Error
		}

		deleteRRTx := tx.Table(TRoleResource).Delete(&RoleResourceVo{}, "role_id=?", newRole.Id)
		if deleteRRTx.Error != nil {
			return deleteRRTx.Error
		}

		deleteURTx := tx.Table(TUserRole).Delete(&UserRoleVo{}, "role_id=?", newRole.Id)
		if deleteURTx.Error != nil {
			return deleteURTx.Error
		}
		return nil
	})
}

type FindRoleRequest struct {
	RoleName string `json:"role_name"`
	Start    int    `json:"start"`
	Size     int    `json:"size"`
	Ctx      context.Context
}

type FindRoleResponse struct {
	Id            int64  `json:"id"`
	RoleName      string `json:"role_name"`
	RoleDesc      string `json:"role_desc"`
	Resources     string `json:"resources"`
	ResourceIds   string `json:"resource_ids"`
	ResourcesType string `json:"resources_type"`
	ResourcesUri  string `json:"resources_uri"`
}

func FindRole(req FindRoleRequest) (resp server.ListResponse[FindRoleResponse], err error) {
	roleResources := []FindRoleResponse{}
	resp = server.ListResponse[FindRoleResponse]{
		List:    roleResources,
		HasNext: false,
		Total:   0,
	}

	start, size := 0, 20
	if req.Start >= 0 {
		start = req.Start
	}
	if req.Size > 0 {
		size = req.Size
	}

	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(req.Ctx)

	//1.查询role
	roleTx := tx.Table(TRole).
		Select("t_role.id", "t_role.role_name", "t_role.role_desc",
			"group_concat(t_resource.resource_name SEPARATOR ',') as resources", "group_concat(t_resource.id SEPARATOR ',') as resource_ids",
			"group_concat(t_resource.resource_type SEPARATOR ',') as resources_type",
			"group_concat(t_resource.uri SEPARATOR ',') as resources_uri",
		).
		Joins("left join t_role_resource on t_role.id = t_role_resource.role_id").
		Joins("left join t_resource on t_role_resource.resource_id = t_resource.id").Group("t_role.id")
	if req.RoleName != "" {
		roleTx = roleTx.Where("role_name=?", req.RoleName)
	}
	var count int64 = 0
	t := roleTx.Find(&roleResources).Count(&count).Offset(start).Limit(size)
	if t.Error != nil {
		return resp, t.Error
	}
	resp.List = roleResources
	if len(roleResources) > size {
		resp.HasNext = true
	}
	resp.Total = count
	return
}
