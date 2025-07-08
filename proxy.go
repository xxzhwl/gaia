// Package gaia 注释
// @author wanlizhan
// @created 2024/4/29
package gaia

import "sync"

var ProxyRouter = map[string]map[string]any{}

var proxyLocker sync.RWMutex

func RegisterProxy(class, service string, proxy any) {
	proxyLocker.Lock()
	defer proxyLocker.Unlock()
	if _, ok := ProxyRouter[class]; !ok {
		ProxyRouter[class] = map[string]any{}
	}
	InfoF("注册Class[%s]-Service[%s]", class, service)
	ProxyRouter[class][service] = proxy
}

func GetProxy(class, service string) any {
	proxyLocker.RLock()
	defer proxyLocker.RUnlock()
	if _, ok := ProxyRouter[class]; !ok {
		return nil
	}
	if _, ok := ProxyRouter[class][service]; !ok {
		return nil
	} else {
		return ProxyRouter[class][service]
	}
}

func GetServiceProxies(class string) map[string]any {
	proxyLocker.RLock()
	defer proxyLocker.RUnlock()
	if _, ok := ProxyRouter[class]; !ok {
		return nil
	}
	return ProxyRouter[class]
}
