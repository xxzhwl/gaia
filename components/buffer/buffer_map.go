// Package buffer 包注释
// @author wanlizhan
// @created 2024/6/25
package buffer

import (
	"fmt"
	"sync"
)

var bufferMap map[string]*DataBuffer

var bufferLock sync.RWMutex

func init() {
	bufferMap = make(map[string]*DataBuffer)
}

func registerInstance(name string, buffer *DataBuffer) error {
	bufferLock.Lock()
	defer bufferLock.Unlock()
	if _, ok := bufferMap[name]; ok {
		return fmt.Errorf("buffer with name %s already exists", name)
	}
	bufferMap[name] = buffer
	return nil
}

func getInstance(name string) *DataBuffer {
	bufferLock.RLock()
	defer bufferLock.RUnlock()
	if buffer, ok := bufferMap[name]; ok {
		return buffer
	}
	return nil
}
