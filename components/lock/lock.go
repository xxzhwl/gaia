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
// @updated 2026-05-28  并发安全 + Session 自动 Close 防泄漏 + TryLock 错误区分
package lock

import (
	"context"
	"errors"
	"fmt"
	"sync"
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

// etcdLockHolder 持有锁的资源（mutex + session），Unlock 时一并释放
type etcdLockHolder struct {
	mutex   *concurrency.Mutex
	session *concurrency.Session
}

// EtcdLocker 基于 ETCD Lease + concurrency 的分布式锁
type EtcdLocker struct {
	cli    *clientv3.Client
	prefix string

	mu      sync.Mutex
	holders map[string]*etcdLockHolder
}

// NewEtcdLocker 创建 ETCD 分布式锁
func NewEtcdLocker(cli *clientv3.Client, prefix string) *EtcdLocker {
	if prefix == "" {
		prefix = "/gaia/lock"
	}
	return &EtcdLocker{cli: cli, prefix: prefix, holders: make(map[string]*etcdLockHolder)}
}

// Lock 获取锁
func (e *EtcdLocker) Lock(ctx context.Context, key string, ttl time.Duration) error {
	session, err := concurrency.NewSession(e.cli, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		return fmt.Errorf("创建 etcd session 失败: %w", err)
	}

	mutex := concurrency.NewMutex(session, e.prefix+"/"+key)
	if err := mutex.Lock(ctx); err != nil {
		// 加锁失败必须立即关闭 session，否则 lease + keepalive goroutine 会泄漏
		_ = session.Close()
		return fmt.Errorf("etcd 加锁失败: %w", err)
	}

	e.mu.Lock()
	e.holders[key] = &etcdLockHolder{mutex: mutex, session: session}
	e.mu.Unlock()
	return nil
}

// TryLock 尝试获取锁（非阻塞）
//
// 返回值：
//   - (true,  nil)：成功获取锁
//   - (false, nil)：锁已被其他持有者占用（concurrency.ErrLocked）
//   - (false, err)：调用过程中发生其他错误（如网络/etcd 不可用）
func (e *EtcdLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	session, err := concurrency.NewSession(e.cli, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		return false, fmt.Errorf("创建 etcd session 失败: %w", err)
	}

	mutex := concurrency.NewMutex(session, e.prefix+"/"+key)
	if err := mutex.TryLock(ctx); err != nil {
		_ = session.Close()
		// 仅锁被占用时返回 false,nil，其它错误向上传递
		if errors.Is(err, concurrency.ErrLocked) {
			return false, nil
		}
		return false, err
	}

	e.mu.Lock()
	e.holders[key] = &etcdLockHolder{mutex: mutex, session: session}
	e.mu.Unlock()
	return true, nil
}

// Unlock 释放锁（同时关闭 session 释放 lease）
func (e *EtcdLocker) Unlock(ctx context.Context, key string) error {
	e.mu.Lock()
	holder, ok := e.holders[key]
	if ok {
		delete(e.holders, key)
	}
	e.mu.Unlock()

	if !ok {
		return fmt.Errorf("未持有锁: %s", key)
	}

	unlockErr := holder.mutex.Unlock(ctx)
	closeErr := holder.session.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
