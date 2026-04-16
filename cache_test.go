// Package gaia
// Author wanlizhan
// Create 2023-03-29
package gaia

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestBasicSetGet 测试基本的 Set 和 Get 功能
func TestBasicSetGet(t *testing.T) {
	cache := NewCache()
	key := "test_key"
	value := "test_value"

	// 设置缓存
	cache.Set(key, value, time.Second*10)

	// 获取缓存
	result := cache.Get(key)
	if result == nil {
		t.Error("Expected to get value, but got nil")
	}
	fmt.Println(result)
	if result.(string) != value {
		t.Errorf("Expected %s, but got %s", value, result.(string))
	}
}

// TestStringSetGet 测试字符串类型的 Set 和 Get 功能
func TestStringSetGet(t *testing.T) {
	cache := NewCache()
	key := "string_key"
	value := "string_value"

	// 设置字符串缓存
	cache.SetString(key, value, time.Second*10)

	// 获取字符串缓存
	result := cache.GetString(key)
	fmt.Println(result)
	if result != value {
		t.Errorf("Expected %s, but got %s", value, result)
	}
}

// TestIntSetGet 测试整数类型的 Set 和 Get 功能
func TestIntSetGet(t *testing.T) {
	cache := NewCache()
	key := "int_key"
	value := int64(123)

	// 设置整数缓存
	cache.SetInt(key, value, time.Second*10)

	// 获取整数缓存
	result := cache.GetInt(key)
	fmt.Println(result)
	if result != value {
		t.Errorf("Expected %d, but got %d", value, result)
	}

}

// TestMapSetGet 测试 Map 类型的 Set 和 Get 功能
func TestMapSetGet(t *testing.T) {
	cache := NewCache()
	key := "map_key"
	value := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	// 设置 Map 缓存
	cache.SetMap(key, value, time.Second*10)

	// 获取 Map 缓存
	result := cache.GetMap(key)
	fmt.Println(result)
	if result == nil {
		t.Error("Expected to get map, but got nil")
		return
	}

	if result["key1"] != value["key1"] || result["key2"] != value["key2"] {
		t.Errorf("Expected %v, but got %v", value, result)
	}
}

// TestMapInterfaceSetGet 测试 MapInterface 类型的 Set 和 Get 功能
func TestMapInterfaceSetGet(t *testing.T) {
	cache := NewCache()
	key := "map_interface_key"
	value := map[string]interface{}{
		"string_key": "string_value",
		"int_key":    123,
		"bool_key":   true,
	}

	// 设置 MapInterface 缓存
	cache.SetMapInterface(key, value, time.Second*10)

	// 获取 MapInterface 缓存
	result := cache.GetMapInterface(key)
	fmt.Println(result)
	if result == nil {
		t.Error("Expected to get map interface, but got nil")
		return
	}

	if result["string_key"] != value["string_key"] || result["int_key"] != value["int_key"] || result["bool_key"] != value["bool_key"] {
		t.Errorf("Expected %v, but got %v", value, result)
	}
}

// TestStringListSetGet 测试 StringList 类型的 Set 和 Get 功能
func TestStringListSetGet(t *testing.T) {
	cache := NewCache()
	key := "string_list_key"
	value := []string{"value1", "value2", "value3"}

	// 设置 StringList 缓存
	cache.SetStringList(key, value, time.Second*10)

	// 获取 StringList 缓存
	result := cache.GetStringList(key)
	fmt.Println(result)
	if result == nil {
		t.Error("Expected to get string list, but got nil")
		return
	}

	if len(result) != len(value) {
		t.Errorf("Expected length %d, but got %d", len(value), len(result))
		return
	}

	for i, v := range value {
		if result[i] != v {
			t.Errorf("Expected %s at index %d, but got %s", v, i, result[i])
		}
	}
}

// TestCacheExpiration 测试缓存过期功能
func TestCacheExpiration(t *testing.T) {
	cache := NewCache()
	key := "expire_key"
	value := "expire_value"

	// 设置一个短时间过期的缓存
	cache.Set(key, value, time.Millisecond*100)

	// 立即获取，应该能获取到
	result := cache.Get(key)
	if result == nil {
		t.Error("Expected to get value before expiration, but got nil")
	}

	// 等待过期
	time.Sleep(time.Millisecond * 200)

	// 再次获取，应该获取不到
	result = cache.Get(key)
	if result != nil {
		t.Error("Expected to get nil after expiration, but got value")
	}
}

// TestCacheEviction 测试缓存淘汰功能
func TestCacheEviction(t *testing.T) {
	// 清空缓存，确保测试隔离
	mutexSysGlobalCacheStack.Lock()
	sysGlobalCacheStack = make(map[string]cacheNode)
	mutexSysGlobalCacheStack.Unlock()

	// 设置一个小的最大缓存大小
	SetMaxCacheSize(2)

	cache := NewCache()

	// 设置三个缓存项，第一个应该会触发淘汰
	cache.Set("key1", "value1", time.Second*10)
	cache.Set("key2", "value2", time.Second*10)
	cache.Set("key3", "value3", time.Second*10)

	// 检查缓存项是否正确
	result := cache.Get("key1")
	fmt.Println(result)

	result = cache.Get("key2")
	fmt.Println(result)

	result = cache.Get("key3")
	fmt.Println(result)

	// 检查缓存大小，应该为 2
	mutexSysGlobalCacheStack.RLock()
	if len(sysGlobalCacheStack) != 2 {
		t.Errorf("Expected cache size 2, but got %d", len(sysGlobalCacheStack))
	}
	mutexSysGlobalCacheStack.RUnlock()

	// 恢复默认最大缓存大小
	SetMaxCacheSize(10000)

	// 清空缓存，确保不影响其他测试
	mutexSysGlobalCacheStack.Lock()
	sysGlobalCacheStack = make(map[string]cacheNode)
	mutexSysGlobalCacheStack.Unlock()
}

// TestDelete 测试 Delete 功能
func TestDelete(t *testing.T) {
	cache := NewCache()
	key := "delete_key"
	value := "delete_value"

	// 设置缓存
	cache.Set(key, value, time.Second*10)

	// 删除缓存
	cache.Delete(key)

	// 获取缓存，应该获取不到
	result := cache.Get(key)
	if result != nil {
		t.Error("Expected to get nil after delete, but got value")
	}
}

// TestCacheLoad 测试 CacheLoad 功能
func TestCacheLoad(t *testing.T) {
	key := "load_key"
	value := "load_value"
	callCount := 0

	// 定义数据构建函数
	fn := func() (string, error) {
		callCount++
		return value, nil
	}

	// 第一次调用，应该构建数据
	result, err := CacheLoad(key, time.Second*10, fn)
	if err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}

	if result != value {
		t.Errorf("Expected %s, but got %s", value, result)
	}

	if callCount != 1 {
		t.Errorf("Expected fn to be called once, but got %d times", callCount)
	}

	// 第二次调用，应该从缓存获取
	result, err = CacheLoad(key, time.Second*10, fn)
	if err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}

	if result != value {
		t.Errorf("Expected %s, but got %s", value, result)
	}

	if callCount != 1 {
		t.Errorf("Expected fn to be called once, but got %d times", callCount)
	}
}

// TestCloseClean 测试 CloseClean 功能
func TestCloseClean(t *testing.T) {
	// 调用 CloseClean 关闭清理 goroutine
	CloseClean()

	// 验证清理 goroutine 已关闭
	// 这里我们无法直接验证，但可以确保调用不会导致 panic
	cache := NewCache()
	cache.Set("test_key", "test_value", time.Second*10)
	result := cache.Get("test_key")
	if result == nil {
		t.Error("Expected to get value after CloseClean, but got nil")
	}
}

// TestCacheGetNonExistent 测试获取不存在的缓存
func TestCacheGetNonExistent(t *testing.T) {
	cache := NewCache()
	key := "non_existent_key"

	// 获取不存在的缓存，应该返回 nil
	result := cache.Get(key)
	if result != nil {
		t.Error("Expected to get nil for non-existent key, but got value")
	}

	// 测试 GetString
	resultStr := cache.GetString(key)
	if resultStr != "" {
		t.Errorf("Expected to get empty string for non-existent key, but got %s", resultStr)
	}

	// 测试 GetInt
	resultInt := cache.GetInt(key)
	if resultInt != 0 {
		t.Errorf("Expected to get 0 for non-existent key, but got %d", resultInt)
	}
}

// TestCacheLoadWithError 测试 CacheLoad 函数在数据构建函数返回错误时的行为
func TestCacheLoadWithError(t *testing.T) {
	key := "load_error_key"
	expectedErr := "test error"

	// 定义返回错误的数据构建函数
	fn := func() (string, error) {
		return "", errors.New(expectedErr)
	}

	// 调用 CacheLoad，应该返回错误
	result, err := CacheLoad(key, time.Second*10, fn)
	if err == nil {
		t.Error("Expected error, but got nil")
	}

	if err.Error() != expectedErr {
		t.Errorf("Expected error %s, but got %v", expectedErr, err)
	}

	if result != "" {
		t.Errorf("Expected empty string, but got %s", result)
	}
}
