// Package cvt 对结构体的 flatten 和 rebuild 操作
// @author: wanlizhan
// @created: 2023-07-06
package cvt

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestFlatten(t *testing.T) {
	type demo struct {
		Name string
		Info struct {
			Age  int
			Vals []string
		}
	}

	type args struct {
		val interface{}
	}

	tests := []struct {
		name string
		args args
	}{
		{
			name: "struct",
			args: args{
				val: demo{
					Name: "hello",
					Info: struct {
						Age  int
						Vals []string
					}{
						Age:  21,
						Vals: []string{"23", "24"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := FlattenStruct(tt.args.val)
			t.Log(res)
		})
	}
}

func Test_buildMap(t *testing.T) {
	type args struct {
		ks  []string
		val interface{}
	}

	tests := []struct {
		name    string
		args    args
		wantRes map[string]interface{}
	}{
		{
			name: "normal",
			args: args{
				ks:  []string{"a", "b", "c"},
				val: 23,
			},
			wantRes: map[string]interface{}{
				"a": map[string]interface{}{
					"b": map[string]interface{}{
						"c": 23,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotRes := buildMap(tt.args.ks, tt.args.val); !reflect.DeepEqual(gotRes, tt.wantRes) {
				t.Errorf("buildMap() = %v, want %v", gotRes, tt.wantRes)
			}
		})
	}
}

type temp struct {
	A struct {
		B struct {
			C int
		}
	}
}

func TestRebuildFlattenMap(t *testing.T) {

	var target temp

	type args struct {
		val    map[string]interface{}
		target interface{}
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "normal",
			args: args{
				val: map[string]interface{}{
					"a.b.c": 23,
				},
				target: &target,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := RebuildFlattenMap(tt.args.val, tt.args.target); (err != nil) != tt.wantErr {
				t.Errorf("RebuildFlattenMap() error = %v, wantErr %v", err, tt.wantErr)
			}

			t.Logf("%+v", target)
			if target.A.B.C != 23 {
				t.Fail()
			}
		})
	}
}

func TestRebuildFlattenJSON(t *testing.T) {
	var target map[string]interface{}

	type args struct {
		val    string
		target interface{}
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "normal",
			args: args{
				val: `
{
    "a.b.c": 23,
    "a.b.e.c": 23,
    "d.b.d": 23,
    "s.b.c": 21,
    "a.c.d": 21
}	
				`,
				target: &target,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := RebuildFlattenJSON(tt.args.val, tt.args.target); (err != nil) != tt.wantErr {
				t.Errorf("RebuildFlattenJSON() error = %v, wantErr %v", err, tt.wantErr)
			}

			bs, _ := json.MarshalIndent(target, "", " ")
			t.Logf("%s", bs)
		})
	}
}

func TestFlattenMap(t *testing.T) {
	type args struct {
		m map[string]any
	}
	tests := []struct {
		name string
		args args
		want map[string]any
	}{
		{
			name: "normal",
			args: args{
				m: map[string]any{
					"Variables": map[string]any{
						"a": "hello #name#",
						"b": map[string]any{
							"c": "age: #age#",
							"d": map[string]any{
								"e": []string{"aa", "bb", "#name#"},
								"f": map[string]any{
									"g": "multi: #name# #age# #addr.province#",
								},
								"h": 12,
								"i": true,
							},
						},
					},
				},
			},
			want: map[string]any{
				"Variables.a":   "hello #name#",
				"Variables.b.c": "age: #age#",
				"Variables.b.d.e": []string{
					"aa",
					"bb",
					"#name#",
				},
				"Variables.b.d.f.g": "multi: #name# #age# #addr.province#",
				"Variables.b.d.h":   12,
				"Variables.b.d.i":   true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 正确打平
			got := FlattenMap(tt.args.m)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FlattenMap() = %v, want %v", got, tt.want)
			}

			// 可重建
			rebuildResult := make(map[string]any)
			err := RebuildFlattenMap(got, &rebuildResult)
			if err != nil {
				t.Fatal(err)
			}

			orig := fmt.Sprint(tt.args.m)
			res := fmt.Sprint(rebuildResult)

			if !reflect.DeepEqual(orig, res) {
				t.Fatal("FlattenMap() result cannot be rebuilt to original map")
			}
		})
	}
}
