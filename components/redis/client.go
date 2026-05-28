// Package redis 注释
// @author wanlizhan
// @created 2024/5/21
package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

type Client struct {
	c        *redis.Client
	logger   *logImpl.DefaultLogger
	ctx      context.Context
	lockVals *sync.Map // key -> lockValue，用于 UnLockByKey；NewClient 时初始化
}

func NewFrameworkClient() *Client {
	return NewClientWithSchema("Framework.Redis")
}

func NewClientWithSchema(schema string) *Client {
	addrKey := fmt.Sprintf("%s.Address", schema)
	userNameKey := fmt.Sprintf("%s.UserName", schema)
	pwdKey := fmt.Sprintf("%s.Password", schema)

	addr := gaia.GetSafeConfString(addrKey)
	userName := gaia.GetSafeConfString(userNameKey)
	pwd := gaia.GetSafeConfString(pwdKey)
	client := NewClient(addr, userName, pwd)
	client.logger = logImpl.NewDefaultLogger().SetTitle(schema + "_redis")
	return client
}

func NewClient(addr, username, password string) *Client {
	trueClient := redis.NewClient(&redis.Options{
		Addr:     addr,
		Username: username,
		Password: password,
	})

	// 启用 tracing
	if err := redisotel.InstrumentTracing(trueClient); err != nil {
		gaia.ErrorF("Error creating redis client: %s", err.Error())
	}

	// 启用 metrics
	if err := redisotel.InstrumentMetrics(trueClient); err != nil {
		gaia.ErrorF("Error creating redis client: %s", err.Error())
	}

	return &Client{c: trueClient, logger: logImpl.NewDefaultLogger().SetTitle("redis"), ctx: context.Background(), lockVals: &sync.Map{}}
}

func (c *Client) SetLogTitle(title string) *Client {
	c.logger = logImpl.NewDefaultLogger().SetTitle(title)
	return c
}

// WithCtx 返回一个绑定指定 context 的 Client 浅拷贝。
// 共享底层连接池，但 ctx 状态隔离，并发安全。
func (c *Client) WithCtx(ctx context.Context) *Client {
	if ctx == nil {
		ctx = context.Background()
	}
	nc := *c
	nc.ctx = ctx
	return &nc
}

// GetCli 返回底层 *redis.Client（共享连接池）
func (c *Client) GetCli() *redis.Client {
	return c.c
}

func (c *Client) emitCacheLog(op, key string, hit *bool, ttl time.Duration, bodySize int, start time.Time, err error) {
	if c.logger == nil {
		return
	}
	end := time.Now()
	body := logImpl.CacheLogBaseModel{
		Backend:        "redis",
		Op:             op,
		Key:            key,
		Hit:            hit,
		TTL:            int64(ttl.Seconds()),
		BodySize:       bodySize,
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		EndTime:        end.Format(gaia.DateTimeMillsFormat),
		Duration:       float64(end.Sub(start).Milliseconds()),
		StartTimeStamp: start.UnixMilli(),
		EndTimeStamp:   end.UnixMilli(),
	}
	level := gaia.LogInfoLevel
	content := fmt.Sprintf("%s %s", op, key)
	if err != nil {
		level = gaia.LogErrorLevel
		body.Err = err.Error()
		content += " err=" + err.Error()
	}
	c.logger.CacheLog(level, content)
	c.logger.CacheLogBody(level, content, body)
}

func (c *Client) GetString(key string) (string, error) {
	start := time.Now()
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		hit := false
		c.emitCacheLog("get", key, &hit, 0, 0, start, nil)
		return "", nil
	}
	if res.Err() != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, res.Err())
		return "", res.Err()
	}
	val := res.Val()
	hit := true
	c.emitCacheLog("get", key, &hit, 0, len(val), start, nil)
	return val, nil
}

func (c *Client) GetInt64(key string) (int64, error) {
	start := time.Now()
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		hit := false
		c.emitCacheLog("get", key, &hit, 0, 0, start, nil)
		return 0, nil
	}
	if res.Err() != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, res.Err())
		return 0, res.Err()
	}
	val, err := res.Int64()
	if err != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, err)
		return 0, err
	}
	hit := true
	c.emitCacheLog("get", key, &hit, 0, len(res.Val()), start, nil)
	return val, nil
}

func (c *Client) GetInt(key string) (int, error) {
	start := time.Now()
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		hit := false
		c.emitCacheLog("get", key, &hit, 0, 0, start, nil)
		return 0, nil
	}
	if res.Err() != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, res.Err())
		return 0, res.Err()
	}
	val, err := res.Int()
	if err != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, err)
		return 0, err
	}
	hit := true
	c.emitCacheLog("get", key, &hit, 0, len(res.Val()), start, nil)
	return val, nil
}

func (c *Client) GetFloat(key string) (float64, error) {
	start := time.Now()
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		hit := false
		c.emitCacheLog("get", key, &hit, 0, 0, start, nil)
		return 0, nil
	}
	if res.Err() != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, res.Err())
		return 0, res.Err()
	}
	val, err := res.Float64()
	if err != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, err)
		return 0, err
	}
	hit := true
	c.emitCacheLog("get", key, &hit, 0, len(res.Val()), start, nil)
	return val, nil
}

func (c *Client) GetBool(key string) (bool, error) {
	start := time.Now()
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		hit := false
		c.emitCacheLog("get", key, &hit, 0, 0, start, nil)
		return false, nil
	}
	if res.Err() != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, res.Err())
		return false, res.Err()
	}
	val, err := res.Bool()
	if err != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, err)
		return false, err
	}
	hit := true
	c.emitCacheLog("get", key, &hit, 0, len(res.Val()), start, nil)
	return val, nil
}

func (c *Client) Get(key string) ([]byte, error) {
	start := time.Now()
	res, err := c.get(key)
	if err != nil {
		c.emitCacheLog("get", key, nil, 0, 0, start, err)
		return nil, err
	}
	hit := res != nil
	c.emitCacheLog("get", key, &hit, 0, len(res), start, nil)
	return res, nil
}

func (c *Client) get(key string) ([]byte, error) {
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		return nil, nil
	}
	if res.Err() != nil {
		return nil, res.Err()
	}
	return res.Bytes()
}

func (c *Client) Set(key string, value any, duration time.Duration) error {
	start := time.Now()
	d := c.c.Set(c.ctx, key, value, duration)
	if d.Err() != nil {
		c.emitCacheLog("set", key, nil, duration, 0, start, d.Err())
		return d.Err()
	}
	c.emitCacheLog("set", key, nil, duration, len(fmt.Sprint(value)), start, nil)
	return nil
}

func (c *Client) SetNx(key string, value any, duration time.Duration) (bool, error) {
	start := time.Now()
	nx := c.c.SetNX(c.ctx, key, value, duration)
	if nx.Err() != nil {
		c.emitCacheLog("setnx", key, nil, duration, 0, start, nx.Err())
		return false, nx.Err()
	}
	ok, err := nx.Result()
	if err != nil {
		c.emitCacheLog("setnx", key, nil, duration, 0, start, err)
		return false, err
	}
	c.emitCacheLog("setnx", key, nil, duration, len(fmt.Sprint(value)), start, nil)
	return ok, nil
}

func (c *Client) SetEx(key string, value any, duration time.Duration) error {
	start := time.Now()
	err := c.c.SetEx(c.ctx, key, value, duration).Err()
	bodySize := 0
	if err == nil {
		bodySize = len(fmt.Sprint(value))
	}
	c.emitCacheLog("setex", key, nil, duration, bodySize, start, err)
	return err
}

func (c *Client) Incr(key string) (int64, error) {
	start := time.Now()
	incr := c.c.Incr(c.ctx, key)
	if incr.Err() != nil {
		c.emitCacheLog("incr", key, nil, 0, 0, start, incr.Err())
		return 0, incr.Err()
	}
	val, err := incr.Result()
	c.emitCacheLog("incr", key, nil, 0, 0, start, err)
	return val, err
}

func (c *Client) IncrByStep(key string, step int64) (int64, error) {
	start := time.Now()
	incrBy := c.c.IncrBy(c.ctx, key, step)
	if incrBy.Err() != nil {
		c.emitCacheLog("incr", key, nil, 0, 0, start, incrBy.Err())
		return 0, incrBy.Err()
	}
	val, err := incrBy.Result()
	c.emitCacheLog("incr", key, nil, 0, 0, start, err)
	return val, err
}

func (c *Client) Exist(key string) (bool, error) {
	start := time.Now()
	exists := c.c.Exists(c.ctx, key)
	if exists.Err() != nil {
		c.emitCacheLog("exists", key, nil, 0, 0, start, exists.Err())
		return false, exists.Err()
	}
	hit := exists.Val() == 1
	c.emitCacheLog("exists", key, &hit, 0, 0, start, nil)
	return hit, nil
}

func (c *Client) Del(key string) error {
	start := time.Now()
	err := c.c.Del(c.ctx, key).Err()
	c.emitCacheLog("del", key, nil, 0, 0, start, err)
	return err
}
