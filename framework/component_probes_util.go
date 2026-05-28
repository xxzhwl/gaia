// Package framework 小工具：net.Dial 包装，与 component_probes.go 解耦。
package framework

import (
	"net"
	"time"
)

func netDial(network, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}
