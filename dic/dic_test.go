// Package dic unit test
// @author wanlizhan
// @created 2023-07-28
package dic_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/xxzhwl/gaia/dic"
)

func TestGetInt64(t *testing.T) {
	testDatas := map[string]interface{}{
		"A": 123,
		"B": 123.1,
		"c": -123,
		"D": "123a",
	}
	for _, k := range []string{"A", "B", "c", "D", "E"} {
		val, err := dic.GetInt64(testDatas, k, "获取数据为空", 0)
		if err != nil {
			t.Log(err.Error())
		} else {
			t.Log(val)
		}

	}

}

func TestKeys(t *testing.T) {
	type args struct {
		dict map[int]int
	}
	tests := []struct {
		name    string
		args    args
		wantRes []int
	}{
		{
			name: "normal",
			args: args{
				dict: map[int]int{1: 1, 2: 3},
			},
			wantRes: []int{1, 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 因为map range无序，需排序后比较
			if gotRes := dic.Keys(tt.args.dict); !reflect.DeepEqual(sort.IntSlice(gotRes), sort.IntSlice(tt.wantRes)) {
				t.Errorf("Keys() = %v, want %v", gotRes, tt.wantRes)
			}
		})
	}
}

func TestVals(t *testing.T) {
	type args struct {
		dict map[int]int
	}
	tests := []struct {
		name    string
		args    args
		wantRes []int
	}{
		{
			name: "normal",
			args: args{
				dict: map[int]int{1: 1, 2: 3},
			},
			wantRes: []int{1, 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotRes := dic.Vals(tt.args.dict); !reflect.DeepEqual(sort.IntSlice(gotRes), sort.IntSlice(tt.wantRes)) {
				t.Errorf("Keys() = %v, want %v", gotRes, tt.wantRes)
			}
		})
	}
}

func TestGetSafeListMapInterface(t *testing.T) {
	type args struct {
		mapData      map[string]interface{}
		key          string
		defaultValue []map[string]interface{}
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "test-1",
			args: args{
				mapData: map[string]interface{}{
					"list": []map[string]interface{}{
						{
							"a": 1,
							"b": 2,
						},
						{
							"a": 11,
							"b": 22,
						},
					},
				},
				key:          "list",
				defaultValue: nil,
			},
		},
		{
			name: "test-2",
			args: args{
				mapData: map[string]interface{}{
					"list": []interface{}{
						map[string]interface{}{
							"a": 1,
							"b": 2,
						},
						map[string]interface{}{
							"a": 11,
							"b": 22,
						},
					},
				},
				key:          "list",
				defaultValue: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dic.GetSafeListMapInterface(tt.args.mapData, tt.args.key, tt.args.defaultValue)
			t.Log(got)
		})
	}
}

func TestGetValueByMapPath(t *testing.T) {
	jsonstr := `{
    "a":{
        "b":123,
        "c":{
            "d":true,
            "f":"hello"
        }
    }
}`
	var v map[string]any
	if err := json.Unmarshal([]byte(jsonstr), &v); err != nil {
		t.Fatal(err)
	}

	jsonstr4 := `{
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
	var v4 map[string]any
	if err := json.Unmarshal([]byte(jsonstr4), &v4); err != nil {
		t.Fatal(err)
	}

	type args struct {
		mapData     map[string]any
		keyPathName string
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
				mapData: map[string]any{
					"key1": map[string]any{
						"key2": map[string]any{
							"key3": 1234,
							"key4": "hello",
						},
					},
				},
				keyPathName: "key1.key2.key3",
			},
			want:    1234,
			wantErr: false,
		},
		{
			name: "test-2",
			args: args{
				mapData: map[string]any{
					"key1": map[string]any{
						"key2": map[string]any{
							"key3": 1234,
							"key4": "hello",
						},
					},
				},
				keyPathName: "key1.key2.key4",
			},
			want:    "hello",
			wantErr: false,
		},
		{
			name: "test-3",
			args: args{
				mapData:     v,
				keyPathName: "a.c.f",
			},
			want:    "hello",
			wantErr: false,
		},
		{
			name: "test-4",
			args: args{
				mapData:     v4,
				keyPathName: "info.title",
			},
			want:    "设备数据",
			wantErr: false,
		},
		{
			name: "test-5",
			args: args{
				mapData:     v4,
				keyPathName: "info.aaa",
			},
			want:    "",
			wantErr: true,
		},
		{
			name: "test-6",
			args: args{
				mapData:     v4,
				keyPathName: "list[0].objList[1].devName",
			},
			want:    "设备2",
			wantErr: false,
		},
		{
			name: "test-6",
			args: args{
				mapData:     v4,
				keyPathName: "info.rows[i].devName",
			},
			want:    []any{"设备1", "设备2"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dic.GetValueByMapPath(tt.args.mapData, tt.args.keyPathName)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetValueByMapPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				t.Log(err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetValueByMapPath() got = %v, want %v", got, tt.want)
			}
		})
	}
}
