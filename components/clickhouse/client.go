// Package clickhouse 对 ClickHouse Go 驱动做轻量封装，提供列式分析数据库操作能力。
//
// 典型用法：
//
//	cli, _ := clickhouse.NewClient(clickhouse.Config{
//	    Addrs: []string{"127.0.0.1:9000"}, Database: "default",
//	})
//	rows, _ := cli.Query(ctx, "SELECT * FROM logs WHERE date = ?", today)
//	cli.Exec(ctx, "INSERT INTO logs VALUES (?, ?, ?)", args...)
//
// @author wanlizhan
// @created 2026-04-24
package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/xxzhwl/gaia"
)

// Config ClickHouse 配置
type Config struct {
	// Addrs 节点地址列表（如 "127.0.0.1:9000"）
	Addrs []string
	// Database 数据库名
	Database string
	// Username/Password 认证
	Username string
	Password string
	// DialTimeout 建连超时；0 默认 5 秒
	DialTimeout time.Duration
	// MaxOpenConns 最大打开连接数
	MaxOpenConns int
	// MaxIdleConns 最大空闲连接数
	MaxIdleConns int
	// Debug 是否开启调试日志
	Debug bool
}

// Client ClickHouse 客户端
type Client struct {
	conn clickhouse.Conn
	db   *sql.DB
	cfg  Config
}

// NewClient 创建 ClickHouse 原生连接客户端
func NewClient(cfg Config) (*Client, error) {
	if len(cfg.Addrs) == 0 {
		return nil, fmt.Errorf("Addrs 必填")
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = 5 * time.Second
	}

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: cfg.Addrs,
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		DialTimeout: cfg.DialTimeout,
		Debug:       cfg.Debug,
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		MaxOpenConns: cfg.MaxOpenConns,
		MaxIdleConns: cfg.MaxIdleConns,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 ClickHouse 连接失败: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("ClickHouse Ping 失败: %w", err)
	}

	return &Client{conn: conn, cfg: cfg}, nil
}

// NewClientWithSchema 从 gaia 配置中读取
func NewClientWithSchema(schema string) (*Client, error) {
	addrs := gaia.GetSafeConfStringSliceFromString(schema + ".Addrs")
	return NewClient(Config{
		Addrs:    addrs,
		Database: gaia.GetSafeConfString(schema + ".Database"),
		Username: gaia.GetSafeConfString(schema + ".Username"),
		Password: gaia.GetSafeConfString(schema + ".Password"),
	})
}

// NewFrameworkClient 使用 Framework.ClickHouse 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.ClickHouse")
}

// GetConn 返回底层原生连接
func (c *Client) GetConn() clickhouse.Conn {
	return c.conn
}

// Ping 探活
func (c *Client) Ping(ctx context.Context) error {
	return c.conn.Ping(ctx)
}

// Exec 执行 DDL/DML（INSERT/ALTER 等）
func (c *Client) Exec(ctx context.Context, query string, args ...any) error {
	return c.conn.Exec(ctx, query, args...)
}

// Query 查询
func (c *Client) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	return c.conn.Query(ctx, query, args...)
}

// QueryRow 查询单行
func (c *Client) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	return c.conn.QueryRow(ctx, query, args...)
}

// PrepareBatch 准备批量写入
func (c *Client) PrepareBatch(ctx context.Context, query string) (driver.Batch, error) {
	return c.conn.PrepareBatch(ctx, query)
}

// Close 关闭连接
func (c *Client) Close() error {
	return c.conn.Close()
}

// QueryAll 泛型查询：自动扫描行到结构体 slice
func QueryAll[T any](ctx context.Context, c *Client, query string, scanner func(driver.Row) (T, error), args ...any) ([]T, error) {
	rows, err := c.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []T
	for rows.Next() {
		item, err := scanner(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}
