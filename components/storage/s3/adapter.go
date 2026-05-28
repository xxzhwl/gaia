// Package s3 S3 适配 storage.ObjectStore 接口
// @author wanlizhan
// @created 2026-05-28
package s3

import (
	"context"
	"time"

	"github.com/xxzhwl/gaia/components/storage"
)

type adapter struct {
	cli *Client
}

// NewAdapter 返回符合 storage.ObjectStore 接口的适配器
func NewAdapter(cli *Client) storage.ObjectStore {
	return &adapter{cli: cli}
}

func (a *adapter) Put(ctx context.Context, key string, data []byte) error {
	return a.cli.PutObject(ctx, key, data)
}

func (a *adapter) Get(ctx context.Context, key string) ([]byte, error) {
	return a.cli.GetObject(ctx, key)
}

func (a *adapter) Delete(ctx context.Context, key string) error {
	return a.cli.DeleteObject(ctx, key)
}

func (a *adapter) Exists(ctx context.Context, key string) (bool, error) {
	_, err := a.cli.HeadObject(ctx, key)
	if err != nil {
		// 简化：只要 HeadObject 失败就视为不存在；调用方需要区分错误时请直接用 HeadObject。
		return false, nil
	}
	return true, nil
}

func (a *adapter) List(ctx context.Context, prefix string, maxKeys int) ([]storage.ObjectInfo, error) {
	out, err := a.cli.ListObjects(ctx, prefix, int32(maxKeys))
	if err != nil {
		return nil, err
	}
	res := make([]storage.ObjectInfo, 0, len(out.Contents))
	for _, o := range out.Contents {
		var lastModified time.Time
		if o.LastModified != nil {
			lastModified = *o.LastModified
		}
		var size int64
		if o.Size != nil {
			size = *o.Size
		}
		var etag string
		if o.ETag != nil {
			etag = *o.ETag
		}
		var key string
		if o.Key != nil {
			key = *o.Key
		}
		res = append(res, storage.ObjectInfo{
			Key:          key,
			Size:         size,
			LastModified: lastModified,
			ETag:         etag,
		})
	}
	return res, nil
}

func (a *adapter) SignURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return a.cli.GetPresignedURL(ctx, key, expires)
}
