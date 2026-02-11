// Package gaia
// Author wanlizhan
// Create 2023-03-29
package gaia

import (
	"fmt"
	"sync"
	"time"
)

// 添加缓存大小限制和淘汰策略
var maxCacheSize = 10000 // 默认最大缓存数量

// 添加优雅关闭机制
var stopCleanCh chan struct{}

// Cache逻辑
// 本Cache封装使用一个特定的全局变量存储缓存数据，因此仅适用于同一Server实例服务内
// 数据结构体
type cacheNode struct {
	Data       any
	Expiration time.Time
}

var sysGlobalCacheStack map[string]cacheNode
var mutexSysGlobalCacheStack sync.RWMutex

func init() {
	sysGlobalCacheStack = make(map[string]cacheNode)
	stopCleanCh = make(chan struct{})
	clean()
}

// Cache 逻辑体
type Cache struct {
}

// NewCache 实例化
func NewCache() *Cache {
	return &Cache{}
}

// SetMaxCacheSize 添加配置函数，允许用户自定义最大缓存大小
func SetMaxCacheSize(size int) {
	mutexSysGlobalCacheStack.Lock()
	defer mutexSysGlobalCacheStack.Unlock()
	maxCacheSize = size
}

// 定时清理过期的缓存数据
func clean() {
	//Log("INFO", "Start a daemon goroutine to clean up expired data.")
	go func() {
		defer CatchPanic()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// 只获取读锁，收集需要删除的键
				mutexSysGlobalCacheStack.RLock()
				var expiredKeys []string
				now := time.Now()
				for key, node := range sysGlobalCacheStack {
					if now.After(node.Expiration) {
						expiredKeys = append(expiredKeys, key)
					}
				}
				mutexSysGlobalCacheStack.RUnlock()

				// 只在需要删除时获取写锁
				if len(expiredKeys) > 0 {
					mutexSysGlobalCacheStack.Lock()
					for _, key := range expiredKeys {
						// 再次检查，避免并发问题
						if node, exists := sysGlobalCacheStack[key]; exists && now.After(node.Expiration) {
							delete(sysGlobalCacheStack, key)
						}
					}
					mutexSysGlobalCacheStack.Unlock()
				}
			case <-stopCleanCh:
				return
			}
		}
	}()
}

// CloseClean 添加关闭函数
func CloseClean() {
	close(stopCleanCh)
}

// Set 设置一个值到cache中
func (o *Cache) Set(key string, value any, expiration time.Duration) {
	mutexSysGlobalCacheStack.Lock()
	defer mutexSysGlobalCacheStack.Unlock()

	// 检查缓存大小，如果超过限制，删除最早过期的数据
	if len(sysGlobalCacheStack) >= maxCacheSize {
		var oldestKey string
		var oldestTime time.Time
		for k, node := range sysGlobalCacheStack {
			if oldestTime.IsZero() || node.Expiration.Before(oldestTime) {
				oldestKey = k
				oldestTime = node.Expiration
			}
		}
		delete(sysGlobalCacheStack, oldestKey)
	}

	sysGlobalCacheStack[key] = cacheNode{Data: value, Expiration: time.Now().Add(expiration)}
}

// Get 从cache中获取一个值
func (o *Cache) Get(key string) any {
	//操作全局map，加锁
	mutexSysGlobalCacheStack.RLock()
	v, ok := sysGlobalCacheStack[key]
	mutexSysGlobalCacheStack.RUnlock()
	if !ok {
		return nil
	}

	//检查是否过期
	if time.Now().Before(v.Expiration) {
		return v.Data
	}

	// 异步删除过期数据
	go func() {
		mutexSysGlobalCacheStack.Lock()
		// 再次检查，避免并发问题
		if node, exists := sysGlobalCacheStack[key]; exists && time.Now().After(node.Expiration) {
			delete(sysGlobalCacheStack, key)
		}
		mutexSysGlobalCacheStack.Unlock()
	}()

	return nil
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
	} else {
		return 0
	}
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
	} else {
		return nil
	}
}

// Delete 删除key所对应的缓存值
func (o *Cache) Delete(key string) {
	mutexSysGlobalCacheStack.Lock()
	defer mutexSysGlobalCacheStack.Unlock()
	if len(sysGlobalCacheStack) > 0 {
		delete(sysGlobalCacheStack, key)
	}
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

	// 直接使用全局缓存，避免创建不必要的实例
	mutexSysGlobalCacheStack.RLock()
	v, ok := sysGlobalCacheStack[ckey]
	mutexSysGlobalCacheStack.RUnlock()
	if ok {
		if time.Now().Before(v.Expiration) {
			if v, ok := v.Data.(O); ok {
				return v, nil
			}
		} else {
			// 异步删除过期数据
			go func() {
				mutexSysGlobalCacheStack.Lock()
				if node, exists := sysGlobalCacheStack[ckey]; exists && time.Now().After(node.Expiration) {
					delete(sysGlobalCacheStack, ckey)
				}
				mutexSysGlobalCacheStack.Unlock()
			}()
		}
	}

	//未命中缓存，构建数据
	result, err = fn()
	if err != nil {
		return result, err
	}

	if !Empty(result) {
		// 设置缓存
		mutexSysGlobalCacheStack.Lock()
		sysGlobalCacheStack[ckey] = cacheNode{Data: result, Expiration: time.Now().Add(expiration)}
		mutexSysGlobalCacheStack.Unlock()
	}

	return
}
