// Package gaia 运行时相关逻辑封装
// @author: wanlizhan
// @created 2023-07-07
package gaia

import (
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// MemoryGetUsage 获取memory使用量，单位byte
func MemoryGetUsage() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc
}

// GetGCStats 获取当前的GC状态
func GetGCStats() debug.GCStats {
	var stats debug.GCStats
	debug.ReadGCStats(&stats)
	return stats
}

// SetCPUNum 设置可使用的CPU核数，默认只使用一个CPU核心
func SetCPUNum() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

// GetGoRoutineId 获取当前协程所在的ID标识
func GetGoRoutineId() string {
	var buf [32]byte
	n := runtime.Stack(buf[:], false)
	arr := strings.Fields(string(buf[:n]))
	goid := arr[1]
	return goid
}

// GetStackFrames 获取当前函数的调用栈，返回原始的[]runtime.Frame
// skip为2为跳过本函数与Callers
// maxSize 最大返回的调用栈行数
func GetStackFrames(skip int, maxSize int) []runtime.Frame {
	skip += 2
	// 最大栈深度默认为16
	if maxSize == 0 {
		maxSize = 16
	}

	pc := make([]uintptr, maxSize)
	n := runtime.Callers(skip, pc)
	if n == 0 {
		return []runtime.Frame{}
	}
	pc = pc[:n]
	frames := runtime.CallersFrames(pc)
	ret := make([]runtime.Frame, 0)
	for {
		frame, more := frames.Next()
		ret = append(ret, frame)
		if !more {
			break
		}
	}
	return ret
}

// GetStackFramesString 获取当前函数的调用栈，返回文本信息
// skip为2为跳过本函数与Callers
// maxSize 最大返回的调用栈行数
func GetStackFramesString(skip int, maxSize int) string {
	retval := ""
	frames := GetStackFrames(skip, maxSize)
	if len(frames) > 0 {
		for _, frame := range frames {
			retval += frame.Function + "\n"
			retval += "    " + frame.File + "  " + strconv.Itoa(frame.Line) + "\n"
		}
	}
	return retval
}

// GetCurrentFuncName 获取当前函数名称
func GetCurrentFuncName() string {
	frames := GetStackFrames(1, 1)
	if len(frames) > 0 {
		list := StringToListWithDelimit(frames[0].Function, "/")
		if len(list) > 0 {
			return list[len(list)-1]
		}
	}
	return ""
}
