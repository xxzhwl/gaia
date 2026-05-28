package gaia

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func makeCacheTestKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func waitForCachePropagation() {
	time.Sleep(50 * time.Millisecond)
}

func TestBasicSetGet(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("test-key")
	value := "test_value"

	cache.Set(key, value, 10*time.Second)

	result := cache.Get(key)
	if result == nil {
		t.Fatal("expected to get value, but got nil")
	}
	if result.(string) != value {
		t.Fatalf("expected %s, but got %s", value, result.(string))
	}
}

func TestStringSetGet(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("string-key")
	value := "string_value"

	cache.SetString(key, value, 10*time.Second)

	result := cache.GetString(key)
	if result != value {
		t.Fatalf("expected %s, but got %s", value, result)
	}
}

func TestIntSetGet(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("int-key")
	value := int64(123)

	cache.SetInt(key, value, 10*time.Second)

	result := cache.GetInt(key)
	if result != value {
		t.Fatalf("expected %d, but got %d", value, result)
	}
}

func TestMapSetGet(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("map-key")
	value := map[string]string{"key1": "value1", "key2": "value2"}

	cache.SetMap(key, value, 10*time.Second)

	result := cache.GetMap(key)
	if result == nil {
		t.Fatal("expected to get map, but got nil")
	}
	if result["key1"] != value["key1"] || result["key2"] != value["key2"] {
		t.Fatalf("expected %v, but got %v", value, result)
	}
}

func TestMapInterfaceSetGet(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("map-interface-key")
	value := map[string]interface{}{
		"string_key": "string_value",
		"int_key":    123,
		"bool_key":   true,
	}

	cache.SetMapInterface(key, value, 10*time.Second)

	result := cache.GetMapInterface(key)
	if result == nil {
		t.Fatal("expected to get map interface, but got nil")
	}
	if result["string_key"] != value["string_key"] || result["int_key"] != value["int_key"] || result["bool_key"] != value["bool_key"] {
		t.Fatalf("expected %v, but got %v", value, result)
	}
}

func TestStringListSetGet(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("string-list-key")
	value := []string{"value1", "value2", "value3"}

	cache.SetStringList(key, value, 10*time.Second)

	result := cache.GetStringList(key)
	if result == nil {
		t.Fatal("expected to get string list, but got nil")
	}
	if len(result) != len(value) {
		t.Fatalf("expected length %d, but got %d", len(value), len(result))
	}
	for i, v := range value {
		if result[i] != v {
			t.Fatalf("expected %s at index %d, but got %s", v, i, result[i])
		}
	}
}

func TestCacheExpiration(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("expire-key")
	value := "expire_value"

	cache.Set(key, value, 100*time.Millisecond)

	result := cache.Get(key)
	if result == nil {
		t.Fatal("expected to get value before expiration, but got nil")
	}

	time.Sleep(200 * time.Millisecond)

	result = cache.Get(key)
	if result != nil {
		t.Fatal("expected to get nil after expiration, but got value")
	}
}

func TestCacheEvictionHonorsConfiguredMaxSize(t *testing.T) {
	cache := NewCache()
	keys := []string{
		makeCacheTestKey("eviction-1"),
		makeCacheTestKey("eviction-2"),
		makeCacheTestKey("eviction-3"),
	}

	SetMaxCacheSize(2)
	t.Cleanup(func() {
		SetMaxCacheSize(10000)
		for _, key := range keys {
			cache.Delete(key)
		}
	})

	cache.Set(keys[0], "value1", 10*time.Second)
	cache.Set(keys[1], "value2", 10*time.Second)
	cache.Set(keys[2], "value3", 10*time.Second)
	waitForCachePropagation()

	hits := 0
	for _, key := range keys {
		if cache.Get(key) != nil {
			hits++
		}
	}

	if hits > 2 {
		t.Fatalf("expected at most 2 cached values after eviction, but got %d", hits)
	}
}

func TestDelete(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("delete-key")

	cache.Set(key, "delete_value", 10*time.Second)
	cache.Delete(key)

	result := cache.Get(key)
	if result != nil {
		t.Fatal("expected to get nil after delete, but got value")
	}
}

func TestCacheLoad(t *testing.T) {
	key := makeCacheTestKey("load-key")
	value := "load_value"
	callCount := 0

	fn := func() (string, error) {
		callCount++
		return value, nil
	}

	result, err := CacheLoad(key, 10*time.Second, fn)
	if err != nil {
		t.Fatalf("expected no error, but got %v", err)
	}
	if result != value {
		t.Fatalf("expected %s, but got %s", value, result)
	}
	if callCount != 1 {
		t.Fatalf("expected fn to be called once, but got %d times", callCount)
	}

	result, err = CacheLoad(key, 10*time.Second, fn)
	if err != nil {
		t.Fatalf("expected no error, but got %v", err)
	}
	if result != value {
		t.Fatalf("expected %s, but got %s", value, result)
	}
	if callCount != 1 {
		t.Fatalf("expected fn to be called once, but got %d times", callCount)
	}
}

func TestCacheGetNonExistent(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("non-existent-key")

	result := cache.Get(key)
	if result != nil {
		t.Fatal("expected to get nil for non-existent key, but got value")
	}

	resultStr := cache.GetString(key)
	if resultStr != "" {
		t.Fatalf("expected empty string for non-existent key, but got %s", resultStr)
	}

	resultInt := cache.GetInt(key)
	if resultInt != 0 {
		t.Fatalf("expected 0 for non-existent key, but got %d", resultInt)
	}
}

func TestCacheLoadWithError(t *testing.T) {
	key := makeCacheTestKey("load-error-key")
	expectedErr := "test error"

	fn := func() (string, error) {
		return "", errors.New(expectedErr)
	}

	result, err := CacheLoad(key, 10*time.Second, fn)
	if err == nil {
		t.Fatal("expected error, but got nil")
	}
	if err.Error() != expectedErr {
		t.Fatalf("expected error %s, but got %v", expectedErr, err)
	}
	if result != "" {
		t.Fatalf("expected empty string, but got %s", result)
	}
}

func TestCacheSetIsImmediatelyVisible(t *testing.T) {
	cache := NewCache()
	key := makeCacheTestKey("set-visible-key")
	value := "visible"

	cache.Set(key, value, 10*time.Second)

	result := cache.Get(key)
	if result == nil || result.(string) != value {
		t.Fatalf("expected immediate read-after-write visibility, got %#v", result)
	}
}

func TestSetMaxCacheSizeTakesEffectForExistingCache(t *testing.T) {
	cache := NewCache()
	keys := []string{
		makeCacheTestKey("resize-1"),
		makeCacheTestKey("resize-2"),
		makeCacheTestKey("resize-3"),
	}

	t.Cleanup(func() {
		SetMaxCacheSize(10000)
		for _, key := range keys {
			cache.Delete(key)
		}
	})

	cache.Set(keys[0], "value1", 10*time.Second)
	cache.Set(keys[1], "value2", 10*time.Second)
	SetMaxCacheSize(1)
	cache.Set(keys[2], "value3", 10*time.Second)
	waitForCachePropagation()

	hits := 0
	for _, key := range keys {
		if cache.Get(key) != nil {
			hits++
		}
	}

	if hits > 1 {
		t.Fatalf("expected resized cache to keep at most 1 key, but got %d", hits)
	}
}
