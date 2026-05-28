// Package gaia
// Author wanlizhan
// Create 2023-03-29
package gaia

import (
	"fmt"
	"sync"
	"time"

	ristretto "github.com/dgraph-io/ristretto/v2"
)

const (
	defaultMaxCacheSize        = 10000
	cacheNumCountersMultiplier = 10
	cacheBufferItems           = 64
)

var (
	maxCacheSize = defaultMaxCacheSize

	globalCache   *ristretto.Cache[string, any]
	globalCacheMu sync.RWMutex
)

func init() {
	globalCache = mustNewRistrettoCache(maxCacheSize)
}

// Cache 逻辑体
type Cache struct{}

// NewCache 实例化
func NewCache() *Cache {
	return &Cache{}
}

func normalizedMaxCacheSize(size int) int {
	if size <= 0 {
		return defaultMaxCacheSize
	}
	return size
}

func newRistrettoCache(size int) (*ristretto.Cache[string, any], error) {
	normalizedSize := normalizedMaxCacheSize(size)
	return ristretto.NewCache(&ristretto.Config[string, any]{
		NumCounters:        int64(normalizedSize * cacheNumCountersMultiplier),
		MaxCost:            int64(normalizedSize),
		BufferItems:        cacheBufferItems,
		IgnoreInternalCost: true,
	})
}

func mustNewRistrettoCache(size int) *ristretto.Cache[string, any] {
	cache, err := newRistrettoCache(size)
	if err != nil {
		panic(fmt.Sprintf("gaia: init cache failed: %v", err))
	}
	return cache
}

func getGlobalCache() *ristretto.Cache[string, any] {
	globalCacheMu.RLock()
	cache := globalCache
	globalCacheMu.RUnlock()
	if cache != nil {
		return cache
	}

	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	if globalCache == nil {
		globalCache = mustNewRistrettoCache(maxCacheSize)
	}
	return globalCache
}

// SetMaxCacheSize 允许用户自定义最大缓存大小。
func SetMaxCacheSize(size int) {
	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()

	maxCacheSize = normalizedMaxCacheSize(size)
	if globalCache == nil {
		globalCache = mustNewRistrettoCache(maxCacheSize)
		return
	}
	globalCache.UpdateMaxCost(int64(maxCacheSize))
}

// Set 设置一个值到cache中
func (o *Cache) Set(key string, value any, expiration time.Duration) {
	cache := getGlobalCache()
	if expiration <= 0 {
		cache.Del(key)
		cache.Wait()
		return
	}

	cache.SetWithTTL(key, value, 1, expiration)
	cache.Wait()
}

// Get 从cache中获取一个值
func (o *Cache) Get(key string) any {
	cache := getGlobalCache()
	value, ok := cache.Get(key)
	if !ok {
		return nil
	}
	return value
}

// SetString 设置一个字符串到cache中
func (o *Cache) SetString(key, value string, expiration time.Duration) {
	o.Set(key, value, expiration)
}

// GetString 获取一个字符串值
func (o *Cache) GetString(key string) string {
	v := o.Get(key)
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%s", v)
}

// SetInt 设置一个int64类型到cache中
func (o *Cache) SetInt(key string, value int64, expiration time.Duration) {
	o.Set(key, value, expiration)
}

// GetInt 获取一个Int值，如果不存在，则返回默认值 0
func (o *Cache) GetInt(key string) int64 {
	v := o.Get(key)
	if v == nil {
		return 0
	}
	if val, ok := v.(int64); ok {
		return val
	}
	return 0
}

// SetMap 设置一个map[string]string类型到cache中
func (o *Cache) SetMap(key string, value map[string]string, expiration time.Duration) {
	o.Set(key, value, expiration)
}

// GetMap 获取一个map[string]string类型的值
func (o *Cache) GetMap(key string) map[string]string {
	v := o.Get(key)
	if v == nil {
		return nil
	}
	retval, ok := v.(map[string]string)
	if ok {
		return retval
	}
	return nil
}

// SetMapInterface 设置一个map[string]interface{}类型到cache中
func (o *Cache) SetMapInterface(key string, value map[string]interface{}, expiration time.Duration) {
	o.Set(key, value, expiration)
}

// GetMapInterface 获取一个map[string]interface{}值
func (o *Cache) GetMapInterface(key string) map[string]interface{} {
	v := o.Get(key)
	if v == nil {
		return nil
	}
	retval, ok := v.(map[string]interface{})
	if ok {
		return retval
	}
	return nil
}

// SetMapInterfaceList 设置一个[]map[string]interface{}类型到cache中
func (o *Cache) SetMapInterfaceList(key string, value []map[string]interface{}, expiration time.Duration) {
	o.Set(key, value, expiration)
}

// GetMapInterfaceList 获取一个 []map[string]interface{}值
func (o *Cache) GetMapInterfaceList(key string) []map[string]interface{} {
	v := o.Get(key)
	if v == nil {
		return nil
	}
	retval, ok := v.([]map[string]interface{})
	if ok {
		return retval
	}
	return nil
}

// SetStringList 设置一个字符串列表到Cache中
func (o *Cache) SetStringList(key string, value []string, expiration time.Duration) {
	o.Set(key, value, expiration)
}

// GetStringList 获取一个[]string值
func (o *Cache) GetStringList(key string) []string {
	v := o.Get(key)
	if v == nil {
		return nil
	}
	if val, ok := v.([]string); ok {
		return val
	}
	return nil
}

// Delete 删除key所对应的缓存值
func (o *Cache) Delete(key string) {
	cache := getGlobalCache()
	cache.Del(key)
	cache.Wait()
}

// CacheLoad 加载数据，如果缓存中存在数据，则直接使用，如果不存在数据，则调用 fn 构建数据返回，同时设置缓存
// 基于 Cache 基础类实现，简单上层业务的调用，在实现的业务开发过程中，推荐直接使用 LoadCache 封装
// ckey 缓存标识，在应用程序进程内，需要全局唯一，避免与其它缓存冲突
// expiration 设置缓存过期时间
// fn 数据构建逻辑，当缓存不存在时，调用此逻辑进行数据构建，由上层业务逻辑实现并注入
// 特别注意：如果 fn 返回的数据为 空 ，将不会缓存
func CacheLoad[O any](ckey string, expiration time.Duration, fn func() (result O, err error)) (result O, err error) {
	if len(ckey) == 0 {
		err = fmt.Errorf("cache.Load(): parameter ckey is required, and globally unique")
		return
	}
	if fn == nil {
		err = fmt.Errorf("cache.Load(): parameter fn undefined")
		return
	}

	cache := NewCache()
	if cached := cache.Get(ckey); cached != nil {
		if typed, ok := cached.(O); ok {
			return typed, nil
		}
	}

	result, err = fn()
	if err != nil {
		return result, err
	}

	if !Empty(result) {
		cache.Set(ckey, result, expiration)
	}

	return
}
