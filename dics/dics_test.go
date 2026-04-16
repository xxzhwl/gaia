// Package dics comment_from_here
// @author wanlizhan
// @created 2023-04-21
package dics

import "testing"

func TestMerge(t *testing.T) {
	type args struct {
		m1 map[string]string
		m2 map[string]string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "test-1",
			args: args{
				m1: map[string]string{"a": "1234", "m1": "11"},
				m2: map[string]string{
					"a": "aa",
					"b": "bb",
					"c": "cc",
				},
			},
		},
		{
			name: "test-2",
			args: args{
				m1: nil,
				m2: map[string]string{
					"a": "aa",
					"b": "bb",
					"c": "cc",
				},
			},
		},
		{
			name: "test-3",
			args: args{
				m1: map[string]string{
					"a": "aa",
					"b": "bb",
					"c": "cc",
				},
				m2: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Merge(tt.args.m1, tt.args.m2)

			t.Log("result -->", got)
		})
	}
}
