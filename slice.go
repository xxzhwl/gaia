// Package gaia 提供slice相关的工具方法
// @author: wanlizhan
// @created: 2023-11-16
package gaia

import (
	"errors"
	"reflect"
)

// GetSliceInnerType 获取切片内部的元素类型。如果是从json转换过来的可能包含多个类型，此时报错。
func GetSliceInnerType(v any) (t reflect.Kind, err error) {
	if v == nil {
		err = errors.New("given slice is nil")
		return
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		err = errors.New("given value is not a slice")
		return
	}

	if rv.Len() == 0 {
		err = errors.New("given slice is empty")
		return
	}

	m := map[reflect.Kind]bool{}
	for i := 0; i < rv.Len(); i++ {
		t = rv.Index(i).Kind()
		// any 类型需拿到底层真实类型
		if t == reflect.Interface {
			t = rv.Index(i).Elem().Kind()
		}
		m[t] = true
	}

	if len(m) > 1 {
		err = errors.New("more than 1 type in slice")
		return
	}

	return
}
