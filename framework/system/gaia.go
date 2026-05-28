// Package system 注释
// @author wanlizhan
// @created 2024/5/6
package system

import "github.com/xxzhwl/gaia"

type GaiaSystem struct{}

func (g GaiaSystem) GetSystemEnName() string {
	return gaia.GetSafeConfString("SystemEnName")
}

func (g GaiaSystem) GetSystemCnName() string {
	return gaia.GetSafeConfString("SystemCnName")
}
