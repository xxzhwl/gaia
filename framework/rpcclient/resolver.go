// Package rpcclient gRPC 自定义 Resolver：对接 rpcregistry 实现客户端负载均衡。
//
// 工作机制：
//   - 注册一个 scheme="gaia" 的 resolver.Builder；
//   - Dial("gaia:///{serviceName}") 时，resolver 通过 registry.Discover 拿到实例列表，
//     推送给 gRPC 的负载均衡器（配合 round_robin 在多实例间分发请求）；
//   - 若 registry 支持 Watch，则订阅实例变更实时更新；否则按 interval 轮询 Discover。
//
// 这样即可复用 gRPC 原生的 round_robin / pick_first 负载均衡策略，
// 而不必在 resolveTarget 里手写"取第一个实例"。
//
// @author gaia-framework
// @created 2026-06-24
package rpcclient

import (
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/rpcregistry"
	"google.golang.org/grpc/resolver"
)

// Scheme 自定义 resolver 的 scheme。
const Scheme = "gaia"

// registryResolverBuilder 基于 registry 的 resolver builder。
// 每个 GrpcClient 持有自己的 builder（绑定其 registry），通过唯一 scheme 隔离。
type registryResolverBuilder struct {
	scheme   string
	registry rpcregistry.ServiceRegistry
	interval time.Duration // Watch 不支持时的轮询间隔
}

func newRegistryResolverBuilder(scheme string, reg rpcregistry.ServiceRegistry, interval time.Duration) *registryResolverBuilder {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &registryResolverBuilder{scheme: scheme, registry: reg, interval: interval}
}

func (b *registryResolverBuilder) Scheme() string { return b.scheme }

// Build 实现 resolver.Builder。target.Endpoint() 即服务名。
func (b *registryResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	serviceName := target.Endpoint()
	if serviceName == "" {
		return nil, fmt.Errorf("rpcclient resolver: 服务名为空（target=%v）", target)
	}

	r := &registryResolver{
		serviceName: serviceName,
		registry:    b.registry,
		cc:          cc,
		interval:    b.interval,
		stopCh:      make(chan struct{}),
	}

	// 首次解析
	r.resolveNow()

	// 优先 Watch，不支持则轮询
	stop, err := b.registry.Watch(serviceName, r.onInstances)
	if err == nil {
		r.watchStop = stop
	} else {
		go r.pollLoop()
	}
	return r, nil
}

// registryResolver 单服务的 resolver 实例。
type registryResolver struct {
	serviceName string
	registry    rpcregistry.ServiceRegistry
	cc          resolver.ClientConn
	interval    time.Duration

	stopCh    chan struct{}
	closeOnce sync.Once
	watchStop func()
}

// ResolveNow 实现 resolver.Resolver，gRPC 在需要刷新时调用。
func (r *registryResolver) ResolveNow(resolver.ResolveNowOptions) {
	go r.resolveNow()
}

func (r *registryResolver) resolveNow() {
	instances, err := r.registry.Discover(r.serviceName)
	if err != nil {
		gaia.WarnF("[rpcclient] resolver 解析 %s 失败: %v", r.serviceName, err)
		r.cc.ReportError(err)
		return
	}
	r.onInstances(instances)
}

func (r *registryResolver) onInstances(instances []*rpcregistry.ServiceInstance) {
	addrs := make([]resolver.Address, 0, len(instances))
	for _, inst := range instances {
		if !inst.Healthy {
			continue
		}
		addrs = append(addrs, resolver.Address{
			Addr:       inst.Addr(),
			ServerName: r.serviceName,
		})
	}
	if len(addrs) == 0 {
		gaia.WarnF("[rpcclient] resolver: 服务 %s 无可用实例", r.serviceName)
	}
	if err := r.cc.UpdateState(resolver.State{Addresses: addrs}); err != nil {
		gaia.WarnF("[rpcclient] resolver 更新地址失败 %s: %v", r.serviceName, err)
	}
}

func (r *registryResolver) pollLoop() {
	defer gaia.CatchPanic()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.resolveNow()
		}
	}
}

// Close 实现 resolver.Resolver。
func (r *registryResolver) Close() {
	r.closeOnce.Do(func() {
		close(r.stopCh)
		if r.watchStop != nil {
			r.watchStop()
		}
	})
}
