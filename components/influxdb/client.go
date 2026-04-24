// Package influxdb 对 InfluxDB Go 客户端做轻量封装，提供时序数据写入/查询能力。
//
// 典型用法：
//
//	cli, _ := influxdb.NewClient(influxdb.Config{
//	    URL: "http://localhost:8086", Token: "xxx",
//	    Org: "myorg", Bucket: "mybucket",
//	})
//	cli.WritePoint("cpu", map[string]string{"host": "server01"},
//	    map[string]any{"usage": 85.5}, time.Now())
//	result, _ := cli.Query(ctx, `from(bucket:"mybucket") |> range(start:-1h)`)
//
// @author wanlizhan
// @created 2026-04-24
package influxdb

import (
	"context"
	"fmt"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"github.com/xxzhwl/gaia"
)

// Config InfluxDB 配置
type Config struct {
	// URL InfluxDB 地址；必填
	URL string
	// Token 认证 Token；必填
	Token string
	// Org 组织名
	Org string
	// Bucket 默认 Bucket
	Bucket string
}

// Client InfluxDB 客户端
type Client struct {
	cli    influxdb2.Client
	writer api.WriteAPIBlocking
	query  api.QueryAPI
	cfg    Config
}

// NewClient 创建 InfluxDB 客户端
func NewClient(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("URL 必填")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("Token 必填")
	}

	cli := influxdb2.NewClient(cfg.URL, cfg.Token)

	// 探活
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ok, err := cli.Ping(ctx)
	if err != nil || !ok {
		cli.Close()
		return nil, fmt.Errorf("InfluxDB Ping 失败: %v", err)
	}

	return &Client{
		cli:    cli,
		writer: cli.WriteAPIBlocking(cfg.Org, cfg.Bucket),
		query:  cli.QueryAPI(cfg.Org),
		cfg:    cfg,
	}, nil
}

// NewClientWithSchema 从 gaia 配置中读取
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClient(Config{
		URL:    gaia.GetSafeConfString(schema + ".URL"),
		Token:  gaia.GetSafeConfString(schema + ".Token"),
		Org:    gaia.GetSafeConfString(schema + ".Org"),
		Bucket: gaia.GetSafeConfString(schema + ".Bucket"),
	})
}

// NewFrameworkClient 使用 Framework.InfluxDB 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.InfluxDB")
}

// GetCli 返回底层客户端
func (c *Client) GetCli() influxdb2.Client {
	return c.cli
}

// WritePoint 写入单个数据点
func (c *Client) WritePoint(ctx context.Context, measurement string, tags map[string]string, fields map[string]any, ts time.Time) error {
	p := influxdb2.NewPoint(measurement, tags, fields, ts)
	return c.writer.WritePoint(ctx, p)
}

// WritePoints 批量写入数据点
func (c *Client) WritePoints(ctx context.Context, points ...*write.Point) error {
	return c.writer.WritePoint(ctx, points...)
}

// Query 执行 Flux 查询
func (c *Client) Query(ctx context.Context, query string) (*api.QueryTableResult, error) {
	return c.query.Query(ctx, query)
}

// QueryRaw 执行原始查询返回 CSV 字符串
func (c *Client) QueryRaw(ctx context.Context, query string) (string, error) {
	return c.query.QueryRaw(ctx, query, influxdb2.DefaultDialect())
}

// Close 关闭客户端
func (c *Client) Close() {
	c.cli.Close()
}
