// Package mongo 对 MongoDB 官方 Go Driver 做轻量封装，提供文档数据库操作能力。
//
// 典型用法：
//
//	cli, err := mongo.NewClient(mongo.Config{URI: "mongodb://localhost:27017", Database: "mydb"})
//	coll := cli.Collection("users")
//	_, err = coll.InsertOne(ctx, bson.M{"name": "alice"})
//
// @author wanlizhan
// @created 2026-04-24
package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"

	"github.com/xxzhwl/gaia"
)

// Config MongoDB 客户端配置
type Config struct {
	// URI MongoDB 连接字符串；必填
	// 例如 "mongodb://localhost:27017" 或 "mongodb+srv://user:pass@cluster.example.com"
	URI string
	// Database 默认数据库名；必填
	Database string
	// ConnectTimeout 建连超时；0 默认 10 秒
	ConnectTimeout time.Duration
	// MaxPoolSize 连接池最大连接数；0 则使用驱动默认值 100
	MaxPoolSize uint64
	// MinPoolSize 连接池最小连接数；0 则使用驱动默认值 0
	MinPoolSize uint64
}

// Client MongoDB 客户端
type Client struct {
	cli *mongo.Client
	db  *mongo.Database
	cfg Config
}

// NewClient 创建 MongoDB 客户端
func NewClient(cfg Config) (*Client, error) {
	if cfg.URI == "" {
		return nil, fmt.Errorf("URI 必填")
	}
	if cfg.Database == "" {
		return nil, fmt.Errorf("Database 必填")
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}

	opts := options.Client().
		ApplyURI(cfg.URI).
		SetConnectTimeout(cfg.ConnectTimeout)

	if cfg.MaxPoolSize > 0 {
		opts.SetMaxPoolSize(cfg.MaxPoolSize)
	}
	if cfg.MinPoolSize > 0 {
		opts.SetMinPoolSize(cfg.MinPoolSize)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("创建 MongoDB 客户端失败: %w", err)
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("MongoDB Ping 失败: %w", err)
	}

	return &Client{
		cli: client,
		db:  client.Database(cfg.Database),
		cfg: cfg,
	}, nil
}

// NewClientWithSchema 从 gaia 配置中读取连接信息创建客户端
func NewClientWithSchema(schema string) (*Client, error) {
	uri := gaia.GetSafeConfString(schema + ".URI")
	database := gaia.GetSafeConfString(schema + ".Database")
	return NewClient(Config{URI: uri, Database: database})
}

// NewFrameworkClient 使用 Framework.Mongo 配置创建客户端
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.Mongo")
}

// GetCli 返回底层 mongo.Client
func (c *Client) GetCli() *mongo.Client {
	return c.cli
}

// GetDB 返回默认数据库
func (c *Client) GetDB() *mongo.Database {
	return c.db
}

// Database 切换数据库
func (c *Client) Database(name string) *mongo.Database {
	return c.cli.Database(name)
}

// Collection 获取默认数据库下的集合
func (c *Client) Collection(name string) *mongo.Collection {
	return c.db.Collection(name)
}

// InsertOne 插入单个文档
func (c *Client) InsertOne(ctx context.Context, collection string, doc any) (*mongo.InsertOneResult, error) {
	return c.db.Collection(collection).InsertOne(ctx, doc)
}

// InsertMany 批量插入文档
func (c *Client) InsertMany(ctx context.Context, collection string, docs []any) (*mongo.InsertManyResult, error) {
	return c.db.Collection(collection).InsertMany(ctx, docs)
}

// FindOne 查询单个文档
func (c *Client) FindOne(ctx context.Context, collection string, filter any, result any) error {
	return c.db.Collection(collection).FindOne(ctx, filter).Decode(result)
}

// Find 查询多个文档
func (c *Client) Find(ctx context.Context, collection string, filter any, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	return c.db.Collection(collection).Find(ctx, filter, opts...)
}

// FindAll 查询多个文档并解码到 slice
func FindAll[T any](ctx context.Context, c *Client, collection string, filter any, opts ...*options.FindOptions) ([]T, error) {
	cursor, err := c.db.Collection(collection).Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []T
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// UpdateOne 更新单个文档
func (c *Client) UpdateOne(ctx context.Context, collection string, filter, update any) (*mongo.UpdateResult, error) {
	return c.db.Collection(collection).UpdateOne(ctx, filter, update)
}

// UpdateMany 批量更新文档
func (c *Client) UpdateMany(ctx context.Context, collection string, filter, update any) (*mongo.UpdateResult, error) {
	return c.db.Collection(collection).UpdateMany(ctx, filter, update)
}

// DeleteOne 删除单个文档
func (c *Client) DeleteOne(ctx context.Context, collection string, filter any) (*mongo.DeleteResult, error) {
	return c.db.Collection(collection).DeleteOne(ctx, filter)
}

// DeleteMany 批量删除文档
func (c *Client) DeleteMany(ctx context.Context, collection string, filter any) (*mongo.DeleteResult, error) {
	return c.db.Collection(collection).DeleteMany(ctx, filter)
}

// CountDocuments 统计文档数量
func (c *Client) CountDocuments(ctx context.Context, collection string, filter any) (int64, error) {
	return c.db.Collection(collection).CountDocuments(ctx, filter)
}

// Aggregate 聚合查询
func (c *Client) Aggregate(ctx context.Context, collection string, pipeline any) (*mongo.Cursor, error) {
	return c.db.Collection(collection).Aggregate(ctx, pipeline)
}

// CreateIndex 创建索引
func (c *Client) CreateIndex(ctx context.Context, collection string, keys bson.D, opts ...*options.IndexOptions) (string, error) {
	model := mongo.IndexModel{Keys: keys}
	if len(opts) > 0 {
		model.Options = opts[0]
	}
	return c.db.Collection(collection).Indexes().CreateOne(ctx, model)
}

// Close 关闭客户端
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.cli.Disconnect(ctx)
}
