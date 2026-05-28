// Package rpcregistry — Consul 实现。
// @author gaia-framework
// @created 2026-06-24
package rpcregistry

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/xxzhwl/gaia"
)

// ConsulRegistry Consul 服务注册 / 发现实现。
type ConsulRegistry struct {
	client       *api.Client
	mu           sync.Mutex
	serviceIDs   map[string]string // serviceName -> serviceID
	checkTTL     time.Duration     // 健康检查间隔
	deregCritTTL string            // 健康检查失败后自动注销时间
	tag          string            // 服务标签（过滤用）
}

// ConsulRegistryConfig Consul 注册配置。
type ConsulRegistryConfig struct {
	Endpoint   string        // Consul 地址（如 "localhost:8500"）
	CheckTTL   time.Duration // 健康检查间隔（默认 10s）
	DeregAfter string        // 健康检查失败后注销时间（默认 "30s"）
	Token      string        // Consul ACL Token（可选）
	Datacenter string        // 数据中心（可选）
	Tag        string        // 服务标签（默认 "gaia-rpc"）
}

// NewConsulRegistry 创建 Consul 注册中心。
func NewConsulRegistry(cfg ConsulRegistryConfig) (*ConsulRegistry, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = gaia.GetSafeConfStringWithDefault("Consul.Endpoint", "localhost:8500")
	}
	if cfg.CheckTTL == 0 {
		cfg.CheckTTL = 10 * time.Second
	}
	if cfg.DeregAfter == "" {
		cfg.DeregAfter = "30s"
	}
	if cfg.Tag == "" {
		cfg.Tag = "gaia-rpc"
	}

	config := api.DefaultConfig()
	config.Address = cfg.Endpoint
	if cfg.Token != "" {
		config.Token = cfg.Token
	}
	if cfg.Datacenter != "" {
		config.Datacenter = cfg.Datacenter
	}

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("创建 Consul 客户端失败: %w", err)
	}

	return &ConsulRegistry{
		client:       client,
		serviceIDs:   make(map[string]string),
		checkTTL:     cfg.CheckTTL,
		deregCritTTL: cfg.DeregAfter,
		tag:          cfg.Tag,
	}, nil
}

// NewConsulRegistryFromConfig 从 gaia 配置创建 Consul 注册中心。
func NewConsulRegistryFromConfig(schema string) (*ConsulRegistry, error) {
	return NewConsulRegistry(ConsulRegistryConfig{
		Endpoint:   gaia.GetSafeConfStringWithDefault(schema+".Registry.Endpoint", "localhost:8500"),
		CheckTTL:   time.Duration(gaia.GetSafeConfInt64WithDefault(schema+".Registry.CheckTTL", 10)) * time.Second,
		DeregAfter: gaia.GetSafeConfStringWithDefault(schema+".Registry.DeregAfter", "30s"),
		Token:      gaia.GetSafeConfString(schema + ".Registry.Token"),
		Datacenter: gaia.GetSafeConfString(schema + ".Registry.Datacenter"),
		Tag:        gaia.GetSafeConfStringWithDefault(schema+".Registry.Tag", "gaia-rpc"),
	})
}

// Register 注册服务到 Consul。
func (r *ConsulRegistry) Register(serviceName, host, port, version string) error {
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("无效的端口号: %s", port)
	}

	serviceID := fmt.Sprintf("%s-%s-%s", serviceName, host, port)

	registration := &api.AgentServiceRegistration{
		ID:      serviceID,
		Name:    serviceName,
		Address: host,
		Port:    portInt,
		Tags:    []string{r.tag, "version:" + version},
		Meta: map[string]string{
			"version":   version,
			"framework": "gaia",
			"protocol":  "grpc",
		},
		Check: &api.AgentServiceCheck{
			GRPC:                           fmt.Sprintf("%s:%d", host, portInt),
			Interval:                       r.checkTTL.String(),
			DeregisterCriticalServiceAfter: r.deregCritTTL,
			Timeout:                        "5s",
		},
	}

	if err := r.client.Agent().ServiceRegister(registration); err != nil {
		return fmt.Errorf("注册服务到 Consul 失败: %w", err)
	}

	r.mu.Lock()
	r.serviceIDs[serviceName] = serviceID
	r.mu.Unlock()
	return nil
}

// Deregister 从 Consul 注销服务。
func (r *ConsulRegistry) Deregister(serviceName string) error {
	r.mu.Lock()
	serviceID, ok := r.serviceIDs[serviceName]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("服务 %s 未注册", serviceName)
	}

	if err := r.client.Agent().ServiceDeregister(serviceID); err != nil {
		return fmt.Errorf("从 Consul 注销服务失败: %w", err)
	}

	r.mu.Lock()
	delete(r.serviceIDs, serviceName)
	r.mu.Unlock()
	return nil
}

// Discover 从 Consul 发现健康的服务实例。
func (r *ConsulRegistry) Discover(serviceName string) ([]*ServiceInstance, error) {
	entries, _, err := r.client.Health().Service(serviceName, r.tag, true, nil)
	if err != nil {
		return nil, fmt.Errorf("从 Consul 发现服务失败: %w", err)
	}

	instances := make([]*ServiceInstance, 0, len(entries))
	for _, entry := range entries {
		instances = append(instances, &ServiceInstance{
			ServiceID: entry.Service.ID,
			Host:      entry.Service.Address,
			Port:      entry.Service.Port,
			Version:   entry.Service.Meta["version"],
			Weight:    1,
			Healthy:   true,
			Metadata:  entry.Service.Meta,
		})
	}

	return instances, nil
}

// Watch 通过 Consul blocking query 监听服务实例变更。
func (r *ConsulRegistry) Watch(serviceName string, onChange func(instances []*ServiceInstance)) (func(), error) {
	stopCh := make(chan struct{})
	go func() {
		defer gaia.CatchPanic()
		var lastIndex uint64
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			q := &api.QueryOptions{WaitIndex: lastIndex, WaitTime: 30 * time.Second}
			entries, meta, err := r.client.Health().Service(serviceName, r.tag, true, q)
			if err != nil {
				gaia.WarnF("[Consul] Watch %s 失败: %v，5s 后重试", serviceName, err)
				select {
				case <-stopCh:
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}
			if meta.LastIndex == lastIndex {
				// 超时无变更，继续 long-poll
				continue
			}
			lastIndex = meta.LastIndex

			instances := make([]*ServiceInstance, 0, len(entries))
			for _, entry := range entries {
				instances = append(instances, &ServiceInstance{
					ServiceID: entry.Service.ID,
					Host:      entry.Service.Address,
					Port:      entry.Service.Port,
					Version:   entry.Service.Meta["version"],
					Weight:    1,
					Healthy:   true,
					Metadata:  entry.Service.Meta,
				})
			}
			onChange(instances)
		}
	}()
	return func() { close(stopCh) }, nil
}

// Close Consul 客户端无需显式关闭，实现为 no-op。
func (r *ConsulRegistry) Close() error { return nil }
