package gaia

import (
	"reflect"
	"testing"
)

// TestGetSliceInnerType tests the GetSliceInnerType function
func TestGetSliceInnerType(t *testing.T) {
	// Test case 1: nil slice
	t.Run("NilSlice", func(t *testing.T) {
		var nilSlice []int
		_, err := GetSliceInnerType(nilSlice)
		if err == nil {
			t.Error("Expected error for nil slice, got nil")
		}
	})

	// Test case 2: non-slice value
	t.Run("NonSliceValue", func(t *testing.T) {
		_, err := GetSliceInnerType("not a slice")
		if err == nil {
			t.Error("Expected error for non-slice value, got nil")
		}
	})

	// Test case 3: empty slice
	t.Run("EmptySlice", func(t *testing.T) {
		emptySlice := []int{}
		_, err := GetSliceInnerType(emptySlice)
		if err == nil {
			t.Error("Expected error for empty slice, got nil")
		}
	})

	// Test case 4: slice with single type (int)
	t.Run("SingleTypeIntSlice", func(t *testing.T) {
		intSlice := []int{1, 2, 3, 4, 5}
		kind, err := GetSliceInnerType(intSlice)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if kind != reflect.Int {
			t.Errorf("Expected Int kind, got %v", kind)
		}
	})

	// Test case 5: slice with single type (string)
	t.Run("SingleTypeStringSlice", func(t *testing.T) {
		stringSlice := []string{"a", "b", "c"}
		kind, err := GetSliceInnerType(stringSlice)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if kind != reflect.String {
			t.Errorf("Expected String kind, got %v", kind)
		}
	})

	// Test case 6: slice with single type (bool)
	t.Run("SingleTypeBoolSlice", func(t *testing.T) {
		boolSlice := []bool{true, false, true}
		kind, err := GetSliceInnerType(boolSlice)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if kind != reflect.Bool {
			t.Errorf("Expected Bool kind, got %v", kind)
		}
	})

	// Test case 7: slice with interface type (all same underlying type)
	t.Run("InterfaceSliceSameType", func(t *testing.T) {
		interfaceSlice := []any{1, 2, 3, 4, 5}
		kind, err := GetSliceInnerType(interfaceSlice)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if kind != reflect.Int {
			t.Errorf("Expected Int kind, got %v", kind)
		}
	})

	// Test case 8: slice with multiple types
	t.Run("MultipleTypesSlice", func(t *testing.T) {
		mixedSlice := []any{1, "string", true}
		_, err := GetSliceInnerType(mixedSlice)
		if err == nil {
			t.Error("Expected error for mixed type slice, got nil")
		}
	})

	// Test case 9: slice with float64 type
	t.Run("SingleTypeFloat64Slice", func(t *testing.T) {
		floatSlice := []float64{1.1, 2.2, 3.3}
		kind, err := GetSliceInnerType(floatSlice)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if kind != reflect.Float64 {
			t.Errorf("Expected Float64 kind, got %v", kind)
		}
	})

	// Test case 10: slice with complex64 type
	t.Run("SingleTypeComplex64Slice", func(t *testing.T) {
		complexSlice := []complex64{1 + 2i, 3 + 4i}
		kind, err := GetSliceInnerType(complexSlice)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if kind != reflect.Complex64 {
			t.Errorf("Expected Complex64 kind, got %v", kind)
		}
	})
}
