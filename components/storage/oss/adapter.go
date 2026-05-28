// Package oss OSS 适配 storage.ObjectStore 接口
// @author wanlizhan
// @created 2026-05-28
package oss

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
	return a.cli.ObjectExists(ctx, key)
}

func (a *adapter) List(ctx context.Context, prefix string, maxKeys int) ([]storage.ObjectInfo, error) {
	objs, err := a.cli.ListObjects(ctx, prefix, maxKeys)
	if err != nil {
		return nil, err
	}
	out := make([]storage.ObjectInfo, 0, len(objs))
	for _, o := range objs {
		out = append(out, storage.ObjectInfo{
			Key:          o.Key,
			Size:         o.Size,
			LastModified: o.LastModified,
			ETag:         o.ETag,
		})
	}
	return out, nil
}

func (a *adapter) SignURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return a.cli.SignURL(ctx, key, expires)
}
