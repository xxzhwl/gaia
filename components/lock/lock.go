// Package lock 提供分布式锁抽象接口及 ETCD 实现。
//
// Redis 分布式锁已内置在 redis 组件中（Client.Lock / Client.UnLock / Client.TryLock / Client.LockCtx），
// 本包提供接口抽象 + ETCD 后端实现。
//
// 典型用法：
//
//	// ETCD 锁
//	locker := lock.NewEtcdLocker(etcdCli, "/app/lock")
//	err := locker.Lock(ctx, "my-lock", 30*time.Second)
//	defer locker.Unlock(ctx, "my-lock")
//
//	// Redis 锁直接用 redis.Client：
//	//   lockVal, _ := redisCli.Lock("my-lock", 30*time.Second, 5*time.Second)
//	//   defer redisCli.UnLock("my-lock", lockVal)
//
// @author wanlizhan
// @created 2026-04-24
package lock

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Locker 分布式锁接口
type Locker interface {
	// Lock 获取锁；ttl 为锁的最大持有时间（防死锁）
	Lock(ctx context.Context, key string, ttl time.Duration) error
	// Unlock 释放锁
	Unlock(ctx context.Context, key string) error
	// TryLock 尝试获取锁（非阻塞）
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// ================================ ETCD 实现 ================================

// EtcdLocker 基于 ETCD Lease + concurrency 的分布式锁
type EtcdLocker struct {
	cli    *clientv3.Client
	prefix string
	mutexs map[string]*concurrency.Mutex
}

// NewEtcdLocker 创建 ETCD 分布式锁
func NewEtcdLocker(cli *clientv3.Client, prefix string) *EtcdLocker {
	if prefix == "" {
		prefix = "/gaia/lock"
	}
	return &EtcdLocker{cli: cli, prefix: prefix, mutexs: make(map[string]*concurrency.Mutex)}
}

// Lock 获取锁
func (e *EtcdLocker) Lock(ctx context.Context, key string, ttl time.Duration) error {
	session, err := concurrency.NewSession(e.cli, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		return fmt.Errorf("创建 etcd session 失败: %w", err)
	}

	mutex := concurrency.NewMutex(session, e.prefix+"/"+key)
	if err := mutex.Lock(ctx); err != nil {
		return fmt.Errorf("etcd 加锁失败: %w", err)
	}
	e.mutexs[key] = mutex
	return nil
}

// TryLock 尝试获取锁（非阻塞）
func (e *EtcdLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	session, err := concurrency.NewSession(e.cli, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		return false, err
	}

	mutex := concurrency.NewMutex(session, e.prefix+"/"+key)
	if err := mutex.TryLock(ctx); err != nil {
		return false, nil
	}
	e.mutexs[key] = mutex
	return true, nil
}

// Unlock 释放锁
func (e *EtcdLocker) Unlock(ctx context.Context, key string) error {
	mutex, ok := e.mutexs[key]
	if !ok {
		return fmt.Errorf("未持有锁: %s", key)
	}
	defer delete(e.mutexs, key)
	return mutex.Unlock(ctx)
}
