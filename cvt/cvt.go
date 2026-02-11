// Package cvt 数据获取逻辑，主要是针对interface{}的数据类型，转换成确定的数据类型
// @author wanlizhan
// @create 2023-11-28
package cvt

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/xxzhwl/gaia/defs"
)

// GetInt64 尝试获取一个int64类型的数据
// 其中string类型也将尝试转换成int类型
// 如果获取失败，则返回error，错误信息为指定的errmsg
func GetInt64(v any, errmsg string, defaultValue int64) (int64, error) {
	//对于float类型，如果被转成了科学计数法的形式，将会失败
	switch val := v.(type) {
	case float64:
		return int64(val), nil
	case float32:
		return int64(val), nil
	default:
		strv := fmt.Sprintf("%v", v)

		//兼容
		//str="12347778"
		//str="1.6612766913592737e+18"
		//str="1234.457" 形式的转换

		if i, err := strconv.ParseInt(strv, 10, 64); err == nil {
			return i, nil
		}
		if i, err := strconv.ParseFloat(strv, 64); err == nil {
			return int64(i), nil
		}

		//到此，说明此数据无法进行转换，即转换失败
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}

}

// GetSafeInt64 尝试获取一个int64的类型数据，如果获取失败，返回默认值
func GetSafeInt64(v any, defaultValue int64) int64 {
	val, _ := GetInt64(v, "", defaultValue)
	return val
}

// GetFloat64 尝试获取一个float64类型的数据
// 其实string类型，int类型，都将尝试转成float64类型
// 如果获取失败，则返回error，其提示内容为errmsg指定的信息
func GetFloat64(v any, errmsg string, defaultValue float64) (float64, error) {
	strv := fmt.Sprintf("%v", v)
	if fv, err := strconv.ParseFloat(strv, 64); err != nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	} else {
		return fv, nil
	}
}

// GetSafeFloat64 尝试获取一个float64类型的数据，如果获取失败，返回默认值
func GetSafeFloat64(v any, defaultValue float64) float64 {
	val, _ := GetFloat64(v, "", defaultValue)
	return val
}

// GetString 尝试获取一个字符串数据
// 其中int类型，float类型，也将尝试转换成string类型返回
// 如果获取失败，返回一个error，内容为errmsg信息
func GetString(v any, errmsg string, defaultValue string) (string, error) {
	if v == nil {
		v = ""
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.String, reflect.Int64, reflect.Uint64, reflect.Int32, reflect.Uint32,
		reflect.Int, reflect.Uint, reflect.Int16, reflect.Uint16, reflect.Int8, reflect.Uint8:
		//string或int
		val := fmt.Sprintf("%v", v)
		if len(val) > 0 {
			return val, nil
		}
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	case reflect.Float64, reflect.Float32:
		//float类型
		if f64, ok := v.(float64); ok {
			return strconv.FormatFloat(f64, 'f', -1, 64), nil
		}
		if f32, ok := v.(float32); ok {
			return strconv.FormatFloat(float64(f32), 'f', -1, 64), nil
		}
		return defaultValue, errors.New("float can not convert")
	case reflect.Bool:
		//bool类型，使用0或1标识结果
		if b, ok := v.(bool); ok {
			if b {
				return "1", nil
			} else {
				return "0", nil
			}
		} else {
			return defaultValue, errors.New("bool can not convert")
		}
	default:
		//非string, int或float类型，不支持向string转换的类型
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}
}

// GetSafeString 尝试获取一个string类型的数据，如果获取失败，返回默认值
func GetSafeString(v any, defaultValue string) string {
	val, _ := GetString(v, "", defaultValue)
	return val
}

// GetSafeStringList 尝试获取一个string类型的列表数据，如果获取失败，返回默认值
func GetSafeStringList(v any, defaultValue []string) []string {
	val, _ := GetStringList(v, "", defaultValue)
	return val
}

// GetStringList 尝试获取一个字符串列表数据
// 其中int类型，float类型，也将尝试转换成string类型返回
// 如果获取失败，返回一个error，内容为errmsg信息
func GetStringList(v any, errmsg string, defaultValue []string) ([]string, error) {
	if v == nil {
		v = []string{}
	}

	//判断v是否是slice，如果不是slice 直接返回
	if !(reflect.TypeOf(v).Kind() == reflect.Slice) {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}
	// 处理每个元素
	sliceValue := reflect.ValueOf(v)
	var err error
	result := make([]string, sliceValue.Len())
	for i := 0; i < sliceValue.Len(); i++ {
		result[i], err = GetString(sliceValue.Index(i).Interface(), errmsg, "")
		if err != nil {
			return defaultValue, err
		}
	}
	return result, nil
}

// GetMustString 将数据强制转换为字符串类型
// 如果是map, array, slice复合结构，则转换为JSON返回
func GetMustString(v any) string {
	if v == nil {
		return ""
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		jsn, _ := json.Marshal(v)
		return string(jsn)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// NumToBool 将数字类型转换为布尔类型
func NumToBool[T defs.NumberType](v T) bool {
	return v != 0
}

// BoolToInt 将bool类型转换为int值
func BoolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// GetBool 尝试获取一个bool类型
// 其中int类型，float类型，0则为true，其他false
// 字符串类型，长度大于0为true，其他false
// 如果获取失败，返回一个error，内容为errmsg信息
func GetBool(v any, errmsg string, defaultValue bool) (bool, error) {
	if v == nil {
		v = ""
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Bool:
		return v.(bool), nil
	case reflect.String:
		if len(v.(string)) > 0 {
			return true, nil
		}
		return false, nil
	case reflect.Int64, reflect.Uint64, reflect.Int32, reflect.Uint32,
		reflect.Int, reflect.Uint, reflect.Int16, reflect.Uint16, reflect.Int8, reflect.Uint8:
		val := fmt.Sprintf("%v", v)
		if val != "0" {
			return true, nil
		}
		return false, nil
	case reflect.Float64, reflect.Float32:
		//float类型
		if f64, ok := v.(float64); ok && f64 != 0 {
			return true, nil
		}
		if f32, ok := v.(float32); ok && f32 != 0 {
			return true, nil
		}
		return false, nil
	default:
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}
}

// GetSafeBool 尝试获取一个安全的bool类型
func GetSafeBool(v any) bool {
	res, _ := GetBool(v, "", false)
	return res
}
