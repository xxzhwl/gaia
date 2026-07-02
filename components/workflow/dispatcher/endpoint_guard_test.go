package dispatcher

import (
	"net"
	"testing"
)

func TestDefaultEndpointGuardRejectsPrivateAndLoopback(t *testing.T) {
	guard := NewDefaultEndpointGuard()
	cases := []string{
		"http://127.0.0.1/tasks",
		"http://localhost/tasks",
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.5/internal",
		"http://192.168.1.10/internal",
	}
	for _, raw := range cases {
		if err := guard.ValidateHTTP(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestDefaultEndpointGuardRejectsNonHTTPScheme(t *testing.T) {
	guard := NewDefaultEndpointGuard()
	if err := guard.ValidateHTTP("file:///etc/passwd"); err == nil {
		t.Fatal("expected file scheme to be rejected")
	}
}

func TestDefaultEndpointGuardAllowsPublicAddress(t *testing.T) {
	guard := &DefaultEndpointGuard{
		resolveIPs: func(string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		},
	}
	if err := guard.ValidateHTTP("https://example.com/tasks"); err != nil {
		t.Fatalf("expected public address to be allowed, got %v", err)
	}
}

func TestDefaultEndpointGuardAllowedHostsWhitelist(t *testing.T) {
	guard := &DefaultEndpointGuard{
		AllowPrivate: true,
		AllowedHosts: []string{".internal.example.com"},
	}
	if err := guard.ValidateHTTP("http://worker.internal.example.com/tasks"); err != nil {
		t.Fatalf("expected whitelisted host to be allowed, got %v", err)
	}
	if err := guard.ValidateHTTP("http://evil.example.com/tasks"); err == nil {
		t.Fatal("expected non-whitelisted host to be rejected")
	}
}

func TestDefaultEndpointGuardAllowPrivateSkipsResolution(t *testing.T) {
	guard := &DefaultEndpointGuard{AllowPrivate: true}
	if err := guard.ValidateGRPC("bufnet"); err != nil {
		t.Fatalf("expected bufnet target to pass when private allowed, got %v", err)
	}
	if err := guard.ValidateHTTP("http://127.0.0.1:8080/tasks"); err != nil {
		t.Fatalf("expected loopback to pass when private allowed, got %v", err)
	}
}
