package rpcclient

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/xxzhwl/gaia/framework/rpcregistry"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// ---- fakes ----

type fakeClientConn struct {
	mu    sync.Mutex
	state resolver.State
	errs  []error
}

func (f *fakeClientConn) UpdateState(s resolver.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
	return nil
}
func (f *fakeClientConn) ReportError(e error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, e)
}
func (f *fakeClientConn) NewAddress([]resolver.Address)                        {}
func (f *fakeClientConn) NewServiceConfig(string)                              {}
func (f *fakeClientConn) ParseServiceConfig(string) *serviceconfig.ParseResult { return nil }

func (f *fakeClientConn) addrs() []resolver.Address {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state.Addresses
}

type fakeRegistry struct {
	instances []*rpcregistry.ServiceInstance
	discErr   error
	watchErr  error
}

func (f *fakeRegistry) Register(string, string, string, string) error { return nil }
func (f *fakeRegistry) Deregister(string) error                       { return nil }
func (f *fakeRegistry) Discover(string) ([]*rpcregistry.ServiceInstance, error) {
	return f.instances, f.discErr
}
func (f *fakeRegistry) Watch(_ string, _ func([]*rpcregistry.ServiceInstance)) (func(), error) {
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	return func() {}, nil
}
func (f *fakeRegistry) Close() error { return nil }

func targetForService(name string) resolver.Target {
	return resolver.Target{URL: url.URL{Scheme: Scheme, Path: "/" + name}}
}

// ---- tests ----

func TestScheme(t *testing.T) {
	if Scheme != "gaia" {
		t.Fatalf("Scheme 应为 gaia，实际 %q", Scheme)
	}
}

func TestResolver_OnInstancesFiltersUnhealthy(t *testing.T) {
	cc := &fakeClientConn{}
	r := &registryResolver{serviceName: "svc", cc: cc}
	r.onInstances([]*rpcregistry.ServiceInstance{
		{Host: "1.1.1.1", Port: 1, Healthy: true},
		{Host: "2.2.2.2", Port: 2, Healthy: false}, // 应被过滤
		{Host: "3.3.3.3", Port: 3, Healthy: true},
	})

	addrs := cc.addrs()
	if len(addrs) != 2 {
		t.Fatalf("应只保留 2 个健康实例，实际 %d", len(addrs))
	}
	if addrs[0].Addr != "1.1.1.1:1" {
		t.Fatalf("地址格式应为 host:port，实际 %q", addrs[0].Addr)
	}
}

func TestBuilder_BuildEmptyService(t *testing.T) {
	b := newRegistryResolverBuilder("gaia-test-empty", &fakeRegistry{}, time.Second)
	if _, err := b.Build(targetForService(""), &fakeClientConn{}, resolver.BuildOptions{}); err == nil {
		t.Fatal("空服务名应报错")
	}
}

func TestBuilder_BuildResolvesInstances(t *testing.T) {
	reg := &fakeRegistry{instances: []*rpcregistry.ServiceInstance{
		{Host: "10.0.0.1", Port: 9090, Healthy: true},
	}}
	b := newRegistryResolverBuilder("gaia-test-ok", reg, time.Second)
	cc := &fakeClientConn{}

	r, err := b.Build(targetForService("UserService"), cc, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("Build 不应报错：%v", err)
	}
	defer r.Close()

	// Build 内会同步执行首次解析
	if len(cc.addrs()) != 1 {
		t.Fatalf("应解析出 1 个实例，实际 %d", len(cc.addrs()))
	}
}

func TestBuilder_DefaultInterval(t *testing.T) {
	b := newRegistryResolverBuilder("gaia-test-interval", &fakeRegistry{}, 0)
	if b.interval != 10*time.Second {
		t.Fatalf("interval<=0 应回退为 10s，实际 %v", b.interval)
	}
}
