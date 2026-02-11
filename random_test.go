package gaia

import (
	"testing"
)

// TestGetRandString tests the GetRandString function
func TestGetRandString(t *testing.T) {
	// Test with different lengths
	lengths := []int{0, 1, 5, 10, 20}
	for _, length := range lengths {
		result := GetRandString(length)
		if len(result) != length {
			t.Errorf("Expected string of length %d, got %d", length, len(result))
		}
		// Check that all characters are valid
		for _, c := range result {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				t.Errorf("Invalid character %c in result %s", c, result)
			}
		}
	}
}

// TestGetRandChar tests the GetRandChar function
func TestGetRandChar(t *testing.T) {
	for i := 0; i < 100; i++ {
		result := GetRandChar()
		if len(result) != 1 {
			t.Errorf("Expected single character, got %s", result)
		}
		c := rune(result[0])
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Errorf("Invalid character %c", c)
		}
	}
}

// TestGetRandHexSecretKey tests the GetRandHexSecretKey function
func TestGetRandHexSecretKey(t *testing.T) {
	// Test with different lengths
	lengths := []int{0, 1, 5, 10, 20}
	for _, length := range lengths {
		result := GetRandHexSecretKey(length)
		if len(result) != length {
			t.Errorf("Expected hex string of length %d, got %d", length, len(result))
		}
		// Check that all characters are valid hex
		for _, c := range result {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("Invalid hex character %c in result %s", c, result)
			}
		}
	}
}

// TestGetRandHexChar tests the GetRandHexChar function
func TestGetRandHexChar(t *testing.T) {
	for i := 0; i < 100; i++ {
		result := GetRandHexChar()
		if len(result) != 1 {
			t.Errorf("Expected single hex character, got %s", result)
		}
		c := rune(result[0])
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Invalid hex character %c", c)
		}
	}
}

// TestRand tests the Rand function
func TestRand(t *testing.T) {
	// Test with different ranges
	testCases := []struct {
		min, max int
	}{
		{0, 0},
		{0, 1},
		{0, 10},
		{5, 10},
		{10, 100},
		{-10, 10},
	}

	for _, tc := range testCases {
		for i := 0; i < 100; i++ {
			result := Rand(tc.min, tc.max)
			if result < tc.min || result > tc.max {
				t.Errorf("Expected result between %d and %d, got %d", tc.min, tc.max, result)
			}
		}
	}
}

// TestGetRandNum tests the GetRandNum function
func TestGetRandNum(t *testing.T) {
	for i := 0; i < 100; i++ {
		result := GetRandNum()
		if len(result) != 1 {
			t.Errorf("Expected single digit, got %s", result)
		}
		c := rune(result[0])
		if c < '0' || c > '9' {
			t.Errorf("Invalid digit %c", c)
		}
	}
}

// TestGetRandStringWithScope tests the GetRandStringWithScope function
func TestGetRandStringWithScope(t *testing.T) {
	// Test with custom character set
	customScope := "abc123"
	lengths := []int{0, 1, 5, 10}
	for _, length := range lengths {
		result := GetRandStringWithScope(length, customScope)
		if len(result) != length {
			t.Errorf("Expected string of length %d, got %d", length, len(result))
		}
		// Check that all characters are from custom scope
		for _, c := range result {
			found := false
			for _, scopeChar := range customScope {
				if c == scopeChar {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Character %c not in custom scope %s", c, customScope)
			}
		}
	}
}
