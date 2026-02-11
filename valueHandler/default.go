// Package valueHandler 包注释
// @author wanlizhan
// @created 2024/6/14
package valueHandler

import (
	"github.com/xxzhwl/gaia"
)

func init() {
	RegisterValueHandler("Time", &Time{})
	RegisterValueHandler("Date", &Date{})
}

type Time struct {
}

func (t Time) NewValue(value any) any {
	return gaia.Date(gaia.DateTimeFormat)
}

type Date struct {
}

func (d Date) NewValue(value any) any {
	return gaia.Date(gaia.DateFormat)
}
