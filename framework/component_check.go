// Package framework 组件依赖检测
// 在 Init 阶段统一检测所有组件的配置状态，区分必选/可选组件
// - 必选组件：未配置则 panic，阻止启动
// - 可选组件：未配置则 warn 一次，运行时静默跳过
// @author gaia-framework
// @created 2026-04-17
package framework

import (
	"fmt"
	"strings"
	"sync"

	"github.com/xxzhwl/gaia"
)

// ComponentLevel 组件重要性等级
type ComponentLevel int

const (
	// Required 必选组件 — 缺失则阻止启动
	Required ComponentLevel = iota
	// Optional 可选组件 — 缺失则 warn 一次，运行时静默跳过
	Optional
)

// ComponentDef 组件定义
type ComponentDef struct {
	Name       string         // 组件名称（用于日志显示）
	Level      ComponentLevel // 必选/可选
	ConfigKeys []string       // 需要检测的配置键（任一存在即视为已配置）
	Desc       string         // 组件用途描述
}

// ComponentStatus 组件状态
type ComponentStatus struct {
	Name      string
	Available bool   // 是否可用
	Level     ComponentLevel
	Reason    string // 不可用的原因
}

// componentRegistry 组件注册表
var (
	componentRegistry   []ComponentDef
	componentStatusMap  = make(map[string]*ComponentStatus)
	componentStatusOnce sync.Once
	componentMu         sync.RWMutex
)

// 框架内置组件定义
var builtinComponents = []ComponentDef{
	// ===== 必选组件（通常为空，由业务层通过 RegisterRequiredComponent 注册） =====

	// ===== 可选组件 =====
	{
		Name:       "Elasticsearch",
		Level:      Optional,
		ConfigKeys: []string{"Framework.ES.Address"},
		Desc:       "远程日志推送（ES）",
	},
	{
		Name:       "Consul",
		Level:      Optional,
		ConfigKeys: []string{"RemoteConfig.EndPoint"},
		Desc:       "远程配置中心（Consul）",
	},
	{
		Name:       "Jaeger/OTel",
		Level:      Optional,
		ConfigKeys: []string{"Framework.JaegerTracePoint"},
		Desc:       "链路追踪（OpenTelemetry）",
	},
	{
		Name:       "FeiShu Robot",
		Level:      Optional,
		ConfigKeys: []string{"Message.FeiShuRobot"},
		Desc:       "飞书机器人告警",
	},
	{
		Name:       "MySQL",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Mysql"},
		Desc:       "框架默认 MySQL 连接",
	},
	{
		Name:       "Redis",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Redis.Address"},
		Desc:       "框架默认 Redis 连接",
	},
	{
		Name:       "Kafka",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Kafka.Brokers"},
		Desc:       "Kafka 消息队列",
	},
	{
		Name:       "COS",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Cos.SecretID"},
		Desc:       "腾讯云对象存储",
	},
}

func init() {
	componentRegistry = append(componentRegistry, builtinComponents...)
}

// RegisterComponent 注册自定义组件（业务层调用，在 Init 之前）
func RegisterComponent(def ComponentDef) {
	componentMu.Lock()
	defer componentMu.Unlock()
	componentRegistry = append(componentRegistry, def)
}

// RegisterRequiredComponent 快捷注册必选组件
func RegisterRequiredComponent(name string, configKeys []string, desc string) {
	RegisterComponent(ComponentDef{
		Name:       name,
		Level:      Required,
		ConfigKeys: configKeys,
		Desc:       desc,
	})
}

// RegisterOptionalComponent 快捷注册可选组件
func RegisterOptionalComponent(name string, configKeys []string, desc string) {
	RegisterComponent(ComponentDef{
		Name:       name,
		Level:      Optional,
		ConfigKeys: configKeys,
		Desc:       desc,
	})
}

// checkComponents 检测所有注册组件的配置状态
// 返回值：是否存在必选组件缺失
func checkComponents() bool {
	componentMu.RLock()
	registry := make([]ComponentDef, len(componentRegistry))
	copy(registry, componentRegistry)
	componentMu.RUnlock()

	var (
		available   []ComponentStatus
		optMissing  []ComponentStatus
		reqMissing  []ComponentStatus
	)

	for _, comp := range registry {
		status := checkSingleComponent(comp)

		componentMu.Lock()
		componentStatusMap[comp.Name] = &status
		componentMu.Unlock()

		if status.Available {
			available = append(available, status)
		} else if comp.Level == Required {
			reqMissing = append(reqMissing, status)
		} else {
			optMissing = append(optMissing, status)
		}
	}

	// 打印检测报告
	printComponentReport(available, optMissing, reqMissing)

	return len(reqMissing) > 0
}

// checkSingleComponent 检测单个组件
func checkSingleComponent(comp ComponentDef) ComponentStatus {
	for _, key := range comp.ConfigKeys {
		val := gaia.GetSafeConfString(key)
		if val != "" {
			return ComponentStatus{
				Name:      comp.Name,
				Available: true,
				Level:     comp.Level,
			}
		}
	}

	return ComponentStatus{
		Name:      comp.Name,
		Available: false,
		Level:     comp.Level,
		Reason:    fmt.Sprintf("配置缺失: %s", strings.Join(comp.ConfigKeys, " 或 ")),
	}
}

// printComponentReport 打印组件检测报告
func printComponentReport(available, optMissing, reqMissing []ComponentStatus) {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString("╔══════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║              GAIA 组件依赖检测报告                      ║\n")
	sb.WriteString("╠══════════════════════════════════════════════════════════╣\n")

	// 已就绪的组件
	if len(available) > 0 {
		sb.WriteString("║  ✅ 已就绪:                                             ║\n")
		for _, s := range available {
			line := fmt.Sprintf("║     %-50s ║\n", s.Name)
			sb.WriteString(line)
		}
	}

	// 可选组件缺失（仅警告）
	if len(optMissing) > 0 {
		sb.WriteString("║  ⚠️  可选组件未配置（已禁用，不影响运行）:                ║\n")
		for _, s := range optMissing {
			line := fmt.Sprintf("║     %-50s ║\n", fmt.Sprintf("%s — %s", s.Name, s.Reason))
			sb.WriteString(line)
		}
	}

	// 必选组件缺失（致命错误）
	if len(reqMissing) > 0 {
		sb.WriteString("║  ❌ 必选组件缺失（无法启动）:                            ║\n")
		for _, s := range reqMissing {
			line := fmt.Sprintf("║     %-50s ║\n", fmt.Sprintf("%s — %s", s.Name, s.Reason))
			sb.WriteString(line)
		}
	}

	sb.WriteString("╚══════════════════════════════════════════════════════════╝\n")

	// 使用不同级别打印
	if len(reqMissing) > 0 {
		gaia.Error(sb.String())
	} else {
		gaia.Info(sb.String())
	}
}

// IsComponentAvailable 运行时检查某个组件是否可用（O(1) 查询）
func IsComponentAvailable(name string) bool {
	componentMu.RLock()
	defer componentMu.RUnlock()
	if s, ok := componentStatusMap[name]; ok {
		return s.Available
	}
	return false
}

// GetComponentStatus 获取组件状态详情
func GetComponentStatus(name string) (*ComponentStatus, bool) {
	componentMu.RLock()
	defer componentMu.RUnlock()
	s, ok := componentStatusMap[name]
	return s, ok
}
