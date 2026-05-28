/*
Package dic 提供一种安全的map[string]interface{}数据项获取方式
主要是针对map[string]interface{}的数据类型，根据key获取确定的数据类型
特别注意，为了运行时安全起见，切换直接使用mapData[key]获取map中的值，如果值不存在，会抛出运行时异常，所以不建议使用mapData[key]这种形式获取数据
@author wanlizhan
@create 2023-11-28
*/
package dic

import (
	"errors"
	"github.com/xxzhwl/gaia/cvt"
	"reflect"
)

// Keys 返回map的所有键列表
func Keys[K comparable, V any](dict map[K]V) (res []K) {
	for k := range dict {
		res = append(res, k)
	}
	return
}

// Vals 返回map的所有值列表
func Vals[K comparable, V any](dict map[K]V) (res []V) {
	for _, v := range dict {
		res = append(res, v)
	}
	return
}

// Merge 将2个或多个map的数据合并成一个map
func Merge[T any](m ...map[string]T) map[string]T {
	retval := make(map[string]T)
	if len(m) > 0 {
		for _, itm := range m {
			if len(itm) > 0 {
				for key, val := range itm {
					retval[key] = val
				}
			}
		}
	}
	return retval
}

// GetMapKeys 获取Map的全部key，并返回列表
func GetMapKeys[T any](mapData map[string]T) []string {
	return Keys(mapData)
}

// GetInt64 尝试从一个map中获取一个int64类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据无法转换成int64，都认为是获取失败的情况
func GetInt64(mapData map[string]interface{}, key string, errmsg string, defaultValue int64) (int64, error) {
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
func GetSafeInt64(mapData map[string]interface{}, key string, defaultValue int64) int64 {
	intval, _ := GetInt64(mapData, key, "", defaultValue)
	return intval
}

// I 简单调用
// 尝试从一个map中安全的获取一个int64类型的数据，如果获取失败，返回0
func I(mapData map[string]interface{}, key string) int64 {
	return GetSafeInt64(mapData, key, 0)
}

// GetFloat64 尝试从一个map中获取一个float64类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据无法转换成float64，都认为是获取失败的情况
func GetFloat64(mapData map[string]interface{}, key string, errmsg string, defaultValue float64) (float64, error) {
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
func GetSafeFloat64(mapData map[string]interface{}, key string, defaultValue float64) float64 {
	fval, _ := GetFloat64(mapData, key, "", defaultValue)
	return fval
}

// F 简单调用
// 尝试从一个map中安全的获取一个float64类型的数据，如果获取失败，返回0
func F(mapData map[string]interface{}, key string) float64 {
	return GetSafeFloat64(mapData, key, 0)
}

// GetString 尝试从一个map中获取一个string类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据string为空，都认为是获取失败的情况
func GetString(mapData map[string]interface{}, key string, errmsg string, defaultValue string) (string, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	if v, ok := mapData[key]; ok {
		return cvt.GetString(v, errmsg, defaultValue)
	} else {
		return defaultValue, dErr(errmsg)
	}
}

// GetSafeString 尝试从一个map中安全的获取一个string类型的数据
func GetSafeString(mapData map[string]interface{}, key string, defaultValue string) string {
	sval, _ := GetString(mapData, key, "", defaultValue)
	return sval
}

// S 简单调用
// 尝试从一个map中安全的获取一个string类型的数据，如果不存在这样的key，返回""
func S(mapData map[string]interface{}, key string) string {
	return GetSafeString(mapData, key, "")
}

// GetMustString 将一个map[string]interface{}中的key的数据强制转换为字符串类型
// 如果是map, array, slice复合结构，则转换为JSON返回
func GetMustString(mapData map[string]interface{}, key string) string {
	if v, ok := mapData[key]; ok {
		return cvt.GetMustString(v)
	} else {
		return ""
	}
}

// GetBool 尝试从一个map中获取一个boolean类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据为空或不为bool，都认为是获取失败的情况
func GetBool(mapData map[string]interface{}, key string, errmsg string, defaultValue bool) (bool, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	data, dataOk := mapData[key]
	if !dataOk {
		return defaultValue, dErr(errmsg)
	}
	if retval, ok := data.(bool); ok {
		return retval, nil
	} else {
		return defaultValue, dErr(errmsg)
	}
}

// GetSafeBool 尝试从一个map中安全的获取一个bool类型的数据
func GetSafeBool(mapData map[string]interface{}, key string, defaultValue bool) bool {
	bval, _ := GetBool(mapData, key, "", defaultValue)
	return bval
}

// B 简单调用
// 尝试从一个map中安全的获取一个boolean类型的数据，如果不存在这样的key，返回false
func B(mapData map[string]interface{}, key string) bool {
	return GetSafeBool(mapData, key, false)
}

// GetMapInterface 获取一个map[string]interface{}类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据无法转成map[string]interface{}，都认为是获取失败的情况
func GetMapInterface(mapData map[string]interface{}, key string, errmsg string, defaultValue map[string]interface{}) (
	map[string]interface{}, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	if subValue, ok := mapData[key]; ok {
		//将获取到的值尝试转换为map[string]interface{}类型
		retval, retvalOk := subValue.(map[string]interface{})
		if retvalOk {
			//类型断言成功
			return retval, nil
		}
	}
	return defaultValue, dErr(errmsg)
}

// GetSafeMapInterface 尝试从一个map中安全的获取一个map[string]interface{}类型的数据
func GetSafeMapInterface(mapData map[string]interface{}, key string,
	defaultValue map[string]interface{}) map[string]interface{} {
	result, _ := GetMapInterface(mapData, key, "", defaultValue)
	return result
}

// GetListString 尝试从一个map类型中获取一个字符串列表[]string
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据无法转成[]string，都认为是获取失败的情况
func GetListString(mapData map[string]interface{}, key string, errmsg string, defaultValue []string) (
	[]string, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	retval := make([]string, 0)
	if subValue, ok := mapData[key]; ok {
		fv := reflect.ValueOf(subValue)
		switch fv.Kind() {
		case reflect.Array, reflect.Slice:
			size := fv.Len()
			for i := 0; i < size; i++ {
				str := cvt.GetSafeString(fv.Index(i).Interface(), "")
				if len(str) > 0 {
					retval = append(retval, str)
				}
			}
		}
	}
	if len(retval) > 0 {
		return retval, nil
	} else {
		return defaultValue, dErr(errmsg)
	}

}

// GetSafeListString 尝试从一个map中安全的获取一个[]string类型的数据
func GetSafeListString(mapData map[string]interface{}, key string, defaultValue []string) []string {
	result, _ := GetListString(mapData, key, "", defaultValue)
	return result
}

// GetListMapInterface 获取一个[]map[string]interface{}类型的数据
// 获取失败的情况下，如果errmsg不为空，则error返回errmsg的错误信息，否则error返回nil并返回默认值
// 针对mapData为空，mapData中不存在key的数据，mapData中对应key的数据无法转成[]map[string]interface{}，都认为是获取失败的情况
func GetListMapInterface(mapData map[string]interface{}, key string, errmsg string,
	defaultValue []map[string]interface{}) ([]map[string]interface{}, error) {
	if mapData == nil {
		return defaultValue, dErr(errmsg)
	}
	if subValue, ok := mapData[key]; ok {
		//将获取到的值尝试转换为[]map[string]interface{}类型
		if list, ok := subValue.([]map[string]interface{}); ok {
			return list, nil
		}
		list, listOk := subValue.([]interface{})
		if !listOk {
			return defaultValue, dErr(errmsg)
		}

		retval := make([]map[string]interface{}, 0)
		for _, itm := range list {
			if val, ok := itm.(map[string]interface{}); ok {
				retval = append(retval, val)
			} else {
				return retval, dErr(errmsg)
			}
		}
		return retval, nil
	}
	return defaultValue, dErr(errmsg)
}

// GetSafeListMapInterface 尝试从一个map中安全的获取一个[]map[string]interface{}类型的数据
func GetSafeListMapInterface(mapData map[string]interface{}, key string,
	defaultValue []map[string]interface{}) []map[string]interface{} {
	result, _ := GetListMapInterface(mapData, key, "", defaultValue)
	return result
}

// GetValueByMapPath 以路径的方式(key1.key2.key3...)获取一个 map[string]any 中的值
// 注意，这里的any，仍然需要是一个map[string]any，否则将获取失败
// keyPathName 格式如下
// key1.key2.key3...
// obj_list[i].key1
// list[0].objList[1].key1
// list[0].objList[i].key1
// []中如果是非数值(如 i, k 之类)，则获取整个列表中所有对应的值，返回的是一个列表
// []中如果是数值(如 0, 1, 2 之类)，则获取对应列表中元素的值
func GetValueByMapPath(mapData map[string]any, keyPathName string) (any, error) {
	return newValuePath().getValue(mapData, keyPathName)
}

// GetValueByMapPathWithKeyCaseInsensitive 同 GetValueByMapPath，key的匹配可以兼容大小写
func GetValueByMapPathWithKeyCaseInsensitive(mapData map[string]any, keyPathName string) (any, error) {
	obj := newValuePath()
	obj.setKeyCaseInsensitive()
	return obj.getValue(mapData, keyPathName)
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
