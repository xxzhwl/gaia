package permission

import (
	"context"
	"errors"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/server"
	"github.com/xxzhwl/gaia/framework/server/account"
	"gorm.io/gorm"
	"time"
)

type AddUserRequest struct {
	UserName       string          `json:"user_name" require:"1"`
	Password       string          `json:"password" require:"1"`
	Mail           string          `json:"mail"`
	PhoneRegionNum int64           `json:"phone_region_num"`
	PhoneNum       string          `json:"phone_num"`
	RoleIds        []int64         `json:"role_ids" require:"1"`
	Ctx            context.Context `json:"ctx"`
}

func AddUser(req AddUserRequest) (err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	return db.GetGormDb().Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(req.Ctx)

		newUser := account.UserVo{
			Password:   req.Password,
			CreateTime: time.Now(),
			UpdateTime: time.Now(),
			UserBaseVo: account.UserBaseVo{
				UserName:        req.UserName,
				Mail:            req.Mail,
				PhoneRegionNum:  req.PhoneRegionNum,
				PhoneNum:        req.PhoneNum,
				CreateTimeStamp: time.Now().UnixMilli(),
				UpdateTimeStamp: time.Now().UnixMilli(),
			},
		}

		newUser.Password, err = account.EncryptPassword(newUser.Password)
		if err != nil {
			return err
		}
		createTx := tx.Table(account.TUser).Create(&newUser)
		if createTx.Error != nil {
			return createTx.Error
		}

		roleIds := []int64{}
		findRoleTx := tx.Table(TRole).Select("id").Find(&roleIds, "id in ?", req.RoleIds)
		if findRoleTx.Error != nil {
			return findRoleTx.Error
		}

		userRoles := []UserRoleVo{}
		for _, id := range gaia.UniqueList(roleIds) {
			userRoles = append(userRoles, UserRoleVo{
				UserId: newUser.Id,
				RoleId: id,
			})
		}
		createURTx := tx.Table(TUserRole).Create(&userRoles)
		if createURTx.Error != nil {
			return createURTx.Error
		}
		return nil
	})
}

type UpdateUserRequest struct {
	UserId         int64           `json:"user_id" require:"1"`
	UserName       string          `json:"user_name"`
	Password       string          `json:"password"`
	Mail           string          `json:"mail"`
	PhoneRegionNum int64           `json:"phone_region_num"`
	PhoneNum       string          `json:"phone_num"`
	RoleIds        []int64         `json:"role_ids"`
	IsBan          int             `json:"is_ban"`
	IsLogOut       int             `json:"is_log_out"`
	Ctx            context.Context `json:"ctx"`
}

func UpdateUser(req UpdateUserRequest) (err error) {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}
	return db.GetGormDb().Transaction(func(tx *gorm.DB) error {
		tx = tx.WithContext(req.Ctx)

		newUser := account.UserVo{}

		findUserTx := tx.Table(account.TUser).Find(&newUser, "id=?", req.UserId)
		if findUserTx.Error != nil {
			return findUserTx.Error
		}
		if newUser.Id == 0 {
			return errors.New("not found user")
		}

		if len(req.Password) != 0 {
			req.Password, err = account.EncryptPassword(req.Password)
			if err != nil {
				return err
			}
		}

		updateUserTx := tx.Table(account.TUser).Where("id=?", newUser.Id).Updates(&account.UserVo{
			Password:   req.Password,
			UpdateTime: time.Now(),
			UserBaseVo: account.UserBaseVo{
				UserName:        req.UserName,
				Mail:            req.Mail,
				PhoneRegionNum:  req.PhoneRegionNum,
				PhoneNum:        req.PhoneNum,
				IsBan:           req.IsBan,
				IsLogOut:        req.IsLogOut,
				UpdateTimeStamp: time.Now().UnixMilli(),
			},
		})
		if updateUserTx.Error != nil {
			return updateUserTx.Error
		}

		roleIds := []int64{}
		findRoleTx := tx.Table(TRole).Select("id").Find(&roleIds, "id in ?", req.RoleIds)
		if findRoleTx.Error != nil {
			return findRoleTx.Error
		}

		oldUserRoles := []UserRoleVo{}

		findOldUr := tx.Table(TUserRole).Find(&oldUserRoles, "user_id = ?", req.UserId)
		if findOldUr.Error != nil {
			return findOldUr.Error
		}

		oldRoleIds := []int64{}
		for _, item := range oldUserRoles {
			oldRoleIds = append(oldRoleIds, item.RoleId)
		}

		deleteUr, addUr := gaia.DifferenceList(oldRoleIds, roleIds)

		t := tx.Table(TUserRole).Delete(&UserRoleVo{}, "user_id = ? and role_id in ?", req.UserId, deleteUr)
		if t.Error != nil {
			return t.Error
		}

		userRoles := []UserRoleVo{}
		for _, id := range gaia.UniqueList(addUr) {
			userRoles = append(userRoles, UserRoleVo{
				UserId: newUser.Id,
				RoleId: id,
			})
		}
		createURTx := tx.Table(TUserRole).Create(&userRoles)
		if createURTx.Error != nil {
			return createURTx.Error
		}

		return nil
	})
}

type FindUserRequest struct {
	UserName string          `json:"user_name"`
	Mail     string          `json:"mail"`
	Start    int             `json:"start"`
	Size     int             `json:"size"`
	Ctx      context.Context `json:"ctx"`
}

type FindUserWithRoleResponse struct {
	Id             int64  `json:"id"`
	UserName       string `json:"user_name"`
	Mail           string `json:"mail"`
	PhoneRegionNum int64  `json:"phone_region_num"`
	PhoneNum       string `json:"phone_num"`
	IsBan          int    `json:"is_ban"`     //被禁
	IsLogOut       int    `json:"is_log_out"` //用户注销
	CreateTime     string `json:"create_time"`
	UpdateTime     string `json:"update_time"`

	RoleNames string `json:"role_names"`
	RoleIds   string `json:"role_ids"`
	RoleDesc  string `json:"role_desc"`
}

func FindUser(req FindUserRequest) (response server.ListResponse[FindUserWithRoleResponse], err error) {
	res := []FindUserWithRoleResponse{}
	response = server.ListResponse[FindUserWithRoleResponse]{List: res}
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return
	}

	start, size := 0, 20
	if req.Start >= 0 {
		start = req.Start
	}
	if req.Size > 0 {
		size = req.Size
	}

	tx := db.GetGormDb().WithContext(req.Ctx).Table(account.TUser).
		Select("t_user.id,t_user.user_name,t_user.mail," +
			"t_user.phone_region_num,t_user.phone_num,t_user.is_ban,t_user.is_log_out," +
			"t_user.create_time,t_user.update_time," +
			"group_concat(t_role.role_name separator ',')  as role_names," +
			"group_concat(t_role.id separator ',' ) as role_ids," +
			"group_concat(t_role.role_desc separator ',' )as role_desc").
		Joins("left join t_user_role on t_user.id = t_user_role.user_id").
		Joins("left join t_role on t_role.id = t_user_role.role_id").Group("t_user.id")

	if len(req.UserName) > 0 {
		tx = tx.Where("t_user.user_name = ?", req.UserName)
	}
	if len(req.Mail) > 0 {
		tx = tx.Where("t_user.mail = ?", req.Mail)
	}
	var count int64 = 0
	tx = tx.Find(&res).Count(&count).Offset(start).Limit(size)
	if tx.Error != nil {
		return response, tx.Error
	}
	if len(res) > size {
		response.HasNext = true
	}
	response.List = res
	response.Total = count
	return
}
