// Package milvus 对 Milvus Go SDK 做轻量封装，提供向量数据库操作能力。
//
// 典型用法：
//
//	cli, err := milvus.NewClient(milvus.Config{Address: "127.0.0.1:19530"})
//	has, err := cli.HasCollection(ctx, "documents")
//	results, err := cli.Search(ctx, milvus.SearchOption{
//	    CollectionName: "documents",
//	    VectorField:    "embedding",
//	    Vectors:        [][]float32{queryVector},
//	    TopK:           10,
//	})
//
// @author wanlizhan
// @created 2026-06-26
package milvus

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/xxzhwl/gaia"
)

const defaultConnectTimeout = 10 * time.Second

// Config Milvus 客户端配置。
type Config struct {
	// Address Milvus Proxy 地址；必填，例如 "127.0.0.1:19530" 或 "https://cluster.example.com"。
	Address string
	// Username/Password 认证信息。
	Username string
	Password string
	// DBName 默认数据库名；为空时使用 Milvus 默认数据库。
	DBName string
	// APIKey Zilliz Cloud / 托管 Milvus 的 API Key。
	APIKey string
	// EnableTLSAuth 是否启用 TLS；Address 使用 https:// 时 SDK 会自动启用。
	EnableTLSAuth bool
	// ConnectTimeout 建连和初次探活超时；0 默认 10 秒。
	ConnectTimeout time.Duration
}

// Client Milvus 客户端。
type Client struct {
	cli client.Client
	cfg Config
}

// SearchOption 向量检索参数。
type SearchOption struct {
	CollectionName string
	PartitionNames []string
	Expr           string
	OutputFields   []string
	VectorField    string
	Vectors        [][]float32
	MetricType     entity.MetricType
	TopK           int
	SearchParam    entity.SearchParam
	Options        []client.SearchQueryOptionFunc
}

// NewClient 创建 Milvus 客户端。
func NewClient(cfg Config) (*Client, error) {
	return NewClientWithContext(context.Background(), cfg)
}

// NewClientWithContext 使用指定 context 创建 Milvus 客户端。
func NewClientWithContext(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Address == "" {
		return nil, fmt.Errorf("Address 必填")
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = defaultConnectTimeout
	}

	connectCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	cli, err := client.NewClient(connectCtx, client.Config{
		Address:       cfg.Address,
		Username:      cfg.Username,
		Password:      cfg.Password,
		DBName:        cfg.DBName,
		APIKey:        cfg.APIKey,
		EnableTLSAuth: cfg.EnableTLSAuth,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 Milvus 客户端失败: %w", err)
	}

	c := &Client{cli: cli, cfg: cfg}
	if err := c.Ping(connectCtx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("Milvus Ping 失败: %w", err)
	}
	return c, nil
}

// NewClientWithSchema 从 gaia 配置中读取连接信息创建客户端。
// 支持的配置项：Address、Username、Password、DBName、APIKey、EnableTLSAuth、ConnectTimeoutSec。
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClient(ConfigFromSchema(schema))
}

// NewClientWithSchemaContext 使用指定 context 从 gaia 配置创建客户端。
func NewClientWithSchemaContext(ctx context.Context, schema string) (*Client, error) {
	return NewClientWithContext(ctx, ConfigFromSchema(schema))
}

// ConfigFromSchema 从 gaia 配置中读取 Milvus 配置。
func ConfigFromSchema(schema string) Config {
	cfg := Config{
		Address:       gaia.GetSafeConfString(schema + ".Address"),
		Username:      gaia.GetSafeConfString(schema + ".Username"),
		Password:      gaia.GetSafeConfString(schema + ".Password"),
		DBName:        gaia.GetSafeConfString(schema + ".DBName"),
		APIKey:        gaia.GetSafeConfString(schema + ".APIKey"),
		EnableTLSAuth: gaia.GetSafeConfBool(schema + ".EnableTLSAuth"),
	}
	if sec := gaia.GetSafeConfInt64(schema + ".ConnectTimeoutSec"); sec > 0 {
		cfg.ConnectTimeout = time.Duration(sec) * time.Second
	}
	return cfg
}

// NewFrameworkClient 使用 Framework.Milvus 配置创建客户端。
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.Milvus")
}

// NewFrameworkClientWithContext 使用指定 context 和 Framework.Milvus 配置创建客户端。
func NewFrameworkClientWithContext(ctx context.Context) (*Client, error) {
	return NewClientWithSchemaContext(ctx, "Framework.Milvus")
}

// GetCli 返回底层 Milvus SDK client.Client。
func (c *Client) GetCli() client.Client {
	return c.cli
}

// Ping 探活 Milvus 集群。
func (c *Client) Ping(ctx context.Context) error {
	state, err := c.cli.CheckHealth(ctx)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("Milvus health state 为空")
	}
	if !state.IsHealthy {
		reason := strings.Join(state.Reasons, "; ")
		if reason == "" {
			reason = "unknown"
		}
		return fmt.Errorf("Milvus 不健康: %s", reason)
	}
	return nil
}

// CheckHealth 返回 Milvus 集群健康状态。
func (c *Client) CheckHealth(ctx context.Context) (*entity.MilvusState, error) {
	return c.cli.CheckHealth(ctx)
}

// Version 返回 Milvus 服务端版本。
func (c *Client) Version(ctx context.Context) (string, error) {
	return c.cli.GetVersion(ctx)
}

// HasCollection 判断 Collection 是否存在。
func (c *Client) HasCollection(ctx context.Context, name string) (bool, error) {
	return c.cli.HasCollection(ctx, name)
}

// CreateCollection 创建 Collection。
func (c *Client) CreateCollection(ctx context.Context, schema *entity.Schema, shardsNum int32, opts ...client.CreateCollectionOption) error {
	return c.cli.CreateCollection(ctx, schema, shardsNum, opts...)
}

// DropCollection 删除 Collection。
func (c *Client) DropCollection(ctx context.Context, name string, opts ...client.DropCollectionOption) error {
	return c.cli.DropCollection(ctx, name, opts...)
}

// LoadCollection 将 Collection 加载到内存。
func (c *Client) LoadCollection(ctx context.Context, name string, async bool, opts ...client.LoadCollectionOption) error {
	return c.cli.LoadCollection(ctx, name, async, opts...)
}

// ReleaseCollection 释放 Collection。
func (c *Client) ReleaseCollection(ctx context.Context, name string, opts ...client.ReleaseCollectionOption) error {
	return c.cli.ReleaseCollection(ctx, name, opts...)
}

// CreateIndex 为指定字段创建索引。
func (c *Client) CreateIndex(ctx context.Context, collection, field string, idx entity.Index, async bool, opts ...client.IndexOption) error {
	return c.cli.CreateIndex(ctx, collection, field, idx, async, opts...)
}

// Insert 按列写入数据。
func (c *Client) Insert(ctx context.Context, collection, partition string, columns ...entity.Column) (entity.Column, error) {
	return c.cli.Insert(ctx, collection, partition, columns...)
}

// Upsert 按列写入或更新数据。
func (c *Client) Upsert(ctx context.Context, collection, partition string, columns ...entity.Column) (entity.Column, error) {
	return c.cli.Upsert(ctx, collection, partition, columns...)
}

// Flush 刷新 Collection。
func (c *Client) Flush(ctx context.Context, collection string, async bool, opts ...client.FlushOption) error {
	return c.cli.Flush(ctx, collection, async, opts...)
}

// Query 按表达式查询标量字段。
func (c *Client) Query(ctx context.Context, collection string, partitions []string, expr string, outputFields []string, opts ...client.SearchQueryOptionFunc) (client.ResultSet, error) {
	return c.cli.Query(ctx, collection, partitions, expr, outputFields, opts...)
}

// Search 执行 FloatVector 向量检索。
func (c *Client) Search(ctx context.Context, opt SearchOption) ([]client.SearchResult, error) {
	if opt.CollectionName == "" {
		return nil, fmt.Errorf("CollectionName 必填")
	}
	if opt.VectorField == "" {
		return nil, fmt.Errorf("VectorField 必填")
	}
	if len(opt.Vectors) == 0 {
		return nil, fmt.Errorf("Vectors 必填")
	}
	if opt.TopK <= 0 {
		return nil, fmt.Errorf("TopK 必须大于 0")
	}
	if opt.MetricType == "" {
		opt.MetricType = entity.L2
	}
	if opt.SearchParam == nil {
		sp, err := entity.NewIndexFlatSearchParam()
		if err != nil {
			return nil, err
		}
		opt.SearchParam = sp
	}

	vectors := make([]entity.Vector, 0, len(opt.Vectors))
	for _, vector := range opt.Vectors {
		vectors = append(vectors, entity.FloatVector(vector))
	}
	return c.cli.Search(
		ctx,
		opt.CollectionName,
		opt.PartitionNames,
		opt.Expr,
		opt.OutputFields,
		vectors,
		opt.VectorField,
		opt.MetricType,
		opt.TopK,
		opt.SearchParam,
		opt.Options...,
	)
}

// Close 关闭客户端。
func (c *Client) Close() error {
	return c.cli.Close()
}
