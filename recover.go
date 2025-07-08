package gaia

/*
错误拦截处理逻辑
@Author wanlizhan
@Date 2023-03-23
*/

import (
	"fmt"
)

// CatchPanic 捕获并处理低级别的panic，此类panic通常是因为出现不可预料的异常导致
// 特别是在API服务中，如果代码不够健壮，导致panic，又没有recover捕获，则整个服务进程将崩溃
// 特别注意: panic不能跨goroutine捕获，因此，所创建的每个goroutine，都需要进行异常捕获。
// 该方法处理外层recover传入的参数，并进行告警和运行时错误日志记录，方便事后排查问题
// 函数 CatchPanic在上层逻辑必须以 defer CatchPanic()的形式调用，不可在闭包内调用
func CatchPanic() {
	if r := recover(); r != nil {
		PanicLog(r)
	}
}

// PanicLog 记录Panic异常日志，并返回panic异常信息
func PanicLog(r interface{}) string {
	if r == nil {
		return ""
	}
	errmsg := fmt.Sprintf("encounter panic: %v\n", r)

	//获取运行时调用栈信息，以便进一步的问题排查
	stack := GetStackFramesString(2, 0)
	content := errmsg + stack

	//文本日志
	Log(LogErrorLevel, content)

	//发送告警提示
	if err := SendPanicAlarm("RuntimePanic", content); err != nil {
		Log(LogErrorLevel, err.Error())
	}
	return errmsg
}
