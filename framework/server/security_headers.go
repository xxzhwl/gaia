// Package server 安全头中间件。
//
// 在响应头中注入业界通用的浏览器侧安全防护头，构成纵深防御的"前端一行"：
//
//	Strict-Transport-Security  强制 HTTPS（防 SSL Stripping）
//	X-Content-Type-Options     禁止 MIME 嗅探
//	X-Frame-Options            禁止被 iframe 嵌套（点击劫持防护）
//	Referrer-Policy            控制 Referer 泄露范围
//	Content-Security-Policy    内容安全策略（默认不开启，需显式配置）
//	Permissions-Policy         浏览器特性开关（摄像头/麦克风等）
//	Cross-Origin-Opener-Policy 跨源隔离（Spectre 防护）
//
// 全部头都做"配置覆盖优先"：用户在 <schema>.Security.* 中显式配置时以用户为准；
// 否则使用一组保守的安全默认值。HSTS 仅在 TLS 启用时注入（HTTP 站点注入 HSTS
// 可能让无证书的子域无法访问，是常见错误）。
//
// @author wanlizhan
// @created 2026-06-01
package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/xxzhwl/gaia"
)

// securityHeadersConfig 安全头配置（从 <schema>.Security.* 解析）。
type securityHeadersConfig struct {
	enable bool

	// HSTS：仅 TLS 启用时生效，未配置时使用 1 年 + includeSubDomains
	hsts string

	// X-Content-Type-Options，默认 "nosniff"
	xContentTypeOptions string

	// X-Frame-Options，默认 "SAMEORIGIN"
	xFrameOptions string

	// Referrer-Policy，默认 "strict-origin-when-cross-origin"
	referrerPolicy string

	// Content-Security-Policy；未配置则不注入（CSP 必须按业务定制，盲打默认值会破坏页面）
	csp string

	// Permissions-Policy；未配置则不注入
	permissionsPolicy string

	// Cross-Origin-Opener-Policy，默认 "same-origin"
	coop string

	// 是否需要在 HSTS 之前判断 TLS（与 schema.EnableTLS 联动）
	tlsEnabled bool
}

// loadSecurityHeadersConfig 从配置加载，应用安全默认值。
func (s *Server) loadSecurityHeadersConfig() securityHeadersConfig {
	c := securityHeadersConfig{
		enable:     gaia.GetSafeConfBool(s.schema + ".Security.Enable"),
		tlsEnabled: gaia.GetSafeConfBool(s.schema + ".EnableTLS"),
	}
	if !c.enable {
		return c
	}

	c.hsts = gaia.GetSafeConfStringWithDefault(s.schema+".Security.HSTS",
		"max-age=31536000; includeSubDomains")
	c.xContentTypeOptions = gaia.GetSafeConfStringWithDefault(
		s.schema+".Security.XContentTypeOptions", "nosniff")
	c.xFrameOptions = gaia.GetSafeConfStringWithDefault(
		s.schema+".Security.XFrameOptions", "SAMEORIGIN")
	c.referrerPolicy = gaia.GetSafeConfStringWithDefault(
		s.schema+".Security.ReferrerPolicy", "strict-origin-when-cross-origin")
	c.csp = gaia.GetSafeConfString(s.schema + ".Security.CSP")
	c.permissionsPolicy = gaia.GetSafeConfString(s.schema + ".Security.PermissionsPolicy")
	c.coop = gaia.GetSafeConfStringWithDefault(
		s.schema+".Security.COOP", "same-origin")
	return c
}

// securityHeadersPlugin 注入安全响应头。
//
// 性能特征：
//   - 配置在中间件创建时一次性解析、缓存进闭包，请求路径上零分配；
//   - 单次写入只调用 5~7 次 Header.Set，开销可忽略。
//
// 关键设计：
//   - 头注入放在 c.Next 之前——保证业务无论是否 panic、是否提前 return 都带上头；
//   - 不覆盖业务已主动设置的同名头（业务可针对单个接口加严或放宽）。
func (s *Server) securityHeadersPlugin() app.HandlerFunc {
	cfg := s.loadSecurityHeadersConfig()

	return func(c context.Context, ctx *app.RequestContext) {
		// 探针请求豁免：/livez|/readyz|/metrics|/health 不需要浏览器侧防护头，
		// 且 K8s/Prometheus 高频调用下额外的 Header.Set 调用是可面费的开销。
		if isProbeRequest(ctx) {
			ctx.Next(c)
			return
		}

		setIfAbsent := func(k, v string) {
			if v == "" {
				return
			}
			if len(ctx.Response.Header.Peek(k)) == 0 {
				ctx.Response.Header.Set(k, v)
			}
		}

		// HSTS 仅 TLS 启用时注入；HTTP 站点注入会导致客户端强制走 HTTPS 而又无证书，反而 DoS 自己
		if cfg.tlsEnabled {
			setIfAbsent("Strict-Transport-Security", cfg.hsts)
		}
		setIfAbsent("X-Content-Type-Options", cfg.xContentTypeOptions)
		setIfAbsent("X-Frame-Options", cfg.xFrameOptions)
		setIfAbsent("Referrer-Policy", cfg.referrerPolicy)
		setIfAbsent("Content-Security-Policy", cfg.csp)
		setIfAbsent("Permissions-Policy", cfg.permissionsPolicy)
		setIfAbsent("Cross-Origin-Opener-Policy", cfg.coop)

		ctx.Next(c)
	}
}

// securityHeadersEnabled 在 server.go 决策是否挂载该中间件。
// 抽出来是为了避免重复读取配置（registerPlugin 里再 GetSafeConfBool 一次成本极小但行为更清晰）。
func (s *Server) securityHeadersEnabled() bool {
	return gaia.GetSafeConfBool(s.schema + ".Security.Enable")
}
