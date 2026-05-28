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

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/opentelemetry/tracing"
)

// PgConnsMaxLifeTime 设置连接可以被重用的最大时限
const PgConnsMaxLifeTime = time.Second * 20

// PgMaxIdleConns 设置连接池中允许存在的空闲连接数
const PgMaxIdleConns = 10

// PgMaxOpenConns 设置连接池最大允许打开的连接数
const PgMaxOpenConns = 100

// 最大重试次数
const _maxPgRetries = 3

// CloseAllPgConnections 关闭所有 PostgreSQL 数据库连接
func CloseAllPgConnections() error {
	pgDbLocker.Lock()
	defer pgDbLocker.Unlock()

	var lastErr error
	for dsn, pg := range pgDbConnPool {
		db, err := pg.db.DB()
		if err != nil {
			lastErr = err
			continue
		}
		if err := db.Close(); err != nil {
			lastErr = err
		}
		delete(pgDbConnPool, dsn)
	}
	return lastErr
}

var pgDbConnPool = make(map[string]*Postgresql)

var pgDbLocker sync.RWMutex

var pgDbLogger logger.Interface

// pgLoggerForDsn 若 pgDbLogger 实现了 dbLoggerCloner，则返回带 DSN 标签的克隆
//
// 复用 mysql.go 中定义的 dbLoggerCloner 接口（同一 package gaia）
func pgLoggerForDsn(dsn string) logger.Interface {
	if pgDbLogger == nil {
		return nil
	}
	if cloner, ok := pgDbLogger.(dbLoggerCloner); ok {
		return cloner.CloneWithDsn(dsn)
	}
	return pgDbLogger
}

// SetPgDbLogger 设置 PostgreSQL 数据库日志器
// 如果数据库连接已经存在，会更新现有连接的 logger（每个连接保留独立的 DSN 标签）
func SetPgDbLogger(newLogger logger.Interface) {
	pgDbLogger = newLogger

	pgDbLocker.Lock()
	defer pgDbLocker.Unlock()

	for dsn, pg := range pgDbConnPool {
		if pg.db != nil {
			lg := newLogger
			if cloner, ok := newLogger.(dbLoggerCloner); ok {
				lg = cloner.CloneWithDsn(dsn)
			}
			pg.db.Logger = lg.LogMode(logger.Info)
		}
	}
}

// Postgresql 封装 GORM PostgreSQL 数据库连接
type Postgresql struct {
	db *gorm.DB
}

// NewFrameworkPostgresql 创建框架默认 PostgreSQL 连接
// 从配置 "Framework.Postgresql" 读取 DSN
func NewFrameworkPostgresql() (*Postgresql, error) {
	db, err := NewPostgresqlWithDsn(GetSafeConfString("Framework.Postgresql"))
	if err != nil {
		return nil, err
	}
	return db, nil
}

// NewPostgresqlWithSchema 根据配置 schema 创建 PostgreSQL 连接
func NewPostgresqlWithSchema(schema string) (*Postgresql, error) {
	return NewPostgresqlWithDsn(GetSafeConfString(schema))
}

// NewPostgresqlWithDsn 根据 DSN 字符串创建 PostgreSQL 连接
func NewPostgresqlWithDsn(dsn string) (*Postgresql, error) {
	g := getPgDb(dsn)
	if g != nil {
		return g, nil
	}
	if err := genPgConn(dsn); err != nil {
		return nil, err
	}
	return getPgDb(dsn), nil
}

func genPgConn(dsn string) error {
	if len(dsn) == 0 {
		return errors.New("dsn is empty")
	}
	conf := &gorm.Config{}
	if lg := pgLoggerForDsn(dsn); lg != nil {
		conf.Logger = lg
	}

	var db *gorm.DB
	var err error
	for i := range _maxPgRetries {
		db, err = gorm.Open(postgres.Open(dsn), conf)
		if err == nil {
			break
		}
		if i < _maxPgRetries-1 {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	if err != nil {
		return fmt.Errorf("failed to connect to postgresql after %d retries: %w", _maxPgRetries, err)
	}

	if err = db.Use(tracing.NewPlugin()); err != nil {
		return err
	}
	dbConn, err := db.DB()
	if err != nil {
		return err
	}

	dbConn.SetConnMaxLifetime(PgConnsMaxLifeTime)
	dbConn.SetMaxIdleConns(PgMaxIdleConns)
	dbConn.SetMaxOpenConns(PgMaxOpenConns)

	dbInstance := db
	if GetSafeConfBool("Debug") {
		dbInstance = db.Debug()
	}
	setPgDb(dsn, &Postgresql{dbInstance})
	return nil
}

func getPgDb(dsn string) *Postgresql {
	pgDbLocker.RLock()
	defer pgDbLocker.RUnlock()
	if v, ok := pgDbConnPool[dsn]; ok {
		return v
	}
	return nil
}

func setPgDb(dsn string, db *Postgresql) {
	pgDbLocker.Lock()
	defer pgDbLocker.Unlock()
	pgDbConnPool[dsn] = db
}

// PostgresqlFetch 将 SQL 查询结果行转换为 map 切片
func PostgresqlFetch(rows *sql.Rows) ([]map[string]string, error) {
	result := make([]map[string]string, 0)
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	rawBytes := make([]sql.RawBytes, len(columns))

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

// GetGormDb 获取底层 GORM DB 实例
func (p *Postgresql) GetGormDb() *gorm.DB {
	return p.db
}

// ExecCommand 执行原始 SQL 命令并返回结果
func (p *Postgresql) ExecCommand(command string, args ...any) ([]map[string]string, error) {
	tx := p.db.Raw(command, args...)
	rows, err := tx.Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	if rows == nil {
		return nil, errors.New("rows nil")
	}

	defer rows.Close()

	return PostgresqlFetch(rows)
}
