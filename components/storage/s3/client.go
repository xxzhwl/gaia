// Package s3 对 AWS SDK v2 做轻量封装，提供 S3 对象存储操作能力。
//
// 典型用法：
//
//	cli, _ := s3.NewClient(s3.Config{
//	    Region: "us-east-1", Bucket: "my-bucket",
//	    AccessKeyID: "xxx", SecretAccessKey: "xxx",
//	})
//	cli.PutObject(ctx, "key", data)
//	data, _ := cli.GetObject(ctx, "key")
//
// @author wanlizhan
// @created 2026-04-24
package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/xxzhwl/gaia"
)

// Config S3 客户端配置
type Config struct {
	Region          string // AWS Region
	Bucket          string // 默认 Bucket
	AccessKeyID     string
	SecretAccessKey string
	Endpoint        string // 自定义端点（兼容 MinIO/Cloudflare R2 等 S3 兼容服务）
	UsePathStyle    bool   // 强制路径风格（自定义端点时通常需要）
}

// Client S3 客户端
type Client struct {
	cli    *s3.Client
	bucket string
	cfg    Config
}

// NewClient 创建 S3 客户端
func NewClient(cfg Config) (*Client, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("Region 必填")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("Bucket 必填")
	}

	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(cfg.Region))

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("加载 AWS 配置失败: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = cfg.UsePathStyle
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)
	return &Client{cli: client, bucket: cfg.Bucket, cfg: cfg}, nil
}

// NewClientWithSchema 从 gaia 配置中读取
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClient(Config{
		Region:          gaia.GetSafeConfString(schema + ".Region"),
		Bucket:          gaia.GetSafeConfString(schema + ".Bucket"),
		AccessKeyID:     gaia.GetSafeConfString(schema + ".AccessKeyID"),
		SecretAccessKey: gaia.GetSafeConfString(schema + ".SecretAccessKey"),
		Endpoint:        gaia.GetSafeConfString(schema + ".Endpoint"),
		UsePathStyle:    gaia.GetSafeConfBool(schema + ".UsePathStyle"),
	})
}

// NewFrameworkClient 使用 Framework.S3 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.S3")
}

// GetCli 返回底层 S3 客户端
func (c *Client) GetCli() *s3.Client {
	return c.cli
}

// PutObject 上传对象
func (c *Client) PutObject(ctx context.Context, key string, data []byte) error {
	_, err := c.cli.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	return err
}

// GetObject 下载对象
func (c *Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	out, err := c.cli.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// DeleteObject 删除对象
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.cli.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return err
}

// HeadObject 获取对象元数据
func (c *Client) HeadObject(ctx context.Context, key string) (*s3.HeadObjectOutput, error) {
	return c.cli.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
}

// ListObjects 列出对象
func (c *Client) ListObjects(ctx context.Context, prefix string, maxKeys int32) (*s3.ListObjectsV2Output, error) {
	return c.cli.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(maxKeys),
	})
}

// GetPresignedURL 获取预签名下载 URL
func (c *Client) GetPresignedURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	presigner := s3.NewPresignClient(c.cli)
	out, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

// CopyObject 复制对象
func (c *Client) CopyObject(ctx context.Context, srcKey, destKey string) error {
	source := fmt.Sprintf("%s/%s", c.bucket, srcKey)
	_, err := c.cli.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(c.bucket),
		CopySource: aws.String(source),
		Key:        aws.String(destKey),
	})
	return err
}
