/*
Package dics 提供一种安全的map[string]string数据项获取方式(比如来源于mysql的数据格式)
主要是针对map[string]string的数据类型，根据key获取确定的数据类型
特别注意，为了运行时安全起见，切换直接使用mapData[key]获取map中的值，如果值不存在，会抛出运行时异常，所以不建议使用mapData[key]这种形式获取数据
@author wanlizhan
@create 2023-12-27
*/
package dics

import (
	"errors"

	"github.com/xxzhwl/gaia/cvt"
)

// GetInt64 尝试从一个map中获取一个int64类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据无法转换成int64，都认为是获取失败的情况
func GetInt64(mapData map[string]string, key string, errmsg string, defaultValue int64) (int64, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	if v, ok := mapData[key]; ok {
		//存在key
		return cvt.GetInt64(v, errmsg, 0)
	} else {
		//不存在key
		return defaultValue, dErr(errmsg)
	}
}

// GetSafeInt64 尝试从一个map中安全的获取一个int64类型的数据
func GetSafeInt64(mapData map[string]string, key string, defaultValue int64) int64 {
	intval, _ := GetInt64(mapData, key, "", defaultValue)
	return intval
}

// I 简单调用
// 尝试从一个map中安全的获取一个int64类型的数据，如果获取失败，返回0
func I(mapData map[string]string, key string) int64 {
	return GetSafeInt64(mapData, key, 0)
}

// GetFloat64 尝试从一个map中获取一个float64类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据无法转换成float64，都认为是获取失败的情况
func GetFloat64(mapData map[string]string, key string, errmsg string, defaultValue float64) (float64, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	if v, ok := mapData[key]; ok {
		return cvt.GetFloat64(v, errmsg, defaultValue)
	} else {
		return defaultValue, dErr(errmsg)
	}
}

// GetSafeFloat64 尝试从一个map中安全的获取一个float64类型的数据
func GetSafeFloat64(mapData map[string]string, key string, defaultValue float64) float64 {
	fval, _ := GetFloat64(mapData, key, "", defaultValue)
	return fval
}

// F 简单调用
// 尝试从一个map中安全的获取一个float64类型的数据，如果获取失败，返回0
func F(mapData map[string]string, key string) float64 {
	return GetSafeFloat64(mapData, key, 0)
}

// GetString 尝试从一个map中获取一个string类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据string为空，都认为是获取失败的情况
func GetString(mapData map[string]string, key string, errmsg string, defaultValue string) (string, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	if v, ok := mapData[key]; ok {
		if len(v) > 0 {
			//不为空
			return v, nil
		}
	}
	return defaultValue, dErr(errmsg)
}

// GetSafeString 尝试从一个map中安全的获取一个string类型的数据
func GetSafeString(mapData map[string]string, key string, defaultValue string) string {
	sval, _ := GetString(mapData, key, "", defaultValue)
	return sval
}

// S 简单调用
// 尝试从一个map中安全的获取一个string类型的数据，如果不存在这样的key，返回""
func S(mapData map[string]string, key string) string {
	return GetSafeString(mapData, key, "")
}

// GetBool 尝试从一个map[string]string中获取一个boolean类型的数据，对应的数据为1或true，则返回true，
// 对应数据为0或false，则返回false，否则认为获取失败
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
func GetBool(mapData map[string]string, key string, errmsg string, defaultValue bool) (bool, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	data, dataOk := mapData[key]
	if !dataOk {
		return defaultValue, dErr(errmsg)
	}
	if data == "0" || data == "false" {
		return false, nil
	}
	if data == "1" || data == "true" {
		return true, nil
	}
	return defaultValue, dErr(errmsg)
}

// GetSafeBool 尝试从一个map中安全的获取一个bool类型的数据
func GetSafeBool(mapData map[string]string, key string, defaultValue bool) bool {
	bval, _ := GetBool(mapData, key, "", defaultValue)
	return bval
}

// Merge 将m2合并到m1中，如果m1,m2存在相同的key，则使用m2覆盖m1中相同key的内容
// m1,m2中的内容均不会被破坏
// 返回新合并后的数据
func Merge(m1, m2 map[string]string) map[string]string {
	retval := make(map[string]string)
	if len(m1) > 0 {
		for k, v := range m1 {
			retval[k] = v
		}
	}
	if len(m2) > 0 {
		for k, v := range m2 {
			retval[k] = v
		}
	}
	return retval
}

// 通过errmsg，生成error信息
// 如果errmsg为空，则返回nil
func dErr(errmsg string) error {
	if len(errmsg) > 0 {
		return errors.New(errmsg)
	} else {
		return nil
	}
}
