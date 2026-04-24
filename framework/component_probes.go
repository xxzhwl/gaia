// Package framework 内置组件的可达性探活实现
// 与 component_check.go 解耦，避免 component_check.go 直接 import 大量 components 包。
// @author gaia-framework
// @created 2026-04-20
package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/consul"
	"github.com/xxzhwl/gaia/components/es"
	"github.com/xxzhwl/gaia/components/kafka"
	"github.com/xxzhwl/gaia/components/redis"
)

// probeMysql 通过 gaia.NewFrameworkMysql 建连 + PingContext
// MySQL 的连接池在 NewMySQLWithDsn 里就会做一次 Ping，这里再做一次带超时的 ping 以防 DSN 改变。
func probeMysql(ctx context.Context) error {
	db, err := gaia.NewFrameworkMysql()
	if err != nil {
		return fmt.Errorf("建立连接失败: %w", err)
	}
	sqlDb, err := db.GetGormDb().DB()
	if err != nil {
		return fmt.Errorf("获取 *sql.DB 失败: %w", err)
	}
	return sqlDb.PingContext(ctx)
}

// probeRedis 构建 Framework.Redis 客户端并执行 PING
func probeRedis(ctx context.Context) error {
	cli := redis.NewFrameworkClient()
	if cli == nil {
		return fmt.Errorf("redis 客户端为空")
	}
	c := cli.GetCli()
	return c.Ping(ctx).Err()
}

// probeES 构建 Framework.ES 客户端并调用 Info API
func probeES(ctx context.Context) error {
	cli, err := es.NewFrameWorkEs()
	if err != nil {
		return fmt.Errorf("创建 ES 客户端失败: %w", err)
	}
	_, err = cli.GetCli().Info().Do(ctx)
	if err != nil {
		return fmt.Errorf("Info API 调用失败: %w", err)
	}
	return nil
}

// probeConsul 通过 Consul HTTP API 的 /v1/status/leader 做探活（TCP+HTTP 可达）
func probeConsul(ctx context.Context) error {
	endpoint := gaia.GetSafeConfString("RemoteConfig.EndPoint")
	if endpoint == "" {
		return fmt.Errorf("RemoteConfig.EndPoint 未配置")
	}
	// consul Client 未暴露底层 api.Client，也没有提供 ping-like 方法；
	// 直接用 GetConfigs 去读一个不存在的 key 即可验证 HTTP 可达。
	cli, err := consul.NewClient(endpoint)
	if err != nil {
		return fmt.Errorf("创建 consul 客户端失败: %w", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := cli.GetConfigs("__gaia_probe_nonexistent_key__")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("consul 探活失败: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// probeKafka 通过 kafka.NewClient 建立 controller 连接；成功即视为可用。
func probeKafka(ctx context.Context) error {
	brokers := gaia.GetSafeConfString("Framework.Kafka.Brokers")
	if brokers == "" {
		return fmt.Errorf("Framework.Kafka.Brokers 未配置")
	}
	done := make(chan error, 1)
	go func() {
		cli, err := kafka.NewClient(brokers)
		if err != nil {
			done <- err
			return
		}
		_ = cli.Close()
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// probeJaeger 仅做 HTTP 端点 head 请求判断是否可达
// 使用 net.DialTimeout 对 TCP 层进行探活，不做真实 OTLP 握手（成本太高）
func probeJaeger(ctx context.Context) error {
	ep := gaia.GetSafeConfString("Framework.JaegerTracePoint")
	if ep == "" {
		return fmt.Errorf("Framework.JaegerTracePoint 未配置")
	}
	// Jaeger/OTel 的 endpoint 一般是 "host:port"；tracer 已经初始化过，这里只是 sanity-check。
	// 直接借用 net.DialTimeout
	return dialTCP(ctx, ep)
}

// dialTCP 是一个最小依赖的 TCP 探活：用于任何格式是 "host:port" 的端点。
func dialTCP(ctx context.Context, addr string) error {
	// host:port 解析由 net 包完成
	d := time.Second
	if deadline, ok := ctx.Deadline(); ok {
		d = time.Until(deadline)
		if d <= 0 {
			return fmt.Errorf("probe deadline exceeded")
		}
	}
	done := make(chan error, 1)
	go func() {
		conn, err := netDial("tcp", addr, d)
		if err != nil {
			done <- err
			return
		}
		_ = conn.Close()
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
