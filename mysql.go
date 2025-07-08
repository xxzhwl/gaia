// Package gaia 注释
// @author wanlizhan
// @created 2024/4/29
package gaia

import (
	"database/sql"
	"errors"
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
	"sync"
	"time"
)

// ConnsMaxLifeTime 设置连接可以被重用的最大时限
// 要注意，这个值的设置应该比直接连接端的超时时间短，
// 或者mysql的wait_timeout参数(如果是使用直连mysql)
// 如果连接的server端已经回收了连接，而连接池却还在可用的生命周期状态，将会出现invalid connections错误，这是tcp的一个半连接状态导致的。
const ConnsMaxLifeTime = time.Second * 20

// MaxIdleConns 设置连接池中允许存在的空闲连接数
const MaxIdleConns = 2

// MaxOpenConns 设置连接池最大允许打开的连接数
const MaxOpenConns = 2000

// 最大重试次数
const _maxMySqlRetries = 3

var dbConnPool = make(map[string]*Mysql)

var dbLocker sync.RWMutex

var dbLogger logger.Interface

func SetDbLogger(logger logger.Interface) {
	dbLogger = logger
}

type Mysql struct {
	db *gorm.DB
}

func NewFrameworkMysql() (*Mysql, error) {
	db, err := NewMySQLWithDsn(GetSafeConfString("Framework.Mysql"))
	if err != nil {
		return nil, err
	}
	return db, nil
}

func NewMysqlWithSchema(schema string) (*Mysql, error) {
	return NewMySQLWithDsn(GetSafeConfString(schema))
}

func NewMySQLWithDsn(dsn string) (*Mysql, error) {
	g := getDb(dsn)
	if g != nil {
		return g, nil
	}
	if err := genConn(dsn); err != nil {
		return nil, err
	}
	return getDb(dsn), nil
}

func genConn(dsn string) error {
	if len(dsn) == 0 {
		return errors.New("dsn is empty")
	}
	conf := &gorm.Config{}
	if dbLogger != nil {
		conf.Logger = dbLogger
	}
	db, err := gorm.Open(mysql.Open(dsn), conf)
	if err != nil {
		return err
	}
	if err = db.Use(tracing.NewPlugin()); err != nil {
		return err
	}
	dbConn, err := db.DB()
	if err != nil {
		return err
	}

	dbConn.SetConnMaxLifetime(ConnsMaxLifeTime)
	dbConn.SetMaxIdleConns(MaxIdleConns)
	dbConn.SetMaxOpenConns(MaxOpenConns)

	setDb(dsn, &Mysql{db.Debug()})
	return nil
}

func getDb(dsn string) *Mysql {
	dbLocker.Lock()
	defer dbLocker.Unlock()
	if v, ok := dbConnPool[dsn]; ok {
		return v
	}
	return nil
}

func setDb(dsn string, db *Mysql) {
	dbLocker.Lock()
	defer dbLocker.Unlock()
	dbConnPool[dsn] = db
}

func MysqlFetch(rows *sql.Rows) ([]map[string]string, error) {
	result := make([]map[string]string, 0)
	columns, err := rows.Columns()
	if err != nil {
		//an error occurred
		return nil, err
	}

	rawBytes := make([]sql.RawBytes, len(columns))

	//rows.Scan wants '[]any' as an argument, so we must copy
	//the references into such a slice
	scanArgs := make([]any, len(columns))

	for i := range rawBytes {
		scanArgs[i] = &rawBytes[i]
	}

	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			return nil, err
		}
		var val string
		item := make(map[string]string)
		for i, col := range rawBytes {
			if col == nil {
				val = ""
			} else {
				val = string(col)
			}
			item[columns[i]] = val
		}
		result = append(result, item)
	}
	return result, nil
}

func (m *Mysql) GetGormDb() *gorm.DB {
	return m.db
}

func (m *Mysql) ExecCommand(command string, args ...any) ([]map[string]string, error) {
	tx := m.db.Raw(fmt.Sprintf(command, args...))
	rows, err := tx.Rows()
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, errors.New("rows nil")
	}

	defer rows.Close()

	return MysqlFetch(rows)
}
