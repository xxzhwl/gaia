package account

import (
	"context"
	"errors"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	frameworkredis "github.com/xxzhwl/gaia/components/redis"
)

// Cache 缓存接口，用于缓存主体和权限数据。
type Cache interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	// Increment 原子递增 key 的值，首次创建时设置 TTL。返回递增后的值。
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// RedisCache 基于 Redis 实现的 Cache 接口。
type RedisCache struct {
	client *goredis.Client
	prefix string
	active bool
}

// NewRedisCache 使用给定的客户端和键前缀创建 RedisCache。
// 如果 client 为 nil，缓存以无操作模式运行。
// 如果 prefix 为空，默认使用 "acct:"。
func NewRedisCache(client *frameworkredis.Client, prefix string) *RedisCache {
	if prefix == "" {
		prefix = "acct:"
	}
	if client == nil {
		return &RedisCache{prefix: prefix}
	}
	return &RedisCache{client: client.GetCli(), prefix: prefix, active: true}
}

// key prepends the prefix to the given key if not already prefixed.
func (c *RedisCache) key(key string) string {
	if strings.HasPrefix(key, c.prefix) {
		return key
	}
	return c.prefix + key
}

// Get 检索指定键的值。命中时返回 (value, true, nil)，
// 未命中时返回 ("", false, nil)，出错时返回 ("", false, err)。
func (c *RedisCache) Get(ctx context.Context, key string) (string, bool, error) {
	if c == nil || !c.active {
		return "", false, nil
	}
	val, err := c.client.Get(ctx, c.key(key)).Result()
	if errors.Is(err, goredis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// Set 存储带 TTL 的值。
func (c *RedisCache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if c == nil || !c.active {
		return nil
	}
	return c.client.Set(ctx, c.key(key), value, ttl).Err()
}

// Increment 原子递增 key 的值，首次创建时设置 TTL。使用 Lua 脚本保证原子性。
func (c *RedisCache) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if c == nil || !c.active {
		return 0, nil
	}
	key = c.key(key)
	script := `
		local val = redis.call('INCR', KEYS[1])
		if val == 1 then
			redis.call('EXPIRE', KEYS[1], ARGV[1])
		end
		return val
	`
	val, err := c.client.Eval(ctx, script, []string{key}, int64(ttl.Seconds())).Int64()
	if err != nil {
		return 0, err
	}
	return val, nil
}

// Del 从缓存中删除指定键。
func (c *RedisCache) Del(ctx context.Context, key string) error {
	if c == nil || !c.active {
		return nil
	}
	return c.client.Del(ctx, c.key(key)).Err()
}

// Close 关闭底层 Redis 连接。
func (c *RedisCache) Close() error {
	if c == nil || !c.active {
		return nil
	}
	return c.client.Close()
}
