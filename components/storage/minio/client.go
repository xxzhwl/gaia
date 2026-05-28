// Package minio MinIO/S3 兼容对象存储封装
// @author wanlizhan
// @created 2024/7/10
// @updated 2026-05-28  实现真正的分片上传 + 暴露 Secure(HTTPS) 配置
package minio

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/xxzhwl/gaia"
)

// 默认分片大小：5MiB（MinIO 协议要求最小 5MiB，最后一片除外）
const defaultPartSize = 5 * 1024 * 1024

// Client MinIO 客户端封装
type Client struct {
	Cli  *minio.Client
	Core *minio.Core
}

// NewClient 创建 MinIO 客户端，使用配置项中的 Secure 字段决定是否启用 HTTPS
func NewClient(schema string) (*Client, error) {
	ept := gaia.GetSafeConfString("Framework." + schema + ".Endpoint")
	un := gaia.GetSafeConfString("Framework." + schema + ".UserName")
	pwd := gaia.GetSafeConfString("Framework." + schema + ".Password")
	secure := gaia.GetSafeConfBool("Framework." + schema + ".Secure")

	options := &minio.Options{
		Creds:  credentials.NewStaticV4(un, pwd, ""),
		Secure: secure,
	}
	client, err := minio.New(ept, options)
	if err != nil {
		return nil, err
	}

	core, err := minio.NewCore(ept, options)
	if err != nil {
		return nil, err
	}
	return &Client{Cli: client, Core: core}, nil
}

// PutObject 普通上传
func (c *Client) PutObject(bucket string, fileName string, data []byte) error {
	reader := bytes.NewReader(data)
	_, err := c.Cli.PutObject(context.Background(), bucket, fileName, reader, reader.Size(), minio.PutObjectOptions{})
	return err
}

// PutMultipartObject 多分片上传：< 5MiB 时直接 PutObject，否则按 5MiB 切分上传并 Complete。
//
// 流程：
//  1. NewMultipartUpload 申请 uploadID
//  2. 循环 PutObjectPart 上传每一片，记录 ETag
//  3. CompleteMultipartUpload 合并；任何中间步骤失败立即 AbortMultipartUpload 释放资源
func (c *Client) PutMultipartObject(bucket, fileName string, data []byte) error {
	if int64(len(data)) <= defaultPartSize {
		return c.PutObject(bucket, fileName, data)
	}
	return c.PutMultipartObjectFromReader(bucket, fileName, bytes.NewReader(data), int64(len(data)), defaultPartSize)
}

// PutMultipartObjectFromReader 流式分片上传
//   - totalSize <= 0 时表示总大小未知（流式），按 partSize 持续读到 EOF
//   - partSize 必须 >= 5MiB（最后一片除外）
func (c *Client) PutMultipartObjectFromReader(bucket, fileName string, reader io.Reader, totalSize, partSize int64) error {
	if partSize < defaultPartSize {
		partSize = defaultPartSize
	}
	ctx := context.Background()

	uploadID, err := c.Core.NewMultipartUpload(ctx, bucket, fileName, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("初始化分片上传失败: %w", err)
	}

	abort := func(cause error) error {
		_ = c.Core.AbortMultipartUpload(ctx, bucket, fileName, uploadID)
		return cause
	}

	var parts []minio.CompletePart
	partNumber := 1
	for {
		buffer := make([]byte, partSize)
		n, readErr := io.ReadFull(reader, buffer)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return abort(fmt.Errorf("读取数据失败: %w", readErr))
		}

		objectPart, putErr := c.Core.PutObjectPart(
			ctx, bucket, fileName, uploadID, partNumber,
			bytes.NewReader(buffer[:n]), int64(n),
			minio.PutObjectPartOptions{},
		)
		if putErr != nil {
			return abort(fmt.Errorf("上传分片[%d]失败: %w", partNumber, putErr))
		}
		parts = append(parts, minio.CompletePart{
			PartNumber: partNumber,
			ETag:       objectPart.ETag,
		})
		partNumber++

		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}

	if len(parts) == 0 {
		// reader 为空：中止 upload，按空对象处理
		_ = c.Core.AbortMultipartUpload(ctx, bucket, fileName, uploadID)
		return c.PutObject(bucket, fileName, nil)
	}

	if _, err := c.Core.CompleteMultipartUpload(ctx, bucket, fileName, uploadID, parts, minio.PutObjectOptions{}); err != nil {
		return abort(fmt.Errorf("合并分片失败: %w", err))
	}
	return nil
}