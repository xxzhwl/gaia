// Package operateProxy 包注释
// @author wanlizhan
// @created 2024/6/13
package operateProxy

import (
	"context"
	"github.com/pkg/errors"
	"sync"
)

type OperateModel interface {
	SetContext(ctx context.Context)
	SetDbSchema(dbSchema string) error
	Insert(table string, columns, extInfo map[string]any) (lastId int64, err error)
	Update(table string, columns, condition, extInfo map[string]any) (rows int64, err error)
	Delete(table string, condition, extInfo map[string]any) (rows int64, err error)
}

var operateProxyMap = map[string]OperateModel{}
var locker = sync.Mutex{}

func RegisterOperateModel(writerName string, writer OperateModel) {
	locker.Lock()
	defer locker.Unlock()
	if operateProxyMap == nil {
		operateProxyMap = make(map[string]OperateModel)
	}
	operateProxyMap[writerName] = writer
}

func GetOperateProxy(writerName string) (OperateModel, error) {
	locker.Lock()
	defer locker.Unlock()
	if writer, ok := operateProxyMap[writerName]; ok {
		return writer, nil
	}
	return nil, errors.New("未找到Writer:" + writerName)
}
