package permission

import (
	"context"
	"errors"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/server"
)

type AddResourceRequest struct {
	ResourceType string `json:"resource_type" require:"1"`
	ResourceName string `json:"resource_name" require:"1"`
	Uri          string `json:"uri" require:"1"`

	Ctx context.Context
}

func AddResource(req AddResourceRequest) (err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	obj := ResourceVo{
		ResourceName: req.ResourceName,
		ResourceType: req.ResourceType,
		Uri:          req.Uri,
	}
	tx := db.GetGormDb().WithContext(req.Ctx).Table(TResource).Create(&obj)
	if tx.Error != nil {
		return tx.Error
	}
	return
}

type UpdateResourceRequest struct {
	ResourceId   int64  `json:"resource_id" require:"1"`
	ResourceType string `json:"resource_type"`
	ResourceName string `json:"resource_name" `
	Uri          string `json:"uri"`

	Ctx context.Context
}

func UpdateResource(req UpdateResourceRequest) (err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	obj := ResourceVo{}
	tx := db.GetGormDb().WithContext(req.Ctx).Table(TResource).Last(&obj, "id = ?", req.ResourceId)
	if tx.Error != nil {
		return tx.Error
	}
	if obj.Id == 0 {
		return errors.New("未查询到该资源")
	}
	if req.ResourceType == obj.ResourceType && req.ResourceName == obj.ResourceName && req.Uri == obj.Uri {
		return nil
	}
	tx = db.GetGormDb().WithContext(req.Ctx).Table(TResource).Where("id = ?", req.ResourceId).Updates(ResourceVo{
		ResourceName: req.ResourceName,
		ResourceType: req.ResourceType,
		Uri:          req.Uri,
	})
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}

type DeleteResourceRequest struct {
	ResourceId int64 `json:"resource_id" require:"1"`
	Ctx        context.Context
}

func DeleteResource(req DeleteResourceRequest) (err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(req.Ctx).Table(TResource).Delete(&ResourceVo{}, "id = ?", req.ResourceId)
	if tx.Error != nil {
		return tx.Error
	}
	tx = db.GetGormDb().WithContext(req.Ctx).Table(TRoleResource).Delete(&RoleResourceVo{}, "resource_id = ?", req.ResourceId)
	if tx.Error != nil {
		return tx.Error
	}
	return nil
}

type FindResourceRequest struct {
	ResourceType string `json:"resource_type"`
	ResourceName string `json:"resource_name"`
	Start        int    `json:"start"`
	Size         int    `json:"size"`

	Ctx context.Context
}

func FindResource(req FindResourceRequest) (resp server.ListResponse[ResourceVo], err error) {
	var resources []ResourceVo
	resp = server.ListResponse[ResourceVo]{
		List:    resources,
		HasNext: false,
		Total:   0,
	}

	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	tx := db.GetGormDb().WithContext(req.Ctx).Table(TResource)

	if len(req.ResourceType) > 0 {
		tx = tx.Where("resource_type = ?", req.ResourceType)
	}

	if len(req.ResourceName) > 0 {
		tx = tx.Where("resource_name = ?", req.ResourceName)
	}

	start, size := 0, 20
	if req.Start >= 0 {
		start = req.Start
	}
	if req.Size > 0 {
		size = req.Size
	}

	var count int64 = 0
	t := tx.Find(&resources).Count(&count).Offset(start).Limit(size)

	if t.Error != nil {
		return resp, t.Error
	}
	if len(resources) > size {
		resp.HasNext = true
	}
	resp.List = resources
	resp.Total = count
	return
}
