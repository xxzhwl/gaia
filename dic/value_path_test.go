// Package dic comment_from_here
// @author wanlizhan
// @created 2023-03-11
package dic

import (
	"encoding/json"
	"reflect"
	"testing"
)

func Test_valuePath_getValue(t *testing.T) {
	jsonstr1 := `{
	"id": 123,
    "list": [
        {
            "objList": [
                {
                    "devName": "设备1"
                }, 
                {
                    "devName": "设备2"
                }
            ]
        }
    ], 
    "info": {
        "title": "设备数据", 
        "rows": [
            {
                "devName": "设备1"
            }, 
            {
                "devName": "设备2"
            }
        ]
    }, 
    "obj_list": [
        {
            "devName": "设备1", 
            "portList": [
                "T1/1", 
                "T1/2"
            ]
        }, 
        {
            "devName": "设备2", 
            "portList": [
                "T2/1", 
                "T2/2"
            ]
        }
    ]
}`
	var v1 map[string]any
	if err := json.Unmarshal([]byte(jsonstr1), &v1); err != nil {
		t.Fatal(err)
	}

	type args struct {
		rawMap  map[string]any
		keypath string
	}
	tests := []struct {
		name    string
		args    args
		want    any
		wantErr bool
	}{
		{
			name: "test-1",
			args: args{
				rawMap:  v1,
				keypath: "info.title",
			},
			want:    "设备数据",
			wantErr: false,
		},
		{
			name: "test-2",
			args: args{
				rawMap:  v1,
				keypath: "obj_list[i].devName",
			},
			want:    []any{"设备1", "设备2"},
			wantErr: false,
		},
		{
			name: "test-3",
			args: args{
				rawMap:  v1,
				keypath: "obj_list[0].devName",
			},
			want:    "设备1",
			wantErr: false,
		},
		{
			name: "test-4",
			args: args{
				rawMap:  v1,
				keypath: "obj_list[i].portList",
			},
			want: []any{
				[]any{"T1/1", "T1/2"},
				[]any{"T2/1", "T2/2"},
			},
			wantErr: false,
		},
		{
			name: "test-5",
			args: args{
				rawMap:  v1,
				keypath: "list[0].objList[1].devName",
			},
			want:    "设备2",
			wantErr: false,
		},
		{
			name: "test-6",
			args: args{
				rawMap:  v1,
				keypath: "list.aaaa",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "test-7",
			args: args{
				rawMap:  v1,
				keypath: "id",
			},
			want:    float64(123),
			wantErr: false,
		},
		{
			name: "test-8",
			args: args{
				rawMap:  nil,
				keypath: "id",
			},
			want:    nil,
			wantErr: true,
		},
	}

	//obj_list[i].devName, 读取返回一个数组，是：["设备1", "设备2"]
	//obj_list[i].portList, 读取返回一个数组，是：[["T1/1", "T1/2"], ["T2/1", "T2/2"]]
	//info.title，读取结果：设备数据
	//list[0].objList[1].devName，读取结果：设备2
	//list[0].objList[i].devName，读取结果：["设备1", "设备2"]
	//list.aaa，读取结果：读取不到，报错信息“list没有属性aaa”
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			o := newValuePath()
			got, err := o.getValue(tt.args.rawMap, tt.args.keypath)
			if (err != nil) != tt.wantErr {
				t.Errorf("getValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				t.Log(err.Error())
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getValue() got = %v, want %v", got, tt.want)
			}
		})
	}
}
