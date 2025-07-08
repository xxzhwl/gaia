// Package dao 包注释
// @author wanlizhan
// @created 2024-12-06
package dao

import (
	"fmt"
	"github.com/xxzhwl/gaia"
	"sync"
)

// Dao Dao对象 泛型
type Dao[T any] struct {
	dbSchema string
	table    string
	db       *gaia.Mysql
	daoLock  sync.RWMutex // 锁
}

// NewDaoInstance 生成Dao
func NewDaoInstance[T any](table, dbSchema string) *Dao[T] {
	dao := &Dao[T]{}
	dao.WithDbSchema(dbSchema)
	dao.WithTable(table)
	return dao
}

// newDaoInstanceWithDB 生成Dao
func newDaoInstanceWithDB[T any](table, dbSchema string, db *gaia.Mysql) *Dao[T] {
	dao := &Dao[T]{
		db: db,
	}
	dao.WithTable(table)
	dao.WithDbSchema(dbSchema)
	return dao
}

// WithDbSchema 配置DBScheme
func (t *Dao[T]) WithDbSchema(dbSchema string) *Dao[T] {
	t.dbSchema = dbSchema
	return t
}

// WithTable 配置表名
func (t *Dao[T]) WithTable(table string) *Dao[T] {
	t.table = table
	return t
}

func (t *Dao[T]) initDB() error {
	_, err := t.getDB()
	return err
}

// 获取数据库实例
func (t *Dao[T]) getDB() (*gaia.Mysql, error) {
	// 如果不是commonDao,db必然为空，则每次不同的dao都会实例化独有的db,如果是commonDao则只需要第一次实例化
	if t.db == nil {
		t.daoLock.Lock()
		defer t.daoLock.Unlock()
		if t.db != nil { // 双重检验
			return t.db, nil
		}
		if t.dbSchema == "" {
			return nil, fmt.Errorf("DBScheme:%s 为空", t.dbSchema)
		}
		db, err := gaia.NewMysqlWithSchema(t.dbSchema)
		if err != nil {
			return nil, err
		}
		t.db = db
	}
	return t.db, nil
}
