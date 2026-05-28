// Package server K8s/运维探针路径白名单。
//
// 探针请求（livez/readyz/metrics/health）由 K8s/Prometheus 高频轮询，
// 让它们经过限流、熔断、链路追踪、详细日志等中间件会带来：
//   - 日志爆炸（每 10s 一次的 livez 都打 INFO）
//   - trace 配额浪费（probe 占满采样器）
//   - metrics 高基数（probe 进 http.server.requests.total，污染 SLO）
//   - 限流计数虚高（探针被计入 IP+Path 限流）
//   - 熔断器误判（探针失败也会拉低 SLA）
//
// 所有需要"对探针豁免"的中间件都通过 isProbeRequest 判断短路。
// 后续若要让探针走独立端口（推荐生产部署），改的也只是这一个开关。
//
// @author wanlizhan
// @created 2026-06-01
package server

import (
	"github.com/cloudwego/hertz/pkg/app"
)

// probePaths 探针路径集合。常量维护，O(1) 查询。
var probePaths = map[string]struct{}{
	"/livez":   {},
	"/readyz":  {},
	"/health":  {},
	"/metrics": {},
}

// isProbeRequest 判断当前请求是否为探针/运维端点。
// 注：此函数是 hot-path 调用，必须保持极低开销——只做 map 查询，不分配内存。
func isProbeRequest(ctx *app.RequestContext) bool {
	// ctx.Request.URI().Path() 返回 []byte，转 string 是 Go 编译器优化场景下的零拷贝
	// （仅用于 map key 查找，不会逃逸到堆）。
	path := string(ctx.Request.URI().Path())
	_, ok := probePaths[path]
	return ok
}
