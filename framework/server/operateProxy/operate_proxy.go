// Package operateProxy 包注释
// @author wanlizhan
// @created 2024/6/13
package operateProxy

import (
	"context"
	"sync"

	"github.com/pkg/errors"
)

type OperateModel interface {
	SetContext(ctx context.Context)
	SetDbSchema(dbSchema string) error
	Insert(table string, columns, extInfo map[string]any) (lastId int64, err error)
	Update(table string, columns, condition, extInfo map[string]any) (rows int64, err error)
	Delete(table string, condition, extInfo map[string]any) (rows int64, err error)
}

// OperateModelFactory 工厂方法：每次调用返回一个全新的 OperateModel 实例，
// 用于规避并发请求共用同一个 OperateModel 实例时 SetContext/SetDbSchema 互相覆盖的问题。
type OperateModelFactory func() OperateModel

var (
	operateProxyMap   = map[string]OperateModel{}
	operateFactoryMap = map[string]OperateModelFactory{}
	locker            = sync.RWMutex{}
)

// RegisterOperateModel 兼容旧用法：注册 OperateModel 单例。
// 注意：单例方式在并发请求下不安全（SetContext/SetDbSchema 会被并发覆盖），
// 建议使用 RegisterOperateModelFactory 注册工厂方法。
func RegisterOperateModel(writerName string, writer OperateModel) {
	locker.Lock()
	defer locker.Unlock()
	if operateProxyMap == nil {
		operateProxyMap = make(map[string]OperateModel)
	}
	operateProxyMap[writerName] = writer
}

// RegisterOperateModelFactory 注册工厂：每次 GetOperateProxy 都会调用工厂创建新实例。
// 推荐用法：解决并发请求共用单例时的状态覆盖问题。
func RegisterOperateModelFactory(writerName string, factory OperateModelFactory) {
	locker.Lock()
	defer locker.Unlock()
	if operateFactoryMap == nil {
		operateFactoryMap = make(map[string]OperateModelFactory)
	}
	operateFactoryMap[writerName] = factory
}

// GetOperateProxy 优先使用工厂创建新实例，回退到注册的单例。
func GetOperateProxy(writerName string) (OperateModel, error) {
	locker.RLock()
	factory, hasFactory := operateFactoryMap[writerName]
	writer, hasSingleton := operateProxyMap[writerName]
	locker.RUnlock()

	if hasFactory && factory != nil {
		return factory(), nil
	}
	if hasSingleton {
		return writer, nil
	}
	return nil, errors.New("未找到Writer:" + writerName)
}
