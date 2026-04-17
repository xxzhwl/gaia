// Package rpcserver Kitex 客户端封装
// 提供 Kitex 客户端的通用选项构建（含 Consul 服务发现）
// @author gaia-framework
// @created 2026-04-17
package rpcserver

import (
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/pkg/transmeta"
	consul "github.com/kitex-contrib/registry-consul"

	"github.com/xxzhwl/gaia"
)

// KitexClientConfig Kitex 客户端配置
type KitexClientConfig struct {
	Schema string // 配置前缀（默认 "RpcClient"）
}

// BuildKitexClientOptions 构建 Kitex 客户端通用选项
// 用户将这些选项传入 kitex 生成的 NewClient 函数
//
// 用法：
//
//	opts := rpcserver.BuildKitexClientOptions(rpcserver.KitexClientConfig{Schema: "RpcClient"})
//	cli, err := userservice.NewClient("UserService", opts...)
func BuildKitexClientOptions(cfg KitexClientConfig) []client.Option {
	if cfg.Schema == "" {
		cfg.Schema = "RpcClient"
	}

	var opts []client.Option

	// 中间件（复用 Server 端中间件，Kitex 的 endpoint.Middleware 通用）
	opts = append(opts,
		client.WithMiddleware(KitexRecoveryMiddleware()),
		client.WithMiddleware(KitexLoggingMiddleware()),
		client.WithMiddleware(KitexTracingMiddleware()),
	)

	// 元信息传递
	opts = append(opts, client.WithMetaHandler(transmeta.ClientTTHeaderHandler))

	// 客户端基础信息
	opts = append(opts, client.WithClientBasicInfo(&rpcinfo.EndpointBasicInfo{
		ServiceName: gaia.GetSystemEnName(),
	}))

	// 超时配置
	rpcTimeout := gaia.GetSafeConfInt64WithDefault(cfg.Schema+".RPCTimeout", 5)
	connTimeout := gaia.GetSafeConfInt64WithDefault(cfg.Schema+".ConnTimeout", 3)
	opts = append(opts,
		client.WithRPCTimeout(time.Duration(rpcTimeout)*time.Second),
		client.WithConnectTimeout(time.Duration(connTimeout)*time.Second),
	)

	// Consul 服务发现（Kitex 原生支持）
	if gaia.GetSafeConfBool(cfg.Schema + ".Registry.Enable") {
		consulEndpoint := gaia.GetSafeConfStringWithDefault(
			cfg.Schema+".Registry.Endpoint", "localhost:8500")
		r, err := consul.NewConsulResolver(consulEndpoint)
		if err != nil {
			gaia.ErrorF("[Kitex-Client] 创建 Consul Resolver 失败: %v", err)
		} else {
			opts = append(opts, client.WithResolver(r))
			gaia.InfoF("[Kitex-Client] 已启用 Consul 服务发现: %s", consulEndpoint)
		}
	}

	return opts
}
