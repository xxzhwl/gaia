// Package minio MinIO 适配 storage.ObjectStore 接口
// @author wanlizhan
// @created 2026-05-28
package minio

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"time"

	miniogo "github.com/minio/minio-go/v7"

	"github.com/xxzhwl/gaia/components/storage"
)

// adapter 将 *Client + 固定 bucket 适配为 storage.ObjectStore
type adapter struct {
	cli    *Client
	bucket string
}

// NewAdapter 返回符合 storage.ObjectStore 接口的适配器；bucket 为目标桶名
func NewAdapter(cli *Client, bucket string) storage.ObjectStore {
	return &adapter{cli: cli, bucket: bucket}
}

func (a *adapter) Put(ctx context.Context, key string, data []byte) error {
	reader := bytes.NewReader(data)
	_, err := a.cli.Cli.PutObject(ctx, a.bucket, key, reader, reader.Size(), miniogo.PutObjectOptions{})
	return err
}

func (a *adapter) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := a.cli.Cli.GetObject(ctx, a.bucket, key, miniogo.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

func (a *adapter) Delete(ctx context.Context, key string) error {
	return a.cli.Cli.RemoveObject(ctx, a.bucket, key, miniogo.RemoveObjectOptions{})
}

func (a *adapter) Exists(ctx context.Context, key string) (bool, error) {
	_, err := a.cli.Cli.StatObject(ctx, a.bucket, key, miniogo.StatObjectOptions{})
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (a *adapter) List(ctx context.Context, prefix string, maxKeys int) ([]storage.ObjectInfo, error) {
	out := make([]storage.ObjectInfo, 0)
	count := 0
	for obj := range a.cli.Cli.ListObjects(ctx, a.bucket, miniogo.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		out = append(out, storage.ObjectInfo{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified,
			ETag:         obj.ETag,
		})
		count++
		if maxKeys > 0 && count >= maxKeys {
			break
		}
	}
	return out, nil
}

func (a *adapter) SignURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	u, err := a.cli.Cli.PresignedGetObject(ctx, a.bucket, key, expires, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
