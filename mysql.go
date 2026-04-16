// Package gaia 注释
// @author wanlizhan
// @created 2024/4/29
package gaia

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

// ConnsMaxLifeTime 设置连接可以被重用的最大时限
// 要注意，这个值的设置应该比直接连接端的超时时间短，
// 或者mysql的wait_timeout参数(如果是使用直连mysql)
// 如果连接的server端已经回收了连接，而连接池却还在可用的生命周期状态，将会出现invalid connections错误，这是tcp的一个半连接状态导致的。
const ConnsMaxLifeTime = time.Second * 20

// MaxIdleConns 设置连接池中允许存在的空闲连接数
// 适当增加可以减少频繁创建连接的开销
const MaxIdleConns = 10

// MaxOpenConns 设置连接池最大允许打开的连接数
// 建议根据实际需求调整，一般根据数据库服务器性能和并发量设置
const MaxOpenConns = 100

// 最大重试次数
const _maxMySqlRetries = 3

// CloseAllConnections 关闭所有数据库连接
// 在应用程序关闭时调用此方法清理资源
func CloseAllConnections() error {
	dbLocker.Lock()
	defer dbLocker.Unlock()

	var lastErr error
	for dsn, mysql := range dbConnPool {
		db, err := mysql.db.DB()
		if err != nil {
			lastErr = err
			continue
		}
		if err := db.Close(); err != nil {
			lastErr = err
		}
		delete(dbConnPool, dsn)
	}
	return lastErr
}

var dbConnPool = make(map[string]*Mysql)

var dbLocker sync.RWMutex

var dbLogger logger.Interface

// SetDbLogger 设置数据库日志器
// 如果数据库连接已经存在，会更新现有连接的 logger
func SetDbLogger(newLogger logger.Interface) {
	dbLogger = newLogger

	// 更新已存在的数据库连接的 logger
	dbLocker.Lock()
	defer dbLocker.Unlock()

	for _, mysql := range dbConnPool {
		if mysql.db != nil {
			// 使用 LogMode 获取当前日志级别对应的 logger 实例
			// 默认使用 Info 级别，后续 GORM 会根据需要调整
			mysql.db.Logger = newLogger.LogMode(logger.Info)
		}
	}
}

// Mysql 封装GORM数据库连接
type Mysql struct {
	db *gorm.DB
}

// NewFrameworkMysql 创建框架默认MySQL连接
func NewFrameworkMysql() (*Mysql, error) {
	db, err := NewMySQLWithDsn(GetSafeConfString("Framework.Mysql"))
	if err != nil {
		return nil, err
	}
	return db, nil
}

// NewMysqlWithSchema 根据配置schema创建MySQL连接
func NewMysqlWithSchema(schema string) (*Mysql, error) {
	return NewMySQLWithDsn(GetSafeConfString(schema))
}

// NewMySQLWithDsn 根据DSN字符串创建MySQL连接
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

	// 添加重试机制
	var db *gorm.DB
	var err error
	for i := 0; i < _maxMySqlRetries; i++ {
		db, err = gorm.Open(mysql.Open(dsn), conf)
		if err == nil {
			break
		}
		if i < _maxMySqlRetries-1 {
			time.Sleep(time.Second * time.Duration(i+1)) // 指数退避重试
		}
	}
	if err != nil {
		return fmt.Errorf("failed to connect to database after %d retries: %w", _maxMySqlRetries, err)
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

	// 根据环境决定是否启用调试模式
	dbInstance := db
	if GetSafeConfBool("Debug") {
		dbInstance = db.Debug()
	}
	setDb(dsn, &Mysql{dbInstance})
	return nil
}

func getDb(dsn string) *Mysql {
	dbLocker.RLock() // 使用读锁替代写锁，提高并发性能
	defer dbLocker.RUnlock()
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

// MysqlFetch 将SQL查询结果行转换为map切片
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

// GetGormDb 获取底层GORM DB实例
func (m *Mysql) GetGormDb() *gorm.DB {
	return m.db
}

// ExecCommand 执行原始SQL命令并返回结果
func (m *Mysql) ExecCommand(command string, args ...any) ([]map[string]string, error) {
	// 使用GORM的参数化查询避免SQL注入
	tx := m.db.Raw(command, args...)
	rows, err := tx.Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	if rows == nil {
		return nil, errors.New("rows nil")
	}

	defer rows.Close()

	return MysqlFetch(rows)
}
