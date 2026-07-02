// Package gaia 反射调用方法
// @author wanlizhan
// @created 2024/7/9
package gaia

import (
	"context"
	"errors"
	"fmt"
	"github.com/xxzhwl/gaia/cvt"
	"strings"
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

type jsonCallInput struct {
	OrderID string `json:"orderId"`
}

type jsonCallCase struct{}

func (t *jsonCallCase) WithContext(ctx context.Context, input jsonCallInput) (map[string]any, error) {
	return map[string]any{
		"orderId": input.OrderID,
		"traceId": ctx.Value("traceId"),
	}, nil
}

func (t *jsonCallCase) ObjectArg(input jsonCallInput) string {
	return input.OrderID
}

func (t *jsonCallCase) NoArg() string {
	return "ok"
}

func (t *jsonCallCase) Panic() {
	panic("boom")
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
		name    string
		args    args
		wantErr bool
	}{
		{"test1-noArg", args{
			service:    &testCase{},
			methodName: "Case1",
			args:       nil,
		}, false},
		{"test2-oneArg", args{
			service:    &testCase{},
			methodName: "Case2",
			args:       []any{arg2},
		}, false},
		{"test3-oneArgWithReturn", args{
			service:    &testCase{},
			methodName: "Case3",
			args:       []any{arg2},
		}, false},
		{"test4-oneArgWithReturnAndErr", args{
			service:    &testCase{},
			methodName: "Case4",
			args:       []any{arg2},
		}, true},
		{"test5-oneArgWithManyArg", args{
			service:    &testCase{},
			methodName: "Case5",
			args:       []any{arg2, arg51},
		}, false},
		{"test6-oneArgWithErr", args{
			service:    &testCase{},
			methodName: "Case6",
			args:       []any{arg2, arg51},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CallMethodWithArgs(tt.args.service, tt.args.methodName, tt.args.args...)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expect error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			PrettyPrint(got)
		})
	}
}

func TestCallMethodWithJSONArgsContextInjectsContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), "traceId", "trace-1")
	got, err := CallMethodWithJSONArgsContext(ctx, &jsonCallCase{}, "WithContext", []byte(`{"orderId":"O-1"}`))
	if err != nil {
		t.Fatalf("CallMethodWithJSONArgsContext() error = %v", err)
	}
	output := got.(map[string]any)
	if output["orderId"] != "O-1" || output["traceId"] != "trace-1" {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestCallMethodWithJSONArgsContextTreatsEmptyObjectAsArgument(t *testing.T) {
	got, err := CallMethodWithJSONArgsContext(context.Background(), &jsonCallCase{}, "ObjectArg", []byte(`{}`))
	if err != nil {
		t.Fatalf("CallMethodWithJSONArgsContext() error = %v", err)
	}
	if got != "" {
		t.Fatalf("expected zero-value object arg, got %#v", got)
	}

	got, err = CallMethodWithJSONArgs(&jsonCallCase{}, "NoArg", []byte(`{}`))
	if err != nil {
		t.Fatalf("CallMethodWithJSONArgs() should keep empty-object no-arg compatibility: %v", err)
	}
	if got != "ok" {
		t.Fatalf("unexpected legacy no-arg result: %#v", got)
	}

	got, err = CallMethodWithJSONArgsContext(context.Background(), &jsonCallCase{}, "NoArg", []byte(`{}`))
	if err != nil {
		t.Fatalf("CallMethodWithJSONArgsContext() should keep empty-object no-arg compatibility: %v", err)
	}
	if got != "ok" {
		t.Fatalf("unexpected context no-arg result: %#v", got)
	}
}

func TestCallMethodWithJSONArgsRejectsNilAndRecoversPanic(t *testing.T) {
	var nilService *jsonCallCase
	if _, err := CallMethodWithJSONArgs(nilService, "NoArg", nil); err == nil || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("expected typed nil service error, got %v", err)
	}

	if _, err := CallMethodWithJSONArgs(&jsonCallCase{}, "Panic", nil); err == nil || !strings.Contains(err.Error(), "panic: boom") {
		t.Fatalf("expected panic to be recovered as error, got %v", err)
	}
}
