package permission

import "github.com/xxzhwl/gaia/framework/server/account"

const (
	TResource     = "t_resource"
	TRole         = "t_role"
	TRoleResource = "t_role_resource"
	TUserRole     = "t_user_role"
)

type ResourceVo struct {
	Id           int64  `json:"id"`
	ResourceName string `json:"resource_name"`
	ResourceType string `json:"resource_type"`
	Uri          string `json:"uri"`
	Actions      string `json:"actions"`
}

type RoleVo struct {
	Id       int64  `json:"id"`
	RoleName string `json:"role_name"`
	RoleDesc string `json:"role_desc"`
}

type RoleWithResourceVo struct {
	RoleVo    `json:"role"`
	Resources []ResourceVo `json:"resources"`
}

type UserWithRoleVo struct {
	account.UserBaseVo `json:"user"`
	Roles              []RoleWithResourceVo `json:"roles"`
}

type RoleResourceVo struct {
	Id         int64 `json:"id"`
	RoleId     int64 `json:"role_id"`
	ResourceId int64 `json:"resource_id"`
}

type UserRoleVo struct {
	Id     int64 `json:"id"`
	UserId int64 `json:"user_id"`
	RoleId int64 `json:"role_id"`
}
