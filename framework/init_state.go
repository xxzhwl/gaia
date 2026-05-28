// Package framework 初始化状态跟踪
//
// 背景：framework.Init() 是推荐的总装入口，负责注入 LocalLogger / NewLogger /
// 远程配置中心 / tracer / metrics / httpclient 拦截器 / 消息告警 / dbLogger。
// 漏调 Init 不会编译报错，但会导致：
//   - 日志走 TempLog 空壳，远程日志推送失效
//   - tracer / metrics 退化为 noop，链路和指标全丢
//   - 远程配置中心不装配，GetConf 只能读本地
//   - panic 告警发不出去（飞书 hook 为空）
//
// 这些都是"静默降级"，排查问题时极易困惑。
//
// 实现说明：真正的标志位和检查逻辑放在根包 gaia（gaia.SetFrameworkInitialized /
// gaia.IsFrameworkInitialized / gaia.EnsureFrameworkInitialized），原因是
// framework 下的子包（logImpl/tracer/metrics 等）都依赖根包，若把检查逻辑放
// 在 framework 包，子包反向 import framework 会形成循环依赖。本文件只保留
// framework 包级的薄封装。
//
// 设计取舍：用 Warn 而非 panic。framework 的部分能力（如单独用 server 起个
// 调试 HTTP）确实不需要完整 Init，panic 会误伤这些合法场景。Warn 一次足以
// 在日志里留下显眼线索。
//
// @author gaia-framework
// @created 2026-06-22
package framework

import "github.com/xxzhwl/gaia"

// markInitialized 由 Init() 末尾调用，标记框架已装配完成。
// 导出小写：仅限包内 Init 调用，外部无法伪造"已初始化"状态。
func markInitialized() {
	gaia.SetFrameworkInitialized(true)
}

// IsInitialized 返回 framework.Init 是否已执行完成。
// 供业务自检或测试断言使用。
func IsInitialized() bool {
	return gaia.IsFrameworkInitialized()
}

// EnsureInitialized 在"会真正依赖 Init 装配结果"的关键路径上做一次性自检。
// 详细语义见 gaia.EnsureFrameworkInitialized 的注释。
//
// callerKey 建议用 "子包.函数名" 形式（如 "logImpl.PushLog"），便于日志定位。
func EnsureInitialized(callerKey string) bool {
	return gaia.EnsureFrameworkInitialized(callerKey)
}

