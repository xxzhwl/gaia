package permission

import (
	"testing"

	_ "github.com/joho/godotenv/autoload"
)

func TestAddResource(t *testing.T) {
	type args struct {
		req AddResourceRequest
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{
			name: "test2",
			args: args{
				req: AddResourceRequest{
					ResourceType: "api",
					ResourceName: "demo测试服务",
					Uri:          "/api/demo",
					Ctx:          nil,
				},
			},
			wantErr: false,
		},
		{
			name: "test3",
			args: args{
				req: AddResourceRequest{
					ResourceType: "page",
					ResourceName: "访问test页面",
					Uri:          "/test",
					Ctx:          nil,
				},
			},
			wantErr: false,
		},
		{
			name: "test4",
			args: args{
				req: AddResourceRequest{
					ResourceType: "page",
					ResourceName: "访问404页面",
					Uri:          "/404",
					Ctx:          nil,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := AddResource(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("AddResource() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteResource(t *testing.T) {
	type args struct {
		req DeleteResourceRequest
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
		{
			name: "test1",
			args: args{
				req: DeleteResourceRequest{
					Ctx:        nil,
					ResourceId: 3,
				},
			},
			wantErr: false,
		},
		{
			name: "test2",
			args: args{
				req: DeleteResourceRequest{
					Ctx:        nil,
					ResourceId: 2,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DeleteResource(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("DeleteResource() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateResource(t *testing.T) {
	type args struct {
		req UpdateResourceRequest
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UpdateResource(tt.args.req); (err != nil) != tt.wantErr {
				t.Errorf("UpdateResource() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
