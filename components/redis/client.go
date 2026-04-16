// Package redis 注释
// @author wanlizhan
// @created 2024/5/21
package redis

import (
	"context"
	"errors"
	"fmt"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/xxzhwl/gaia"
	"time"
)

type Client struct {
	c      redis.Client
	logger gaia.IBaseLog
	ctx    context.Context
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
	client.logger = gaia.NewLogger(schema + "_redis")
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

	return &Client{c: *trueClient, logger: gaia.NewLogger("redis")}
}

func (c *Client) SetLogTitle(title string) *Client {
	c.logger = gaia.NewLogger(title)
	return c
}

func (c *Client) WithCtx(ctx context.Context) *Client {
	c.ctx = ctx
	return c
}

func (c *Client) GetCli() redis.Client {
	return c.c
}

func (c *Client) GetString(key string) (string, error) {
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		return "", nil
	}
	if res.Err() != nil {
		return "", res.Err()
	}
	return res.Val(), nil
}

func (c *Client) GetInt64(key string) (int64, error) {
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		return 0, nil
	}
	if res.Err() != nil {
		return 0, res.Err()
	}
	return res.Int64()
}

func (c *Client) GetInt(key string) (int, error) {
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		return 0, nil
	}
	if res.Err() != nil {
		return 0, res.Err()
	}
	return res.Int()
}

func (c *Client) GetFloat(key string) (float64, error) {
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		return 0, nil
	}
	if res.Err() != nil {
		return 0, res.Err()
	}
	return res.Float64()
}

func (c *Client) GetBool(key string) (bool, error) {
	res := c.c.Get(c.ctx, key)
	if errors.Is(res.Err(), redis.Nil) {
		return false, nil
	}
	if res.Err() != nil {
		return false, res.Err()
	}
	return res.Bool()
}

func (c *Client) Get(key string) ([]byte, error) {
	res, err := c.get(key)
	if err != nil {
		c.logger.ErrorF("获取%s:失败,err:%s", key, err.Error())
		return nil, err
	}
	c.logger.InfoF("获取%s:%s", key, string(res))
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
	d := c.c.Set(c.ctx, key, value, duration)
	if d.Err() != nil {
		c.logger.ErrorF("设置[%s-%v-%dms]失败:%s", key, value, duration.Milliseconds(), d.Err().Error())
	}
	c.logger.InfoF("设置[%s-%v-%dms]成功", key, value, duration.Milliseconds())
	return d.Err()
}

func (c *Client) SetNx(key string, value any, duration time.Duration) (bool, error) {
	nx := c.c.SetNX(c.ctx, key, value, duration)
	if nx.Err() != nil {
		return false, nx.Err()
	}
	return nx.Result()
}

func (c *Client) SetEx(key string, value any, duration time.Duration) error {
	return c.c.SetEx(c.ctx, key, value, duration).Err()
}

func (c *Client) Incr(key string) (int64, error) {
	incr := c.c.Incr(c.ctx, key)
	if incr.Err() != nil {
		return 0, incr.Err()
	}
	return incr.Result()
}

func (c *Client) IncrByStep(key string, step int64) (int64, error) {
	incrBy := c.c.IncrBy(c.ctx, key, step)
	if incrBy.Err() != nil {
		return 0, incrBy.Err()
	}
	return incrBy.Result()
}

func (c *Client) Exist(key string) (bool, error) {
	exists := c.c.Exists(c.ctx, key)
	if exists.Err() != nil {
		return false, exists.Err()
	}
	if exists.Val() == 1 {
		return true, nil
	}
	return false, nil
}

func (c *Client) Del(key string) error {
	return c.c.Del(c.ctx, key).Err()
}
