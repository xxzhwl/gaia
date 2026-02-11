/*
Package cvt 数据获取逻辑，主要是针对interface{}的数据类型，转换成确定的数据类型
@author wanlizhan
@create 2023-11-28
*/
package cvt

import (
	"reflect"
	"testing"
)

func TestIntToBool(t *testing.T) {
	type args struct {
		v int
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "false",
			args: args{
				v: 0,
			},
			want: false,
		},
		{
			name: "true",
			args: args{
				v: 1,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NumToBool(tt.args.v); got != tt.want {
				t.Errorf("IntToBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBoolToInt(t *testing.T) {
	type args struct {
		v bool
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "true",
			args: args{
				v: true,
			},
			want: 1,
		},
		{
			name: "false",
			args: args{
				v: false,
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BoolToInt(tt.args.v); got != tt.want {
				t.Errorf("BoolToInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetStringList(t *testing.T) {
	type args struct {
		v            any
		errmsg       string
		defaultValue []string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{"testStringList", args{
			v:            []string{"1", "2", "3"},
			errmsg:       "错误",
			defaultValue: []string{},
		}, []string{"1", "2", "3"}, false},
		{"testInt", args{
			v:            []int{1, 2, 3},
			errmsg:       "错误",
			defaultValue: []string{},
		}, []string{"1", "2", "3"}, false},
		{"testInt", args{
			v:            []float64{1.2, 2.0, 3.0},
			errmsg:       "错误",
			defaultValue: []string{},
		}, []string{"1.2", "2", "3"}, false},
		{"testBool", args{
			v:            []bool{true, false, true},
			errmsg:       "错误",
			defaultValue: []string{},
		}, []string{"1", "0", "1"}, false},
		{"testanyList", args{
			v:            []any{"1", "2", "3", 4e3},
			errmsg:       "错误",
			defaultValue: []string{},
		}, []string{"1", "2", "3", "4000"}, false},
		{"testanyMapList", args{
			v:            []any{"1", "2", "3", map[string]any{"1": 1}},
			errmsg:       "错误",
			defaultValue: []string{},
		}, []string{}, true},
		{"testMap", args{
			v:            map[string]any{"1": 1, "2": "2"},
			errmsg:       "错误",
			defaultValue: []string{},
		}, []string{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetStringList(tt.args.v, tt.args.errmsg, tt.args.defaultValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetStringList() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			t.Log(err)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetStringList() got = %v, want %v", got, tt.want)
			}
		})
	}
}
