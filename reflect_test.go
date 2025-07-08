// Package gaia 反射调用方法
// @author wanlizhan
// @created 2024/7/9
package gaia

import (
	"errors"
	"fmt"
	"github.com/xxzhwl/gaia/cvt"
	"testing"
)

type testCase struct{}

func (t *testCase) Case1() {
	fmt.Println("Case1")
	return
}

type Case2Arg struct {
	Name string
	Age  int64
}

func (t *testCase) Case2(arg Case2Arg) {
	PrettyPrint(arg)
	return
}

func (t *testCase) Case3(arg Case2Arg) map[string]any {
	return cvt.FlattenStruct(arg)
}

func (t *testCase) Case4(arg Case2Arg) (map[string]any, error) {
	return cvt.FlattenStruct(arg), errors.New("这里是个错误")
}

func (t *testCase) Case5(arg1, arg2 Case2Arg) (map[string]any, error) {
	return map[string]any{"Arg1": arg1, "Arg2": arg2}, nil
}

func (t *testCase) Case6(arg1, arg2 Case2Arg) error {
	return errors.New("这是一个错误")
}

func TestCallMethodWithArgs(t *testing.T) {

	arg2 := Case2Arg{
		Name: "www",
		Age:  11,
	}
	arg51 := Case2Arg{
		Name: "ww",
		Age:  244,
	}

	type args struct {
		service    any
		methodName string
		args       []any
	}
	tests := []struct {
		name string
		args args
	}{
		{"test1-noArg", args{
			service:    &testCase{},
			methodName: "Case1",
			args:       nil,
		}},
		{"test2-oneArg", args{
			service:    &testCase{},
			methodName: "Case2",
			args:       []any{arg2},
		}},
		{"test3-oneArgWithReturn", args{
			service:    &testCase{},
			methodName: "Case3",
			args:       []any{arg2},
		}},
		{"test4-oneArgWithReturnAndErr", args{
			service:    &testCase{},
			methodName: "Case4",
			args:       []any{arg2},
		}},
		{"test5-oneArgWithManyArg", args{
			service:    &testCase{},
			methodName: "Case5",
			args:       []any{arg2, arg51},
		}},
		{"test6-oneArgWithErr", args{
			service:    &testCase{},
			methodName: "Case6",
			args:       []any{arg2, arg51},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CallMethodWithArgs(tt.args.service, tt.args.methodName, tt.args.args...)
			if err != nil {
				t.Fatal(err)
			}
			PrettyPrint(got)
		})
	}
}
