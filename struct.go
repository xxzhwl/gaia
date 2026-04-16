// Package gaia 结构体相关逻辑封装
// @author wanlizhan
// @created 2023-11-09
package gaia

import (
	"reflect"
)

// GetStructFields 获取给定结构体的可导出字段的列表，需传入结构体值对象 或 结构体指针
// 同时，支持结构体中存在匿名结构体，匿名结构体中的字段也会被获取到
// 如果传入的不是结构体，返回空
func GetStructFields(v any) (res []string) {
	if v == nil {
		return
	}
	ft := reflect.TypeOf(v)
	return _getStructFields(ft, "", res)
}

// GetStructFieldsByJsonTagPreference 获取给定结构体的可导出字段的列表，需传入结构体值对象 或 结构体指针
// 同时，支持结构体中存在匿名结构体，匿名结构体中的字段也会被获取到
// 如果传入的不是结构体，返回空
func GetStructFieldsByJsonTagPreference(v any) (res []string) {
	if v == nil {
		return
	}
	ft := reflect.TypeOf(v)
	return _getStructFields(ft, "json", res)
}

func _getStructFields(ft reflect.Type, tag string, fields []string) []string {
	if ft.Kind() == reflect.Ptr {
		ft = ft.Elem()
	}
	if ft.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < ft.NumField(); i++ {
		sfield := ft.Field(i)
		if sfield.Anonymous {
			//说明这是一个匿名结构体
			fields = _getStructFields(sfield.Type, tag, fields)
		} else {
			//说明这是一个结构体的普通属性
			if sfield.IsExported() {
				if len(tag) > 0 && len(sfield.Tag.Get(tag)) > 0 {
					fields = append(fields, sfield.Tag.Get(tag))
				} else {
					fields = append(fields, sfield.Name)
				}
			}
		}
	}
	return fields
}
