// Package cvt 对结构体的 flatten 和 rebuild 操作
// @author: wanlizhan
// @created: 2023-07-06
package cvt

import (
	"encoding/json"
	"reflect"
	"strings"
)

var sep string = "."
var maxDepth = 100

// FlattenMap 将输入的 map 转换成打平的 map 结构，默认分隔符是 `.`, 可通过 SetSep() 方法设置自定义分隔符.
// 打平后的 map 可通过 RebuildFlattenMap() 方法重建成原来结构的 map
//
// 如 {"a": {"b": 1}} => {"a.b": 1}
func FlattenMap(m map[string]any) map[string]any {
	container := make(map[string]any)
	flattenMap("", m, container)
	return container
}

func flattenMap(parent string, val any, m map[string]any) (key string, res any) {
	// 递归终止
	v := reflect.ValueOf(val)
	if v.Kind() != reflect.Map || v.IsNil() {
		return parent, val
	}

	// 处理空map
	if v.Len() == 0 {
		m[parent] = val
	}

	for _, k := range v.MapKeys() {
		// 记录路径
		var field string
		if parent != "" {
			field = parent + sep + k.String()
		} else {
			field = k.String()
		}

		// 对于map中的值继续递归
		key, val = flattenMap(field, v.MapIndex(k).Interface(), m)
		if len(key) > 0 && val != nil {
			m[key] = val
		}
	}

	return parent, res
}

// FlattenStruct 将输入的结构体值转换成打平的 map 结构. 默认分隔符是`.`, 可通过 SetSep() 方法设置自定义分隔符
//
// 如 {"a": {"b": 1}} => {"a.b": 1}
func FlattenStruct(val interface{}) (res map[string]interface{}) {
	container := make(map[string]interface{})
	flattenStruct("", val, container)
	return container
}

func flattenStruct(parent string, val interface{}, m map[string]interface{}) (key string, res interface{}) {
	// 递归终止条件
	v := reflect.ValueOf(val)
	if v.Kind() != reflect.Struct {
		return parent, val
	}

	// 处理空结构体
	if v.NumField() == 0 {
		m[parent] = val
	}

	for i := 0; i < v.NumField(); i++ {
		vt := v.Type().Field(i).Name
		var field string
		if parent != "" {
			field = parent + sep + vt
		} else {
			field = vt
		}

		// 对结构体递归构造 k, v 结构并更新 map
		var k string
		k, val = flattenStruct(field, v.Field(i).Interface(), m)
		if len(k) > 0 && val != nil {
			m[k] = val
		}
	}

	return parent, res
}

// RebuildFlattenJSON ... 将打平的 json 重建成树状结构，target 应该是一个结构体指针
func RebuildFlattenJSON(val string, target interface{}) (err error) {
	var v map[string]interface{}
	err = json.Unmarshal([]byte(val), &v)
	if err != nil {
		return
	}

	return RebuildFlattenMap(v, target)
}

// RebuildFlattenMap 将打平的 map 重建成树状结构，target 应该是一个结构体指针
func RebuildFlattenMap(val map[string]interface{}, target interface{}) (err error) {
	var maps []map[string]interface{}
	for rawKey, val := range val {
		ks := strings.Split(rawKey, sep)
		maps = append(maps, buildMap(ks, val))
	}

	// 合并 map
	init := make(map[string]interface{})
	for i := range maps {
		init = MergeMap(maps[i], init)
	}

	bs, err := json.Marshal(init)
	if err != nil {
		return
	}

	return json.Unmarshal(bs, target)
}

func buildMap(ks []string, val interface{}) (res map[string]interface{}) {
	if len(ks) == 0 {
		return
	}

	// 自底向上重建树状 map
	var temp interface{}
	temp = val
	for i := len(ks) - 1; i >= 0; i-- {
		temp = map[string]interface{}{ks[i]: temp}
	}

	return temp.(map[string]interface{})
}

// SetSep 设置分隔符, 默认分隔符是 .
func SetSep(val string) {
	sep = val
}

// SetMaxMergeDepth 设置合并map时的最大深度
func SetMaxMergeDepth(depth int) {
	maxDepth = depth
}

// MergeMap 将两个 map 递归合并, 默认最大深度为 100
func MergeMap(src, dst map[string]interface{}) map[string]interface{} {
	return merge(src, dst, 0)
}

func merge(src, dst map[string]interface{}, depth int) map[string]interface{} {
	if depth > maxDepth {
		return src
	}

	for key, srcVal := range dst {
		if dstVal, ok := src[key]; ok {
			srcMap, srcMapOk := toMap(srcVal)
			dstMap, dstMapOk := toMap(dstVal)
			if srcMapOk && dstMapOk {
				srcVal = merge(dstMap, srcMap, depth+1)
			}
		}
		src[key] = srcVal
	}
	return src
}

func toMap(i interface{}) (map[string]interface{}, bool) {
	value := reflect.ValueOf(i)
	if value.Kind() == reflect.Map {
		m := map[string]interface{}{}
		for _, k := range value.MapKeys() {
			m[k.String()] = value.MapIndex(k).Interface()
		}
		return m, true
	}
	return map[string]interface{}{}, false
}
