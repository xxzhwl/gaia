// Package remoteConfig — Consul Provider
//
// 配置示例：
//
//	RemoteConfig:
//	  Type: consul
//	  EndPoint: 127.0.0.1:8500
//	  Path: app/config
//	  CacheTTL: 300         # 秒，可选，默认 300
//	  WatchInterval: 5      # 秒，可选，默认 5
//
// Consul 使用 KV 存储整颗配置树（JSON 格式），本 Provider 基于 BaseCenter
// 的轮询能力做配置监听。
//
// @author wanlizhan
// @refactored 2026-04-24
package remoteConfig

import (
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/consul"
	"github.com/xxzhwl/gaia/dic"
)

func init() {
	RegisterProvider("consul", newConsulProvider)
}

// ConsulConfigCenter Consul 配置中心实现
type ConsulConfigCenter struct {
	*BaseCenter
	endPoint string
	path     string
	client   *consul.Client
}

// NewConsulConfigCenter 创建 Consul 配置中心实例（保留老入口，向后兼容）
func NewConsulConfigCenter(endPoint, path string) (*ConsulConfigCenter, error) {
	return newConsulCenter(endPoint, path, BaseOption{})
}

// newConsulProvider 工厂函数：供 InitFromConfig 自动装配使用
func newConsulProvider(cfg map[string]any) (ConfigCenter, error) {
	endPoint, _ := dic.GetValueByMapPath(cfg, "EndPoint")
	path, _ := dic.GetValueByMapPath(cfg, "Path")
	ttlRaw, _ := dic.GetValueByMapPath(cfg, "CacheTTL")
	intervalRaw, _ := dic.GetValueByMapPath(cfg, "WatchInterval")

	ep, _ := endPoint.(string)
	p, _ := path.(string)
	if ep == "" || p == "" {
		return nil, fmt.Errorf("RemoteConfig.EndPoint / RemoteConfig.Path 必须配置")
	}

	return newConsulCenter(ep, p, BaseOption{
		CacheTTL:      toDuration(ttlRaw, 0) * time.Second,
		WatchInterval: toDuration(intervalRaw, 0) * time.Second,
	})
}

// newConsulCenter 通用构造入口
func newConsulCenter(endPoint, path string, opt BaseOption) (*ConsulConfigCenter, error) {
	client, err := consul.NewClient(endPoint)
	if err != nil {
		return nil, fmt.Errorf("创建 Consul 客户端失败: %w", err)
	}

	cc := &ConsulConfigCenter{
		endPoint: endPoint,
		path:     path,
		client:   client,
	}
	cc.BaseCenter = NewBaseCenter(cc.fetch, opt)
	return cc, nil
}

// fetch 从 Consul 拉取配置（不带缓存，缓存交给 BaseCenter 管）
func (c *ConsulConfigCenter) fetch(key string) (any, bool, error) {
	configs, err := c.client.GetConfigs(c.path)
	if err != nil {
		return nil, false, fmt.Errorf("获取配置失败: %w", err)
	}
	val, err := dic.GetValueByMapPath(configs, key)
	if err != nil {
		// GetValueByMapPath 路径不存在会返回 error，这里认为是"未找到"
		return nil, false, nil
	}
	return val, val != nil, nil
}

// RegisterConsulRemoteConf 注入远程配置中心为 Consul（向后兼容的老接口）
//
// Deprecated: 请使用 InitFromConfig + 配置 RemoteConfig.Type=consul
func RegisterConsulRemoteConf() {
	endPoint := gaia.GetSafeConfString("RemoteConfig.EndPoint")
	path := gaia.GetSafeConfString("RemoteConfig.Path")
	if endPoint == "" || path == "" {
		gaia.Info("远程配置中心配置无效")
		return
	}

	center, err := newConsulCenter(endPoint, path, BaseOption{})
	if err != nil {
		gaia.ErrorF("创建 Consul 配置中心失败: %v", err)
		return
	}
	gaia.InfoF("注入远程配置中心[Consul:%s-%s]", endPoint, path)
	RegisterConfigCenter(center)
}

// ================================ 辅助函数 ================================

// toDuration 把 map 里读出的 interface{} 统一转成 time.Duration（秒数）
// 支持 int / int64 / float64 / string；失败返回 fallback
func toDuration(raw any, fallback time.Duration) time.Duration {
	switch v := raw.(type) {
	case int:
		return time.Duration(v)
	case int64:
		return time.Duration(v)
	case float64:
		return time.Duration(int64(v))
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d / time.Second
		}
	}
	return fallback
}
