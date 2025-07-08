// Package gaia 注释
// @author wanlizhan
// @created 2024/4/28
package gaia

import "cmp"

func DefaultValue[T comparable](arg, defaultValue T) T {
	return cmp.Or(arg, defaultValue)
}
