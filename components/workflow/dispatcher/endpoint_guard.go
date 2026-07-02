package dispatcher

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// EndpointValidator 校验调度器即将访问的出站地址是否被允许。
//
// 流程定义中的 ServiceTask Endpoint / 自动化服务 BaseURL 可能由用户提交，若不加约束
// 会形成 SSRF（让引擎向任意内网地址发请求）。dispatcher 在每次出站请求前调用本校验器。
// 宿主可注入自定义实现（如域名白名单），默认使用 DefaultEndpointGuard 拒绝内网地址。
type EndpointValidator interface {
	// ValidateHTTP 校验 HTTP(S) 出站目标。
	ValidateHTTP(rawURL string) error
	// ValidateGRPC 校验 gRPC 出站目标（host:port 形式）。
	ValidateGRPC(target string) error
}

// DefaultEndpointGuard 是默认的出站地址校验器。
//
// 行为：
//   - HTTP 仅允许 http/https scheme；
//   - 解析主机名对应 IP，拒绝环回、私有网段、link-local、未指定地址（除非显式 AllowPrivate）；
//   - 当配置了 AllowedHosts 时，仅允许命中白名单的主机（精确或后缀匹配）。
type DefaultEndpointGuard struct {
	// AllowPrivate 为 true 时不拒绝私有/环回地址，便于本地开发与测试。
	AllowPrivate bool
	// AllowedHosts 为非空时启用主机白名单；元素以 "." 开头表示后缀匹配（含子域）。
	AllowedHosts []string
	// resolveIPs 允许测试替换 DNS 解析，生产留空时使用 net.LookupIP。
	resolveIPs func(host string) ([]net.IP, error)
}

// NewDefaultEndpointGuard 创建默认出站地址校验器。
func NewDefaultEndpointGuard() *DefaultEndpointGuard {
	return &DefaultEndpointGuard{}
}

// ValidateHTTP 校验 HTTP(S) 出站目标。
func (g *DefaultEndpointGuard) ValidateHTTP(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid dispatch url %q: %w", rawURL, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("dispatch url %q scheme %q is not allowed", rawURL, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("dispatch url %q has empty host", rawURL)
	}
	return g.checkHost(host)
}

// ValidateGRPC 校验 gRPC 出站目标。
func (g *DefaultEndpointGuard) ValidateGRPC(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("grpc target is empty")
	}
	host := target
	if h, _, err := net.SplitHostPort(target); err == nil {
		host = h
	}
	host = strings.TrimPrefix(host, "dns:///")
	if host == "" {
		return fmt.Errorf("grpc target %q has empty host", target)
	}
	return g.checkHost(host)
}

func (g *DefaultEndpointGuard) checkHost(host string) error {
	if len(g.AllowedHosts) > 0 && !g.hostAllowed(host) {
		return fmt.Errorf("host %q is not in the allowed hosts list", host)
	}
	if g.AllowPrivate {
		return nil
	}
	ips, err := g.lookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %q resolved to no addresses", host)
	}
	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return fmt.Errorf("host %q resolves to disallowed address %s", host, ip)
		}
	}
	return nil
}

func (g *DefaultEndpointGuard) hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, allowed := range g.AllowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if bare, ok := strings.CutPrefix(allowed, "."); ok {
			if host == bare || strings.HasSuffix(host, allowed) {
				return true
			}
			continue
		}
		if host == allowed {
			return true
		}
	}
	return false
}

func (g *DefaultEndpointGuard) lookupIP(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if g.resolveIPs != nil {
		return g.resolveIPs(host)
	}
	return net.LookupIP(host)
}

// isDisallowedIP 判断 IP 是否属于不应被工作流出站访问的网段。
func isDisallowedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// 显式拦截 IPv4 link-local 169.254.0.0/16（云 metadata 服务常用 169.254.169.254）。
	if v4 := ip.To4(); v4 != nil && v4[0] == 169 && v4[1] == 254 {
		return true
	}
	return false
}
