// Package remoteConfig 包注释
// @author wanlizhan
// @created 2024-12-05
package remoteConfig

import (
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/consul"
	"github.com/xxzhwl/gaia/dic"
)

// ConfigCenter 配置中心接口
type ConfigCenter interface {
	GetConfig(key string) (any, bool, error)
	WatchConfig(key string, callback func(any)) error
}

// ConsulConfigCenter Consul配置中心实现
type ConsulConfigCenter struct {
	endPoint      string
	path          string
	cache         map[string]*configCache
	mu            sync.RWMutex
	cacheTTL      time.Duration
	watchers      map[string]*configWatcher // 配置监听器
	watchersMu    sync.RWMutex
	client        *consul.Client // 复用客户端
	stopWatchChan chan struct{}  // 停止监听信号
}

// configWatcher 配置监听器
type configWatcher struct {
	key       string
	callback  func(any)
	lastIndex uint64 // Consul阻塞查询的索引
	stopChan  chan struct{}
}

// configCache 配置缓存
type configCache struct {
	value     any
	timestamp time.Time
	exists    bool
}

// NewConsulConfigCenter 创建Consul配置中心实例
func NewConsulConfigCenter(endPoint, path string) (*ConsulConfigCenter, error) {
	client, err := consul.NewClient(endPoint)
	if err != nil {
		return nil, fmt.Errorf("创建Consul客户端失败: %w", err)
	}

	return &ConsulConfigCenter{
		endPoint:      endPoint,
		path:          path,
		cache:         make(map[string]*configCache),
		cacheTTL:      5 * time.Minute, // 默认缓存5分钟
		watchers:      make(map[string]*configWatcher),
		stopWatchChan: make(chan struct{}),
		client:        client,
	}, nil
}

// GetConfig 获取配置
func (c *ConsulConfigCenter) GetConfig(key string) (any, bool, error) {
	// 检查缓存
	if c.isCacheValid(key) {
		c.mu.RLock()
		cached := c.cache[key]
		c.mu.RUnlock()
		return cached.value, cached.exists, nil
	}

	// 从Consul获取配置（使用复用的客户端）
	configs, err := c.client.GetConfigs(c.path)
	if err != nil {
		return nil, false, fmt.Errorf("获取配置失败: %w", err)
	}

	mapPath, err := dic.GetValueByMapPath(configs, key)
	if err != nil {
		return nil, false, fmt.Errorf("解析配置失败: %w", err)
	}

	exists := mapPath != nil

	// 更新缓存
	c.mu.Lock()
	c.cache[key] = &configCache{
		value:     mapPath,
		timestamp: time.Now(),
		exists:    exists,
	}
	c.mu.Unlock()

	return mapPath, exists, nil
}

// WatchConfig 监听配置变更
func (c *ConsulConfigCenter) WatchConfig(key string, callback func(any)) error {
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}

	c.watchersMu.Lock()
	defer c.watchersMu.Unlock()

	// 检查是否已存在该key的监听器
	if _, exists := c.watchers[key]; exists {
		return fmt.Errorf("watcher for key '%s' already exists", key)
	}

	// 创建监听器
	watcher := &configWatcher{
		key:       key,
		callback:  callback,
		lastIndex: 0,
		stopChan:  make(chan struct{}),
	}
	c.watchers[key] = watcher

	// 启动监听协程
	go c.watchConfigChange(watcher)

	return nil
}

// watchConfigChange 监听配置变更（内部方法）
func (c *ConsulConfigCenter) watchConfigChange(watcher *configWatcher) {
	ticker := time.NewTicker(5 * time.Second) // 轮询间隔
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.checkConfigChange(watcher)
		case <-watcher.stopChan:
			return
		case <-c.stopWatchChan:
			return
		}
	}
}

// checkConfigChange 检查配置是否变更
func (c *ConsulConfigCenter) checkConfigChange(watcher *configWatcher) {
	defer func() {
		if r := recover(); r != nil {
			gaia.ErrorF("配置监听发生panic: %v", r)
		}
	}()

	// 获取当前配置值
	newValue, exists, err := c.GetConfig(watcher.key)
	if err != nil {
		gaia.ErrorF("获取配置失败[key=%s]: %v", watcher.key, err)
		return
	}

	if !exists {
		return
	}

	// 获取缓存中的旧值
	c.mu.RLock()
	cached, hasCache := c.cache[watcher.key]
	c.mu.RUnlock()

	// 如果有缓存且值发生变化，触发回调
	if hasCache && !isConfigEqual(cached.value, newValue) {
		gaia.InfoF("配置变更 detected[key=%s]", watcher.key)
		go watcher.callback(newValue)
	}
}

// isConfigEqual 比较两个配置值是否相等
func isConfigEqual(a, b any) bool {
	// 简单实现：转换为字符串比较
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// StopWatch 停止指定key的配置监听
func (c *ConsulConfigCenter) StopWatch(key string) error {
	c.watchersMu.Lock()
	defer c.watchersMu.Unlock()

	watcher, exists := c.watchers[key]
	if !exists {
		return fmt.Errorf("watcher for key '%s' not found", key)
	}

	close(watcher.stopChan)
	delete(c.watchers, key)
	return nil
}

// StopAllWatches 停止所有配置监听
func (c *ConsulConfigCenter) StopAllWatches() {
	c.watchersMu.Lock()
	defer c.watchersMu.Unlock()

	close(c.stopWatchChan)
	for key, watcher := range c.watchers {
		close(watcher.stopChan)
		delete(c.watchers, key)
	}
}

// isCacheValid 检查缓存是否有效
func (c *ConsulConfigCenter) isCacheValid(key string) bool {
	c.mu.RLock()
	cached, exists := c.cache[key]
	c.mu.RUnlock()

	if !exists {
		return false
	}

	return time.Since(cached.timestamp) < c.cacheTTL
}

// RegisterConsulRemoteConf 注入远程配置中心为Consul
func RegisterConsulRemoteConf() {
	endPoint := gaia.GetSafeConfString("RemoteConfig.EndPoint")
	path := gaia.GetSafeConfString("RemoteConfig.Path")
	if len(endPoint) == 0 || len(path) == 0 {
		gaia.Info("远程配置中心配置无效")
		return
	}

	configCenter, err := NewConsulConfigCenter(endPoint, path)
	if err != nil {
		gaia.ErrorF("创建Consul配置中心失败: %v", err)
		return
	}
	gaia.InfoF("注入远程配置中心[Consul:%s-%s]", endPoint, path)

	gaia.GetConfFromRemote = func(key string) (any, bool, error) {
		return configCenter.GetConfig(key)
	}
}

// RegisterConfigCenter 注册自定义配置中心
func RegisterConfigCenter(configCenter ConfigCenter) {
	gaia.Info("注入自定义远程配置中心")
	gaia.GetConfFromRemote = func(key string) (any, bool, error) {
		return configCenter.GetConfig(key)
	}
}
