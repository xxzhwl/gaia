// Package gaia 注释
// @author wanlizhan
// @created 2024/4/27
package gaia

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// GetCallbackFunc 从object(支持值和指针类型)中动态获取一个可执行的方法，会尝试获取该对象下的指针方法和普通值方法
//
// 该方法可被断言成匿名函数类型，如 fm.Interface().(func(string) (interface{}, error))
// 但不能被断言成具名函数类型
//
// 例子：
//
//	type A struct {}
//	func (a *A) HelloPtr() {}
//	func (a A) HelloVal() {}
//
// 下面列举的4种调用方式都能得到需要的方法
//
//	GetCallbackFunc(&A{}, "HelloVal")
//	GetCallbackFunc(&A{}, "HelloPtr")
//	GetCallbackFunc(A{}, "HelloVal")
//	GetCallbackFunc(A{}, "HelloPtr")
//
// NOTE: 如果传入的对象是指针类型，则直接使用，如果是结构体类型，则使用New实例化后使用
// 1. 如果在被调起的函数的实现过程中需要共享属性值，则注册时传入结构体类型，避免不同的逻辑之间属性值相互干扰
// 2. 如果在被调起的函数的实现过程中不需要共享属性值，则注册时传入指针类型，提升结构体的复用性
func GetCallbackFunc(object any, method string, errmsg string) (fm reflect.Value, err error) {
	fv := reflect.ValueOf(object)
	var ptr, value reflect.Value
	if fv.Kind() == reflect.Ptr {
		ptr = fv
		value = ptr.Elem()
	} else {
		ptr = reflect.New(fv.Type())
		value = fv
	}

	method = Title(method)
	fm = ptr.MethodByName(method)
	if !fm.IsValid() {
		fm = value.MethodByName(method)
		if !fm.IsValid() {
			err = errors.New(errmsg)
			return
		}
	}

	return
}

// CallMethodWithOneArgBytes 用于调用的方法有多个参数的情况
func CallMethodWithOneArgBytes(service any, methodName string, args []byte) (any, error) {
	if len(args) == 0 {
		return CallMethodWithArgs(service, methodName)
	}

	var arg any
	if err := json.Unmarshal(args, &arg); err != nil {
		return nil, err
	}

	return CallMethodWithArgs(service, methodName, arg)
}

// CallMethodWithArgs 用于调用的方法有多个参数的情况
func CallMethodWithArgs(service any, methodName string, args ...any) (any, error) {
	if service == nil {
		return nil, fmt.Errorf("service is nil")
	}

	fm, err := GetCallbackFunc(service, methodName, fmt.Sprintf("[%s]-[%s] is not found", reflect.ValueOf(service).Type(),
		methodName))
	if err != nil {
		return nil, err
	}

	methodArgLenth := fm.Type().NumIn()
	if methodArgLenth != len(args) && methodArgLenth != 0 {
		return nil, fmt.Errorf("the length of method's args does not match[%d-%d]", methodArgLenth, len(args))
	}
	if methodArgLenth == 0 {
		return call(methodName, fm, nil)
	} else {
		values := make([]reflect.Value, len(args))
		for i, arg := range args {
			pType := fm.Type().In(i)
			stctPointer := reflect.New(pType).Interface()

			var bytesData []byte
			if v, ok := arg.([]byte); ok {
				bytesData = v
			} else {
				marshal, err := json.Marshal(arg)
				if err != nil {
					return nil, fmt.Errorf("user input parameter error, can not be parsed into struct, detail: %s",
						err.Error())
				}
				bytesData = marshal
			}

			if err := json.Unmarshal(bytesData, stctPointer); err != nil {
				return nil, fmt.Errorf("user input parameter error, can not be parsed into struct, detail: %s",
					err.Error())
			}
			if reflect.TypeOf(stctPointer).Elem().Kind() == reflect.Struct {
				if err := NewDataChecker().CheckStructDataValid(stctPointer); err != nil {
					return nil, fmt.Errorf("user input parameter error, can not be parsed into struct, detail: %s",
						err.Error())
				}
			}

			values[i] = reflect.ValueOf(stctPointer).Elem()
		}
		return call(methodName, fm, values)
	}
}

// CallMethodWithJSONArgs 从 JSON 字节数组解析参数并调用方法
// 支持两种格式：
// 1. JSON 数组：["arg1", "arg2", 123] - 会被展开为多个参数
// 2. 空数组：[] - 无参数调用
func CallMethodWithJSONArgs(service any, methodName string, jsonArgs []byte) (any, error) {
	if service == nil {
		return nil, fmt.Errorf("service is nil")
	}

	fm, err := GetCallbackFunc(service, methodName, fmt.Sprintf("[%s]-[%s] is not found", reflect.ValueOf(service).Type(),
		methodName))
	if err != nil {
		return nil, err
	}

	methodArgLength := fm.Type().NumIn()

	if len(jsonArgs) == 0 || string(jsonArgs) == "[]" || string(jsonArgs) == "{}" {
		if methodArgLength != 0 {
			return nil, fmt.Errorf("the length of method's args does not match[%d-%d]", methodArgLength, 0)
		}
		return call(methodName, fm, nil)
	}

	var rawArgs any
	if err := json.Unmarshal(jsonArgs, &rawArgs); err != nil {
		return nil, fmt.Errorf("json unmarshal args error: %s", err.Error())
	}

	var args []any
	switch v := rawArgs.(type) {
	case []interface{}:
		args = v
	case map[string]interface{}:
		args = []any{v}
	default:
		args = []any{v}
	}

	if methodArgLength != len(args) {
		return nil, fmt.Errorf("the length of method's args does not match[%d-%d]", methodArgLength, len(args))
	}

	values := make([]reflect.Value, len(args))
	for i, arg := range args {
		pType := fm.Type().In(i)
		stctPointer := reflect.New(pType).Interface()

		var bytesData []byte
		var err error
		bytesData, err = json.Marshal(arg)
		if err != nil {
			return nil, fmt.Errorf("marshal arg error: %s", err.Error())
		}

		if err := json.Unmarshal(bytesData, stctPointer); err != nil {
			return nil, fmt.Errorf("unmarshal arg to type %s error: %s", pType.String(), err.Error())
		}

		if reflect.TypeOf(stctPointer).Elem().Kind() == reflect.Struct {
			if err := NewDataChecker().CheckStructDataValid(stctPointer); err != nil {
				return nil, fmt.Errorf("validate arg error: %s", err.Error())
			}
		}

		values[i] = reflect.ValueOf(stctPointer).Elem()
	}

	return call(methodName, fm, values)
}

// 回调结果处理
// 允许的情况：1.无返回值 2.一个结果(可以是error，也可以是普通结果) 3.一个结果，一个error
func getResult(methodName string, outFv []reflect.Value) (interface{}, error) {
	outNum := len(outFv)
	if outNum == 0 {
		return nil, nil
	}
	if outNum > 2 {
		return nil, fmt.Errorf("%s return value parameter is not as expected, only 1 or 2 "+
			"return value parameters are allowed, but got %d", methodName, outNum)
	}

	errVal := outFv[outNum-1].Interface()
	var retErr error
	if errVal != nil {
		if err, ok := errVal.(error); ok {
			retErr = err
		}
	}

	if outNum == 1 {
		if retErr != nil {
			return nil, retErr
		} else {
			return outFv[outNum-1].Interface(), nil
		}
	}

	return outFv[0].Interface(), retErr
}

func call(methodName string, value reflect.Value, args []reflect.Value) (any, error) {
	res := value.Call(args)
	return getResult(methodName, res)
}
