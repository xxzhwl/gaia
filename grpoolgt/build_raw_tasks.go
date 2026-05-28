// Package grpoolgt 构建原处理数据相关逻辑
// @author wanlizhan
// @created 2023-05-21
package grpoolgt

import (
	"strconv"
)

// BuildRawTaskList 构建原始任务列表，要求必须为一个slice数型
func BuildRawTaskList[I any](rawDatas []I) []RawTask[I] {
	llen := len(rawDatas)
	if llen == 0 {
		return make([]RawTask[I], 0)
	}
	rawTaskList := make([]RawTask[I], llen)
	for i := 0; i < llen; i++ {
		rawTaskList[i] = RawTask[I]{
			Id:        strconv.Itoa(i),
			InputData: rawDatas[i],
		}
	}
	return rawTaskList
}
