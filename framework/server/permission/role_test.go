package permission

import "testing"

func TestAddRole(t *testing.T) {
	type args struct {
		req AddRoleRequest
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "test1",
			args: args{
				req: AddRoleRequest{
					RoleName:    "超级管理员",
					RoleDesc:    "最高级别身份",
					ResourceIds: []int64{1, 2, 3, 4},
					Ctx:         nil,
				},
			},
			wantErr: false,
		},
		{
			name: "test2",
			args: args{
				req: AddRoleRequest{
					RoleName:    "接口调用人",
					RoleDesc:    "只拥有接口调用权限",
					ResourceIds: []int64{1, 2},
					Ctx:         nil,
				},
			},
			wantErr: false,
		},
		{
			name: "test3",
			args: args{
				req: AddRoleRequest{
					RoleName:    "页面访问人",
					RoleDesc:    "只拥有页面访问权限",
					ResourceIds: []int64{3, 4},
					Ctx:         nil,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := AddRole(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("AddRole() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateRole(t *testing.T) {
	type args struct {
		req UpdateRoleRequest
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "test1",
			args: args{
				req: UpdateRoleRequest{
					RoleId:      4,
					ResourceIds: []int64{1, 2, 3, 4},
					Ctx:         nil,
				},
			},
			wantErr: false,
		},
		{
			name: "test2",
			args: args{
				req: UpdateRoleRequest{
					RoleId:      5,
					ResourceIds: []int64{1, 2},
					Ctx:         nil,
				},
			},
			wantErr: false,
		},
		{
			name: "test3",
			args: args{
				req: UpdateRoleRequest{
					RoleId:      6,
					ResourceIds: []int64{3, 4},
					Ctx:         nil,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UpdateRole(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("UpdateRole() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestName(t *testing.T) {
	resp, err := FindRole(FindRoleRequest{RoleName: "超级管理员"})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(resp)
}
