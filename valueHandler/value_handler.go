// Package valueHandler 包注释
// @author wanlizhan
// @created 2024/6/13
package valueHandler

import (
	"github.com/pkg/errors"
	"sync"
)

type ValueHandler interface {
	NewValue(value any) any
}

var valueHandlerProxyMap = map[string]ValueHandler{}
var handlerLocker = sync.RWMutex{}

func RegisterValueHandler(handlerName string, handler ValueHandler) {
	if len(handlerName) == 0 {
		return
	}
	handlerLocker.Lock()
	defer handlerLocker.Unlock()
	if valueHandlerProxyMap == nil {
		valueHandlerProxyMap = make(map[string]ValueHandler)
	}
	valueHandlerProxyMap[handlerName] = handler
}

func GetValueHandler(handlerName string) (ValueHandler, error) {
	if len(handlerName) == 0 {
		return nil, nil
	}
	handlerLocker.RLock()
	defer handlerLocker.RUnlock()
	if handler, ok := valueHandlerProxyMap[handlerName]; ok {
		return handler, nil
	}
	return nil, errors.New("未找到Handler:" + handlerName)
}

func GetAllValueHandlers() []string {
	handlerLocker.RLock()
	defer handlerLocker.RUnlock()
	res := make([]string, 0, len(valueHandlerProxyMap))
	for k := range valueHandlerProxyMap {
		res = append(res, k)
	}
	return res
}
