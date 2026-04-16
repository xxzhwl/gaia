/*
Package errwrap 对错误信息进行二次封装，加入错误码标识和状态码前缀，便于API的返回
@author wanlizhan
@created 2024-05-05
*/
package errwrap

import (
	"regexp"
	"strconv"
	"strings"
)

/*
 * 定义API返回的错误码(ERROR CODE)规范
 * API框架逻辑会自动从错误信息中分析ERROR CODE(比如返回的error信息中有[477]errormsg..，则认为477即为状态码)
 * 如果有分析出有效的ERROR CODE，则使用错误信息中的ERROR CODE覆盖默认指定的
 * 4xx为客户端错误
 * 5xx为服务端错误
 * 10000+ 为具体逻辑定义错误，这里不做定义
 * 考虑到有些较为严重的错误会有特殊处理逻辑，这里给出一个范围指定这些错误
 */
const (
	EcDefaultErr int64 = 10000 //默认错误码
)

var codeReg *regexp.Regexp
var prefixReg *regexp.Regexp

func init() {
	codeReg = regexp.MustCompile(`^\[\d+?]`)
	prefixReg = regexp.MustCompile(`^\[.+?]`)
}

// Error 给传入的错误类型添加错误码
// code 如果之前的错误中已经存在错误码，则直接使用，不再被当前的错误码覆盖
// err 如果为 LogicError 对象，则直接返回此对象，不再进行任何处理
// 如果返回error，则将返回 LogicError 封装类型，但同时也兼容老的处理方式，使原有的用法不发生变化
func Error(code int64, err error) LogicError {
	if err == nil {
		return nil
	}

	if logicErr, ok := err.(LogicError); ok {
		return logicErr
	}

	prefix, existCode, message := _splitMessage(err.Error())

	//尝试移除error中的状态码
	if codeReg.MatchString(message) {
		message = codeReg.ReplaceAllString(message, "")
	}
	if existCode > EcDefaultErr {
		//如果之前的错误中已经存在错误码，则直接使用，不再被当前的错误码覆盖
		code = existCode
	}

	return New(prefix, code, message)
}

// PrefixError 返回 LogicError 并设置prefix标识
// prefix 如果之前的错误中已经存在prefix，则直接使用，不再被当前的prefix覆盖
// err 如果为 LogicError 对象，则直接返回此对象，不再进行任何处理
func PrefixError(prefix string, err error) LogicError {
	if err == nil {
		return nil
	}

	if logicErr, ok := err.(LogicError); ok {
		logicErr.SetPrefix(prefix)
		return logicErr
	}

	errmsg := err.Error()
	existPrefix, code, message := _splitMessage(errmsg)
	if len(existPrefix) > 0 {
		prefix = existPrefix
	}

	return New(prefix, code, message)
}

// GetCode 获取状态码
// errmsgOrLogicError 的类型可以是 string / error / LogicError
func GetCode(errmsgOrLogicError any) int64 {
	if errmsgOrLogicError == nil {
		//不存在错误
		return 0
	}
	if errmsg, ok := errmsgOrLogicError.(string); ok {
		//说明是一条文本错误消息
		_, code, _ := _splitMessage(errmsg)
		return code
	}
	if logicErr, ok := errmsgOrLogicError.(LogicError); ok {
		//说明是一个 LogicError 错误类型
		return logicErr.GetCode()
	}
	if notFoundErr, ok := errmsgOrLogicError.(NotFoundError); ok {
		//说明是一个 NotFoundErr 错误类型
		return notFoundErr.GetCode()
	}
	if notFoundErr, ok := errmsgOrLogicError.(*NotFoundError); ok {
		//说明是一个 NotFoundErr 错误类型
		return notFoundErr.GetCode()
	}
	if err, ok := errmsgOrLogicError.(error); ok {
		_, code, _ := _splitMessage(err.Error())
		return code
	}

	//未知的数据类型传入，返回默认值
	return EcDefaultErr
}

// GetStack 获取错误产生时的调用栈信息，所传入的err为 LogickError 对象时有效，否则返回空
func GetStack(err error) string {
	if err == nil {
		return ""
	}
	if logicErr, ok := err.(LogicError); ok {
		return logicErr.GetStack()
	}
	return ""
}

// 对一条错误消息，比如[500][lbsapi]error message，进行格式分离，如分离出 500, lbsapi, error message部分
func _splitMessage(verboseMessage string) (prefix string, code int64, message string) {
	if len(verboseMessage) == 0 {
		return
	}

	rawVerboseMessage := verboseMessage

	//处理状态码
	matched := codeReg.FindString(verboseMessage)
	strCode := strings.Trim(matched, "[]")
	if len(strCode) > 0 {
		if v, e := strconv.Atoi(strCode); e == nil {
			code = int64(v)
		}

		//移除状态码
		verboseMessage = codeReg.ReplaceAllString(verboseMessage, "")
	}

	//处理prefix
	matched = prefixReg.FindString(verboseMessage)
	prefix = strings.Trim(matched, "[]")
	if len(prefix) > 0 {
		verboseMessage = prefixReg.ReplaceAllString(verboseMessage, "")
	}

	//最后留下的就是干净的错误信息
	message = verboseMessage
	if len(message) == 0 {
		//如果最终错误信息已经为空，则使用原始的消息
		message = rawVerboseMessage
	}
	if code == 0 {
		//存在异常时, 又没有指定具体的状态码，则使用默认状态码 EcDefaultErr
		code = EcDefaultErr
	}
	return
}
