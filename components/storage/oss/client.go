// Package oss 对阿里云 OSS Go SDK 做轻量封装。
//
// 典型用法：
//
//	cli, _ := oss.NewClient(oss.Config{
//	    Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "my-bucket",
//	    AccessKeyID: "xxx", AccessKeySecret: "xxx",
//	})
//	cli.PutObject(ctx, "key", data)
//
// @author wanlizhan
// @created 2026-04-24
package oss

import (
	"bytes"
	"context"
	"fmt"
	"io"

	alioss "github.com/aliyun/aliyun-oss-go-sdk/oss"

	"github.com/xxzhwl/gaia"
)

// Config 阿里云 OSS 配置
type Config struct {
	Endpoint        string // OSS 端点（如 oss-cn-hangzhou.aliyuncs.com）
	Bucket          string // Bucket 名称
	AccessKeyID     string
	AccessKeySecret string
	UseInternalURL  bool // 是否使用内网地址
}

// Client OSS 客户端
type Client struct {
	ossClient *alioss.Client
	bucket    *alioss.Bucket
	cfg       Config
}

// NewClient 创建 OSS 客户端
func NewClient(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("Endpoint 必填")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("Bucket 必填")
	}

	var opts []alioss.ClientOption
	if cfg.UseInternalURL {
		// 仅在阿里云 ECS 内网环境使用
	}

	ossClient, err := alioss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.AccessKeySecret, opts...)
	if err != nil {
		return nil, fmt.Errorf("创建 OSS 客户端失败: %w", err)
	}

	bucket, err := ossClient.Bucket(cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("获取 Bucket 失败: %w", err)
	}

	return &Client{ossClient: ossClient, bucket: bucket, cfg: cfg}, nil
}

// NewClientWithSchema 从 gaia 配置中读取
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClient(Config{
		Endpoint:        gaia.GetSafeConfString(schema + ".Endpoint"),
		Bucket:          gaia.GetSafeConfString(schema + ".Bucket"),
		AccessKeyID:     gaia.GetSafeConfString(schema + ".AccessKeyID"),
		AccessKeySecret: gaia.GetSafeConfString(schema + ".AccessKeySecret"),
		UseInternalURL:  gaia.GetSafeConfBool(schema + ".UseInternalURL"),
	})
}

// NewFrameworkClient 使用 Framework.OSS 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.OSS")
}

// GetBucket 返回底层 Bucket
func (c *Client) GetBucket() *alioss.Bucket {
	return c.bucket
}

// PutObject 上传对象
func (c *Client) PutObject(_ context.Context, key string, data []byte) error {
	return c.bucket.PutObject(key, bytes.NewReader(data))
}

// PutObjectFromFile 从文件上传
func (c *Client) PutObjectFromFile(_ context.Context, key, filePath string) error {
	return c.bucket.PutObjectFromFile(key, filePath)
}

// GetObject 下载对象
func (c *Client) GetObject(_ context.Context, key string) ([]byte, error) {
	body, err := c.bucket.GetObject(key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(body)
}

// GetObjectToFile 下载对象到文件
func (c *Client) GetObjectToFile(_ context.Context, key, filePath string) error {
	return c.bucket.GetObjectToFile(key, filePath)
}

// DeleteObject 删除对象
func (c *Client) DeleteObject(_ context.Context, key string) error {
	return c.bucket.DeleteObject(key)
}

// DeleteObjects 批量删除对象
func (c *Client) DeleteObjects(_ context.Context, keys []string) error {
	_, err := c.bucket.DeleteObjects(keys)
	return err
}

// ObjectExists 检查对象是否存在
func (c *Client) ObjectExists(_ context.Context, key string) (bool, error) {
	return c.bucket.IsObjectExist(key)
}

// ListObjects 列出对象
func (c *Client) ListObjects(_ context.Context, prefix string, maxKeys int) ([]alioss.ObjectProperties, error) {
	result, err := c.bucket.ListObjects(alioss.Prefix(prefix), alioss.MaxKeys(maxKeys))
	if err != nil {
		return nil, err
	}
	return result.Objects, nil
}

// CopyObject 复制对象
func (c *Client) CopyObject(_ context.Context, srcKey, destKey string) error {
	_, err := c.bucket.CopyObject(srcKey, destKey)
	return err
}

// SignURL 生成签名 URL
func (c *Client) SignURL(_ context.Context, key string, expiredInSec int64) (string, error) {
	return c.bucket.SignURL(key, alioss.HTTPGet, expiredInSec)
}
