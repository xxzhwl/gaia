// Package rpcregistry — Nacos 实现（基于 nacos-sdk-go/v2 naming client）。
//
// 配置示例：
//
//	RpcServer:
//	  Registry:
//	    Enable: true
//	    Type: nacos
//	    Nacos:
//	      ServerAddrs: 127.0.0.1:8848        # 多个用逗号分隔
//	      Namespace: ""                      # 命名空间 ID，默认 public
//	      Group: DEFAULT_GROUP
//	      Cluster: DEFAULT
//	      Username: ""
//	      Password: ""
//	      AccessKey: ""                      # 阿里云 MSE 鉴权
//	      SecretKey: ""
//	      Endpoint: ""                       # 与 ServerAddrs 二选一
//	      Scheme: ""                         # http / https
//	      ContextPath: ""
//	      TimeoutMs: 5000
//	      Ephemeral: true                    # 临时实例（默认 true，进程退出自动剔除）
//	      Weight: 10                         # 实例权重（默认 10）
//
// @author gaia-framework
// @created 2026-06-24
package rpcregistry

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"github.com/xxzhwl/gaia"
)

// NacosRegistry Nacos 服务注册 / 发现实现。
type NacosRegistry struct {
	cli       naming_client.INamingClient
	group     string
	cluster   string
	ephemeral bool
	weight    float64

	mu          sync.Mutex
	registered  map[string]registeredInstance // serviceName -> 已注册实例信息（用于注销）
	subscribers map[string]*vo.SubscribeParam // serviceName -> 订阅参数（用于取消订阅）
}

type registeredInstance struct {
	ip   string
	port uint64
}

// NacosRegistryConfig Nacos 注册中心配置。
type NacosRegistryConfig struct {
	ServerAddrs []string
	Namespace   string
	Group       string
	Cluster     string
	Username    string
	Password    string
	AccessKey   string
	SecretKey   string
	Endpoint    string
	Scheme      string
	ContextPath string
	TimeoutMs   uint64
	LogLevel    string
	LogDir      string
	CacheDir    string
	Ephemeral   bool
	Weight      float64
}

// NewNacosRegistry 创建 Nacos 注册中心。
func NewNacosRegistry(cfg NacosRegistryConfig) (*NacosRegistry, error) {
	if len(cfg.ServerAddrs) == 0 && cfg.Endpoint == "" {
		return nil, fmt.Errorf("nacos registry: ServerAddrs 与 Endpoint 至少配置一个")
	}
	if cfg.Group == "" {
		cfg.Group = "DEFAULT_GROUP"
	}
	if cfg.Cluster == "" {
		cfg.Cluster = "DEFAULT"
	}
	if cfg.TimeoutMs == 0 {
		cfg.TimeoutMs = 5000
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "warn"
	}
	if cfg.Weight <= 0 {
		cfg.Weight = 10
	}

	serverConfigs := make([]constant.ServerConfig, 0, len(cfg.ServerAddrs))
	for _, addr := range cfg.ServerAddrs {
		host, port, err := splitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("nacos registry: 解析 ServerAddr[%s] 失败: %w", addr, err)
		}
		opts := []constant.ServerOption{}
		if cfg.Scheme != "" {
			opts = append(opts, constant.WithScheme(cfg.Scheme))
		}
		if cfg.ContextPath != "" {
			opts = append(opts, constant.WithContextPath(cfg.ContextPath))
		}
		serverConfigs = append(serverConfigs, *constant.NewServerConfig(host, port, opts...))
	}

	clientOpts := []constant.ClientOption{
		constant.WithNamespaceId(cfg.Namespace),
		constant.WithUsername(cfg.Username),
		constant.WithPassword(cfg.Password),
		constant.WithTimeoutMs(cfg.TimeoutMs),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogLevel(cfg.LogLevel),
		constant.WithLogDir(cfg.LogDir),
		constant.WithCacheDir(cfg.CacheDir),
	}
	if cfg.Endpoint != "" {
		clientOpts = append(clientOpts, constant.WithEndpoint(cfg.Endpoint))
	}
	if cfg.AccessKey != "" {
		clientOpts = append(clientOpts, constant.WithAccessKey(cfg.AccessKey))
	}
	if cfg.SecretKey != "" {
		clientOpts = append(clientOpts, constant.WithSecretKey(cfg.SecretKey))
	}
	clientConfig := *constant.NewClientConfig(clientOpts...)

	cli, err := clients.NewNamingClient(vo.NacosClientParam{
		ClientConfig:  &clientConfig,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Nacos naming 客户端失败: %w", err)
	}

	return &NacosRegistry{
		cli:         cli,
		group:       cfg.Group,
		cluster:     cfg.Cluster,
		ephemeral:   cfg.Ephemeral,
		weight:      cfg.Weight,
		registered:  make(map[string]registeredInstance),
		subscribers: make(map[string]*vo.SubscribeParam),
	}, nil
}

// NewNacosRegistryFromConfig 从 gaia 配置创建 Nacos 注册中心。
func NewNacosRegistryFromConfig(schema string) (*NacosRegistry, error) {
	prefix := schema + ".Registry.Nacos"
	addrsRaw := gaia.GetSafeConfStringWithDefault(prefix+".ServerAddrs", "")
	var addrs []string
	for _, a := range strings.Split(addrsRaw, ",") {
		if a = strings.TrimSpace(a); a != "" {
			addrs = append(addrs, a)
		}
	}
	return NewNacosRegistry(NacosRegistryConfig{
		ServerAddrs: addrs,
		Namespace:   gaia.GetSafeConfString(prefix + ".Namespace"),
		Group:       gaia.GetSafeConfStringWithDefault(prefix+".Group", "DEFAULT_GROUP"),
		Cluster:     gaia.GetSafeConfStringWithDefault(prefix+".Cluster", "DEFAULT"),
		Username:    gaia.GetSafeConfString(prefix + ".Username"),
		Password:    gaia.GetSafeConfString(prefix + ".Password"),
		AccessKey:   gaia.GetSafeConfString(prefix + ".AccessKey"),
		SecretKey:   gaia.GetSafeConfString(prefix + ".SecretKey"),
		Endpoint:    gaia.GetSafeConfString(prefix + ".Endpoint"),
		Scheme:      gaia.GetSafeConfString(prefix + ".Scheme"),
		ContextPath: gaia.GetSafeConfString(prefix + ".ContextPath"),
		TimeoutMs:   gaia.GetSafeConfUint64WithDefault(prefix+".TimeoutMs", 5000),
		LogLevel:    gaia.GetSafeConfStringWithDefault(prefix+".LogLevel", "warn"),
		LogDir:      gaia.GetSafeConfString(prefix + ".LogDir"),
		CacheDir:    gaia.GetSafeConfString(prefix + ".CacheDir"),
		Ephemeral:   gaia.GetSafeConfBoolWithDefault(prefix+".Ephemeral", true),
		Weight:      gaia.GetSafeConfFloat64WithDefault(prefix+".Weight", 10),
	})
}

// Register 注册服务实例到 Nacos。
func (r *NacosRegistry) Register(serviceName, host, port, version string) error {
	portU, err := strconv.ParseUint(port, 10, 64)
	if err != nil {
		return fmt.Errorf("nacos registry: 无效端口号 %s: %w", port, err)
	}

	ok, err := r.cli.RegisterInstance(vo.RegisterInstanceParam{
		Ip:          host,
		Port:        portU,
		ServiceName: serviceName,
		GroupName:   r.group,
		ClusterName: r.cluster,
		Weight:      r.weight,
		Enable:      true,
		Healthy:     true,
		Ephemeral:   r.ephemeral,
		Metadata: map[string]string{
			"version":   version,
			"framework": "gaia",
			"protocol":  "grpc",
		},
	})
	if err != nil {
		return fmt.Errorf("nacos registry: 注册实例失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("nacos registry: 注册实例返回 false（service=%s）", serviceName)
	}

	r.mu.Lock()
	r.registered[serviceName] = registeredInstance{ip: host, port: portU}
	r.mu.Unlock()
	return nil
}

// Deregister 从 Nacos 注销服务实例。
func (r *NacosRegistry) Deregister(serviceName string) error {
	r.mu.Lock()
	inst, ok := r.registered[serviceName]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("nacos registry: 服务 %s 未注册", serviceName)
	}

	if _, err := r.cli.DeregisterInstance(vo.DeregisterInstanceParam{
		Ip:          inst.ip,
		Port:        inst.port,
		ServiceName: serviceName,
		GroupName:   r.group,
		Cluster:     r.cluster,
		Ephemeral:   r.ephemeral,
	}); err != nil {
		return fmt.Errorf("nacos registry: 注销实例失败: %w", err)
	}

	r.mu.Lock()
	delete(r.registered, serviceName)
	r.mu.Unlock()
	return nil
}

// Discover 发现 Nacos 上指定服务的健康实例。
func (r *NacosRegistry) Discover(serviceName string) ([]*ServiceInstance, error) {
	insts, err := r.cli.SelectInstances(vo.SelectInstancesParam{
		ServiceName: serviceName,
		GroupName:   r.group,
		HealthyOnly: true,
	})
	if err != nil {
		return nil, fmt.Errorf("nacos registry: 发现服务失败: %w", err)
	}
	// 与 Watch 回调保持一致的可用实例判定口径
	return convertNacosInstances(filterUsableNacos(insts)), nil
}

// Watch 订阅 Nacos 服务实例变更。
func (r *NacosRegistry) Watch(serviceName string, onChange func(instances []*ServiceInstance)) (func(), error) {
	param := &vo.SubscribeParam{
		ServiceName: serviceName,
		GroupName:   r.group,
		SubscribeCallback: func(services []model.Instance, err error) {
			if err != nil {
				gaia.WarnF("[Nacos] 订阅 %s 回调错误: %v", serviceName, err)
				return
			}
			onChange(convertNacosInstances(filterUsableNacos(services)))
		},
	}
	if err := r.cli.Subscribe(param); err != nil {
		return nil, fmt.Errorf("nacos registry: 订阅失败: %w", err)
	}

	r.mu.Lock()
	r.subscribers[serviceName] = param
	r.mu.Unlock()

	return func() {
		_ = r.cli.Unsubscribe(param)
		r.mu.Lock()
		delete(r.subscribers, serviceName)
		r.mu.Unlock()
	}, nil
}

// Close 关闭 Nacos 客户端，注销所有已注册实例并取消订阅。
func (r *NacosRegistry) Close() error {
	r.mu.Lock()
	services := make([]string, 0, len(r.registered))
	for name := range r.registered {
		services = append(services, name)
	}
	subs := make([]*vo.SubscribeParam, 0, len(r.subscribers))
	for _, p := range r.subscribers {
		subs = append(subs, p)
	}
	r.mu.Unlock()

	for _, name := range services {
		if err := r.Deregister(name); err != nil {
			gaia.WarnF("[Nacos] Close 时注销 %s 失败: %v", name, err)
		}
	}
	for _, p := range subs {
		_ = r.cli.Unsubscribe(p)
	}
	r.cli.CloseClient()
	return nil
}

// filterUsableNacos 仅保留健康、启用且权重大于 0 的实例，
// 使 Discover 与 Watch 的可用实例判定口径保持一致。
func filterUsableNacos(insts []model.Instance) []model.Instance {
	out := make([]model.Instance, 0, len(insts))
	for _, s := range insts {
		if s.Healthy && s.Enable && s.Weight > 0 {
			out = append(out, s)
		}
	}
	return out
}

func convertNacosInstances(insts []model.Instance) []*ServiceInstance {
	out := make([]*ServiceInstance, 0, len(insts))
	for _, in := range insts {
		out = append(out, &ServiceInstance{
			ServiceID: in.InstanceId,
			Host:      in.Ip,
			Port:      int(in.Port),
			Version:   in.Metadata["version"],
			Weight:    in.Weight,
			Healthy:   in.Healthy,
			Metadata:  in.Metadata,
		})
	}
	return out
}

// splitHostPort 解析 "host:port" → (host, port)。
func splitHostPort(addr string) (string, uint64, error) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("地址缺少端口: %s", addr)
	}
	host := addr[:idx]
	portStr := addr[idx+1:]
	port, err := strconv.ParseUint(portStr, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("无效端口 %s: %w", portStr, err)
	}
	return host, port, nil
}
