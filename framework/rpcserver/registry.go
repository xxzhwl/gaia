// Package rpcserver 服务注册/发现
// 提供 ServiceRegistry 接口和 Consul 实现
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"fmt"
	"strconv"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/xxzhwl/gaia"
)

// ServiceRegistry 服务注册/发现接口
type ServiceRegistry interface {
	// Register 注册服务到注册中心
	Register(serviceName, host, port, version string) error

	// Deregister 从注册中心注销服务
	Deregister(serviceName string) error

	// Discover 发现服务实例列表
	Discover(serviceName string) ([]*ServiceInstance, error)
}

// ServiceInstance 服务实例信息
type ServiceInstance struct {
	ServiceID string
	Host      string
	Port      int
	Version   string
	Healthy   bool
	Metadata  map[string]string
}

// ===========================
// Consul 实现
// ===========================

// ConsulRegistry Consul 服务注册/发现实现
type ConsulRegistry struct {
	client      *api.Client
	serviceIDs  map[string]string // serviceName -> serviceID 映射
	checkTTL    time.Duration     // 健康检查 TTL
	deregCritTTL string           // 服务失效后自动注销时间
}

// ConsulRegistryConfig Consul 注册配置
type ConsulRegistryConfig struct {
	Endpoint     string        // Consul 地址（如 "localhost:8500"）
	CheckTTL     time.Duration // 健康检查 TTL（默认 10 秒）
	DeregAfter   string        // 健康检查失败后注销时间（默认 "30s"）
	Token        string        // Consul ACL Token（可选）
	Datacenter   string        // 数据中心（可选）
}

// NewConsulRegistry 创建 Consul 服务注册中心
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
	}, nil
}

// NewConsulRegistryFromConfig 从 gaia 配置创建 Consul 注册中心
func NewConsulRegistryFromConfig(schema string) (*ConsulRegistry, error) {
	return NewConsulRegistry(ConsulRegistryConfig{
		Endpoint:   gaia.GetSafeConfStringWithDefault(schema+".Registry.Endpoint", "localhost:8500"),
		CheckTTL:   time.Duration(gaia.GetSafeConfInt64WithDefault(schema+".Registry.CheckTTL", 10)) * time.Second,
		DeregAfter: gaia.GetSafeConfStringWithDefault(schema+".Registry.DeregAfter", "30s"),
		Token:      gaia.GetSafeConfString(schema + ".Registry.Token"),
		Datacenter: gaia.GetSafeConfString(schema + ".Registry.Datacenter"),
	})
}

// Register 注册服务到 Consul
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
		Tags:    []string{"gaia-rpc", "version:" + version},
		Meta: map[string]string{
			"version":   version,
			"framework": "gaia",
			"protocol":  "grpc",
		},
		Check: &api.AgentServiceCheck{
			// 使用 gRPC 健康检查协议
			GRPC:                           fmt.Sprintf("%s:%d", host, portInt),
			Interval:                       r.checkTTL.String(),
			DeregisterCriticalServiceAfter: r.deregCritTTL,
			Timeout:                        "5s",
		},
	}

	if err := r.client.Agent().ServiceRegister(registration); err != nil {
		return fmt.Errorf("注册服务到 Consul 失败: %w", err)
	}

	r.serviceIDs[serviceName] = serviceID
	return nil
}

// Deregister 从 Consul 注销服务
func (r *ConsulRegistry) Deregister(serviceName string) error {
	serviceID, ok := r.serviceIDs[serviceName]
	if !ok {
		return fmt.Errorf("服务 %s 未注册", serviceName)
	}

	if err := r.client.Agent().ServiceDeregister(serviceID); err != nil {
		return fmt.Errorf("从 Consul 注销服务失败: %w", err)
	}

	delete(r.serviceIDs, serviceName)
	return nil
}

// Discover 从 Consul 发现健康的服务实例
func (r *ConsulRegistry) Discover(serviceName string) ([]*ServiceInstance, error) {
	entries, _, err := r.client.Health().Service(serviceName, "gaia-rpc", true, nil)
	if err != nil {
		return nil, fmt.Errorf("从 Consul 发现服务失败: %w", err)
	}

	var instances []*ServiceInstance
	for _, entry := range entries {
		instances = append(instances, &ServiceInstance{
			ServiceID: entry.Service.ID,
			Host:      entry.Service.Address,
			Port:      entry.Service.Port,
			Version:   entry.Service.Meta["version"],
			Healthy:   true,
			Metadata:  entry.Service.Meta,
		})
	}

	return instances, nil
}
