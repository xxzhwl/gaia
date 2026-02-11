// Package magicsort 将任意数据类型按照统一规则排序, 使其便于进行比较
// @author: wanlizhan
// @created: 2023-11-15
package magicsort

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/xxzhwl/gaia"
)

// M 类型别名
type M = map[string]any

// Any 排序任意类型
//
// 注意, 针对其中的slice是按照字母序排列，目的是按照稳定的规则重新组织被排序的若干对象，使其便于进行比较。
func Any(v any) (res any, err error) {
	return any_(v, true)
}

// AnyDesc Any排序的降序版本
func AnyDesc(v any) (res any, err error) {
	return any_(v, false)
}

func any_(v any, asc bool) (res any, err error) {
	res = v

	rv := reflect.ValueOf(v)
	k := rv.Kind()
	switch k {
	case reflect.Slice:
		return slice(v, asc)
	case reflect.Map:
		return map_(v, asc)
	case reflect.String:
		if str, ok := v.(string); ok {
			return string_(str, asc), nil
		}
		return
	}

	return
}

// String 对列表类字符串进行排序, 升序
//
// 注意统一使用第一个出现的分隔符 (;,|)
func String(v string) (res string) {
	return string_(v, true)
}

// StringDesc 对列表类字符串进行排序, 降序版
//
// 注意统一使用第一个出现的分隔符 (;,|)
func StringDesc(v string) (res string) {
	return string_(v, false)
}

func string_(v string, asc bool) (res string) {
	list := gaia.StringToList(v)
	if asc {
		sort.Strings(list)
	} else {
		sort.Slice(list, func(i, j int) bool { return list[i] > list[j] })
	}

	seperators := map[string]bool{
		";": true,
		",": true,
		"|": true,
	}

	sep := ","
	for _, r := range v {
		if seperators[string(r)] {
			sep = string(r)
			break
		}
	}

	// 使用统一分隔符
	res = strings.Join(list, sep)
	return
}

// Slice 排序任意列表，升序
//
// 注意, 这里默认是按照字母序排列，目的是按照稳定的规则重新组织被排序的若干对象，使其便于进行比较。
// 当切片完全是数字时，会按照数字排序，类型统一返回 []float64
func Slice(v any) (res any, err error) {
	return slice(v, true)
}

// SliceDesc Slice的降序版本
func SliceDesc(v any) (res any, err error) {
	return slice(v, false)
}

func slice(v any, asc bool) (res any, err error) {
	// 默认值
	res = v

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return
	}

	if rv.Len() == 0 {
		return
	}

	// 默认按照字符串排序
	var innerT = reflect.String
	if t, typeInferenceErr := gaia.GetSliceInnerType(v); typeInferenceErr == nil {
		innerT = t
	}

	switch innerT {
	case reflect.String:
		return sortSliceAsString(rv, asc)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return sortSliceAsNumber(rv, asc)
	}

	return
}

func sortSliceAsNumber(rv reflect.Value, asc bool) (res any, err error) {
	// 不丢失精度, 统一按照float64排序
	floatSlice := make([]float64, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		cv := rv.Index(i)
		// any 类型，获取底层元素
		if cv.Kind() == reflect.Interface {
			cv = cv.Elem()
		}
		// 转换成float64
		floatSlice[i] = cv.Convert(reflect.TypeOf(float64(0))).Float()
	}

	if asc {
		sort.Float64s(floatSlice)
	} else {
		sort.Slice(floatSlice, func(i, j int) bool { return floatSlice[i] > floatSlice[j] })
	}

	res = floatSlice

	return
}

func sortSliceAsString(rv reflect.Value, asc bool) (res any, err error) {
	// 直接按照序列化后的字符排序
	type sortItem struct {
		val        any
		identifier string
	}
	var toSort []sortItem
	for i := 0; i < rv.Len(); i++ {
		iv := rv.Index(i)
		// 对于无法取值的情况，直接跳过，比如nil/null
		if !iv.CanInterface() {
			continue
		}
		tmp := iv.Interface()
		var raw []byte
		raw, err = json.Marshal(tmp)
		if err != nil {
			return
		}
		toSort = append(toSort, sortItem{
			val:        tmp,
			identifier: string(raw),
		})
	}

	// 按 identifier 排序
	if asc {
		sort.Slice(toSort, func(i, j int) bool {
			return toSort[i].identifier < toSort[j].identifier
		})
	} else {
		sort.Slice(toSort, func(i, j int) bool {
			return toSort[i].identifier > toSort[j].identifier
		})
	}

	// 按序解出
	res = gaia.MapListByFunc(toSort, func(v sortItem) any { return v.val })

	return
}

// Map 排序任意 map, 升序
//
// 注意, 针对map内的slice是按照字母序排列，目的是按照稳定的规则重新组织被排序的若干对象，使其便于进行比较。
func Map(v any) (res any, err error) {
	return map_(v, true)
}

// MapDesc 排序任意 map，降序版
func MapDesc(v any) (res any, err error) {
	return map_(v, false)
}

func map_(v any, asc bool) (res any, err error) {
	// default
	res = v

	rv := reflect.ValueOf(v)
	k := rv.Kind()
	switch k {
	case reflect.Map:
		if rv.Len() == 0 {
			return
		}
		ks := rv.MapKeys()
		for _, key := range ks {
			var curV any
			mv := rv.MapIndex(key)
			// 无法取值，直接跳过
			if !mv.CanInterface() {
				continue
			}
			// 递归排序
			curV, err = any_(mv.Interface(), asc)
			if err != nil {
				return
			}
			// 更新为排序了的值
			rv.SetMapIndex(key, reflect.ValueOf(curV))
		}
		// 排完了之后设置返回值
		res = rv.Interface()
	}

	return
}
