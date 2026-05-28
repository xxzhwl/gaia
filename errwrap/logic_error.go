/*
Package errwrap 对错误信息进行二次封装，加入错误码标识和状态码前缀，便于API的返回
@author wanlizhan
@created 2024-05-16
*/
package errwrap

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// LogicError 逻辑层错误接口定义
type LogicError interface {
	Error() string
	GetCode() int64
	GetMessage() string
	GetPrefix() string
	GetStack() string
	SetPrefix(prefix string)
}

// 自定义逻辑错误类型
type fundamental struct {
	prefix  string //错误前缀说明，经常用于对某一类错误的标识，比如某一个特定的API接口服务名称
	code    int64  //错误码，默认为 EcDefaultErr
	message string //错误消息
	stack   string //调用栈
}

// New 实例化一个 LogicError 错误
func New(prefix string, code int64, message string) LogicError {
	if code == 0 {
		code = EcDefaultErr
	}
	return &fundamental{
		prefix:  prefix,
		code:    code,
		message: message,
		stack:   string(debug.Stack()),
	}
}

// Newf 实例化一个 LogicError 错误，支持错误消息的格式化
func Newf(prefix string, code int64, msgf string, args ...any) LogicError {
	message := fmt.Sprintf(msgf, args...)
	return New(prefix, code, message)
}

// Errorf 返回带有错误码的错误信息，格式类似 [101]ErrMessage
func Errorf(code int64, format string, args ...any) LogicError {
	msg := fmt.Sprintf(format, args...)
	return New("", code, msg)
}

// Error 实例错误类型接口
// 详细的错误信息，如果code/prefix都存在值的情况下，格式如 [code][prefix]message
func (o *fundamental) Error() string {
	strlist := make([]string, 0, 3)
	strlist = append(strlist, fmt.Sprintf("[%d]", o.code))
	if len(o.prefix) > 0 {
		strlist = append(strlist, "["+o.prefix+"]")
	}
	strlist = append(strlist, o.message)
	return strings.Join(strlist, "")
}

// GetCode 获取错误码
func (o *fundamental) GetCode() int64 {
	return o.code
}

// GetMessage 获取错误消息
func (o *fundamental) GetMessage() string {
	return o.message
}

// GetPrefix 获取错误前缀分类信息
func (o *fundamental) GetPrefix() string {
	return o.prefix
}

// GetStack 获取错误发生时的调用栈信息
func (o *fundamental) GetStack() string {
	return o.stack
}

// SetPrefix 设置错误前缀，仅当当前错误前缀不存在时生效
func (o *fundamental) SetPrefix(prefix string) {
	if len(o.prefix) == 0 && len(prefix) > 0 {
		o.prefix = prefix
	}
}
