// Package gaia 与类型相关的业务逻辑封装
// @author wanlizhan
// @created 2023-06-30
package gaia

import (
	"reflect"
	"strconv"
)

// Empty 判断某单个数据是否为空，如果为空则返回true，否则返回false
// 空值范围String, Slice, Array, Map为空，或数据为nil
func Empty(v any) bool {
	// FIXME(wanlizhan): 结构体中，字段为nil时，返回值(false)不符合预期(true)
	if v == nil {
		return true
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map, reflect.Chan:
		if reflect.ValueOf(v).Len() == 0 {
			return true
		}
	case reflect.Int, reflect.Uint, reflect.Int8, reflect.Uint8, reflect.Int16, reflect.Uint16, reflect.Int32, reflect.Uint32, reflect.Int64, reflect.Uint64:
		if v.(int64) == 0 {
			return true
		}
	case reflect.Float32, reflect.Float64:
		if v.(float64) == 0 {
			return true
		}
	}
	return false
}

// EmptyStruct 判断一个结构体所有成员是否为零值。若均为零值，则返回true。
// 若传入String, Slice, Array, Map，则判断是否为空，或数据为nil。如果为空则返回true，否则返回false。相当于Empty()的扩展。
func EmptyStruct(val any) bool {
	if val == nil {
		return true
	}
	fv := reflect.ValueOf(val)
	if fv.Kind() != reflect.Struct {
		return Empty(val)
	}
	return reflect.DeepEqual(fv.Interface(), reflect.New(fv.Type()).Elem().Interface())
}

// IsFloat 判断一个字符串是否为float类型的数据
func IsFloat(str string) bool {
	if _, err := strconv.ParseFloat(str, 64); err == nil {
		return true
	}
	return false
}

// IsNumber 判断一个字符串是否为int类型的数据
func IsNumber(str string) bool {
	if _, err := strconv.ParseInt(str, 10, 64); err == nil {
		return true
	}
	return false
}

// IsIntOrFloat 判断一个参数是不是Int类型或者float类型
func IsIntOrFloat(val any) bool {
	switch val.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

// IsSlice 判断某数据是否为slice类型
func IsSlice(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Slice
}

// IsMap 判断某数据是否为map类型
func IsMap(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Map
}

// IsArray 判断某数据是否为array类型
func IsArray(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Array
}

// IsStruct 判断某个数据的数据类型是否为struct类型，struct的指针类型不算
func IsStruct(v any) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Struct
}

// IsStructPtr 判断某个数据的数据类型是否为struct的指针类型
func IsStructPtr(v any) bool {
	if v == nil {
		return false
	}
	ft := reflect.TypeOf(v)
	if ft.Kind() != reflect.Ptr {
		return false
	}

	//到此，说明v是Ptr类型
	return ft.Elem().Kind() == reflect.Struct
}

// IsComplexType 判断一个数组是否为复合类型，包括array, slice, map, struct类型
func IsComplexType(v any) bool {
	if v == nil {
		return false
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
		return true
	}
	return false
}
