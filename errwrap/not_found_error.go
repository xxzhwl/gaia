/*
Package errwrap 对错误信息进行二次封装，加入错误码标识和状态码前缀，便于API的返回
@author wanlizhan
@created 2024-05-16
*/
package errwrap

import "fmt"

// NotFoundError 数据/资源不存在错误类型
type NotFoundError struct {
	code int64
	err  error
}

// NewNotFoundError 实例化不存在错误类型
func NewNotFoundError(errf string, args ...any) *NotFoundError {
	return &NotFoundError{
		code: 4004,
		err:  fmt.Errorf("[4004]"+errf, args...),
	}
}

// Unwrap 解出原始错误类型
func (o *NotFoundError) Unwrap() error {
	return o.err
}

// GetCode 返回错误码
func (o *NotFoundError) GetCode() int64 {
	return o.code
}

// Error 返回错误信息
func (o *NotFoundError) Error() string {
	if o.err == nil {
		return ""
	}
	return o.err.Error()
}

// IsNotFoundError 判断某一个错误类型是否为NotFoundError
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*NotFoundError); ok {
		return true
	} else {
		return false
	}
}
