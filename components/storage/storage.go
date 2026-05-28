// Package storage 定义对象存储的统一抽象接口。
//
// 所有 storage 子包（cos/minio/s3/oss）都可以适配此接口，
// 上游只依赖 storage.ObjectStore 即可实现无感切换。
//
// 典型用法：
//
//	var store storage.ObjectStore = cos.NewAdapter(cosCli)
//	// 后续可以替换为 s3.NewAdapter(s3Cli) 而不改业务代码
//	store.Put(ctx, "key", data)
//	data, _ := store.Get(ctx, "key")
//
// @author wanlizhan
// @created 2026-04-24
package storage

import (
	"context"
	"time"
)

// ObjectStore 对象存储统一接口
type ObjectStore interface {
	// Put 上传对象
	Put(ctx context.Context, key string, data []byte) error
	// Get 下载对象
	Get(ctx context.Context, key string) ([]byte, error)
	// Delete 删除对象
	Delete(ctx context.Context, key string) error
	// Exists 检查对象是否存在
	Exists(ctx context.Context, key string) (bool, error)
	// List 列出指定前缀的对象
	List(ctx context.Context, prefix string, maxKeys int) ([]ObjectInfo, error)
	// SignURL 生成预签名下载 URL
	SignURL(ctx context.Context, key string, expires time.Duration) (string, error)
}

// ObjectInfo 对象信息
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}
