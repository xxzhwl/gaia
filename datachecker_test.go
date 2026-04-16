// Package gaia 包注释
// @author wanlizhan
// @created 2024/5/30
package gaia

import (
	"testing"
)

func TestChecker_Date(t *testing.T) {
	type args struct {
		value any
		label string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"test1", args{"2018-09-09T00:00:00Z", "date1"}, false},
		{"test2", args{"2018-09-09", "date2"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewDataChecker()
			if err := c.Date(tt.args.value, tt.args.label); (err != nil) != tt.wantErr {
				t.Errorf("Date() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChecker_DateWithSplit(t *testing.T) {
	type args struct {
		value any
		label string
		split string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"test1", args{"2018-09-09", "date1", "-"}, false},
		{"test1", args{"2018/09/09", "date2", "/"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewDataChecker()
			if err := c.DateWithSplit(tt.args.value, tt.args.label, tt.args.split); (err != nil) != tt.wantErr {
				t.Errorf("DateWithSplit() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type Demo struct {
	Age int64 `gte:"0" lte:"200"`
}

func TestChecker_Struct(t *testing.T) {
	demo := Demo{Age: 1}

	err := NewDataChecker().CheckStructDataValid(demo)
	if err != nil {
		t.Fatal(err)
	}
}
