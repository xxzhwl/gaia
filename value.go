// Package gaia 注释
// @author wanlizhan
// @created 2024/4/28
package gaia

import "cmp"

// DefaultValue 返回第一个非零值参数，如果arg为零值则返回defaultValue
func DefaultValue[T comparable](arg, defaultValue T) T {
	return cmp.Or(arg, defaultValue)
}
