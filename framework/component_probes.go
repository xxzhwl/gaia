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
	"github.com/xxzhwl/gaia/components/es"
	"github.com/xxzhwl/gaia/components/kafka"
	"github.com/xxzhwl/gaia/components/milvus"
	"github.com/xxzhwl/gaia/components/redis"
	"github.com/xxzhwl/gaia/framework/remoteConfig"
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

// probeMilvus 构建 Framework.Milvus 客户端并调用 CheckHealth。
func probeMilvus(ctx context.Context) error {
	cli, err := milvus.NewFrameworkClientWithContext(ctx)
	if err != nil {
		return fmt.Errorf("创建 Milvus 客户端失败: %w", err)
	}
	defer cli.Close()
	return cli.Ping(ctx)
}

// probeTracer 仅做 TCP 探活，判断追踪后端（OTLP/Jaeger/Tempo/SigNoz/Collector等）是否可达。
func probeTracer(ctx context.Context) error {
	ep := gaia.GetSafeConfString("Framework.Tracer.Endpoint")
	if ep == "" {
		return fmt.Errorf("Framework.Tracer.Endpoint 未配置")
	}
	// OTLP endpoint 一般是 "host:port"；tracer 已经初始化过，这里只是 sanity-check。
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

// probeRemoteConfig 远程配置中心探活
//
//   - RemoteConfig.Type 为空 → 视为未启用，直接通过（component_check 上层会跳过 probe，这里兜底）
//   - 已装配的 Provider 实现了 remoteConfig.Prober → 调用其 Probe（耗时受 ctx 控制）
//   - 未实现 Prober → 仅核对 ActiveCenter 已注册即视为可用
//   - 装配失败（ActiveCenter 为 nil）→ 报错
func probeRemoteConfig(ctx context.Context) error {
	typ := gaia.GetSafeConfString("RemoteConfig.Type")
	if typ == "" || typ == "none" {
		return nil
	}
	center := remoteConfig.ActiveCenter()
	if center == nil {
		return fmt.Errorf("RemoteConfig.Type=%s 但未装配 Provider，请检查启动日志", typ)
	}
	prober, ok := center.(remoteConfig.Prober)
	if !ok {
		// Provider 未实现 Probe：装配成功即视为可用
		return nil
	}
	return prober.Probe(ctx)
}
