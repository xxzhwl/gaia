// Package framework 组件依赖检测
// 在 Init 阶段统一检测所有组件的配置状态 + 可达性，区分必选/可选组件
// - 必选组件：未配置 / 不可达 → panic，阻止启动
// - 可选组件：未配置 → warn 一次，运行时静默跳过
//             已配置但不可达 → warn，标记为不可用，运行时仍会跳过
// @author gaia-framework
// @created 2026-04-17
package framework

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
)

// ComponentLevel 组件重要性等级
type ComponentLevel int

const (
	// Required 必选组件 — 缺失 / 不可达则阻止启动
	Required ComponentLevel = iota
	// Optional 可选组件 — 缺失 / 不可达则 warn 一次，运行时静默跳过
	Optional
)

// defaultProbeTimeout Probe 探活超时（单次）
const defaultProbeTimeout = 3 * time.Second

// ProbeFunc 可达性探活函数：返回 nil 表示组件真实可用；任意非 nil 表示不可达。
// 函数应当能感知 context 超时，避免拖垮启动；不能感知 context 的网络调用应在内部自建超时。
type ProbeFunc func(ctx context.Context) error

// ComponentDef 组件定义
type ComponentDef struct {
	Name       string         // 组件名称（用于日志显示）
	Level      ComponentLevel // 必选 / 可选
	ConfigKeys []string       // 需要检测的配置键（任一存在即视为已配置）
	Desc       string         // 组件用途描述
	Probe      ProbeFunc      // 可选：可达性探活函数；nil 表示只做配置检查
}

// ComponentStatus 组件状态
type ComponentStatus struct {
	Name      string
	Available bool   // 最终是否可用（配置存在 && 探活通过；未提供探活时只看配置）
	Level     ComponentLevel
	Reason    string // 不可用的原因
	Configured bool  // 配置是否存在
	Probed     bool  // 是否进行过 Probe（提供了 Probe 且配置存在时为 true）
	ProbeErr   error // Probe 失败的底层错误；Probed=true 且 Available=false 时有值
}

// componentRegistry 组件注册表
var (
	componentRegistry  []ComponentDef
	componentStatusMap = make(map[string]*ComponentStatus)
	componentMu        sync.RWMutex
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
		Probe:      probeES,
	},
	{
		Name:       "Consul",
		Level:      Optional,
		ConfigKeys: []string{"RemoteConfig.EndPoint"},
		Desc:       "远程配置中心（Consul）",
		Probe:      probeConsul,
	},
	{
		Name:       "Jaeger/OTel",
		Level:      Optional,
		ConfigKeys: []string{"Framework.JaegerTracePoint"},
		Desc:       "链路追踪（OpenTelemetry）",
		Probe:      probeJaeger,
	},
	{
		Name:       "FeiShu Robot",
		Level:      Optional,
		ConfigKeys: []string{"Message.FeiShuRobot"},
		Desc:       "飞书机器人告警",
		// 飞书机器人是 webhook 类组件，"发出去才知道"，不做主动探活
	},
	{
		Name:       "MySQL",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Mysql"},
		Desc:       "框架默认 MySQL 连接",
		Probe:      probeMysql,
	},
	{
		Name:       "Redis",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Redis.Address"},
		Desc:       "框架默认 Redis 连接",
		Probe:      probeRedis,
	},
	{
		Name:       "Kafka",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Kafka.Brokers"},
		Desc:       "Kafka 消息队列",
		Probe:      probeKafka,
	},
	{
		Name:       "COS",
		Level:      Optional,
		ConfigKeys: []string{"Framework.Cos.SecretID"},
		Desc:       "腾讯云对象存储",
		// COS 的 client 构造本身就会做鉴权握手，探活意义不大
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

// checkComponents 检测所有注册组件的配置状态 + 可达性
// 返回值：是否存在必选组件不可用
func checkComponents() bool {
	componentMu.RLock()
	registry := make([]ComponentDef, len(componentRegistry))
	copy(registry, componentRegistry)
	componentMu.RUnlock()

	// 并发探活：每个组件一个 goroutine，共享统一的截止时间。
	// 超时为 defaultProbeTimeout（默认 3s），由业务配置 Gaia.ProbeTimeout（秒）覆盖。
	probeTimeout := time.Duration(
		gaia.GetSafeConfInt64WithDefault("Gaia.ProbeTimeout", int64(defaultProbeTimeout/time.Second)),
	) * time.Second
	if probeTimeout <= 0 {
		probeTimeout = defaultProbeTimeout
	}

	statuses := make([]ComponentStatus, len(registry))
	var wg sync.WaitGroup
	for i, comp := range registry {
		wg.Add(1)
		go func(idx int, c ComponentDef) {
			defer wg.Done()
			statuses[idx] = checkSingleComponent(c, probeTimeout)
		}(i, comp)
	}
	wg.Wait()

	var (
		available       []ComponentStatus
		optUnconfigured []ComponentStatus
		optUnreachable  []ComponentStatus
		reqMissing      []ComponentStatus
	)
	for _, status := range statuses {
		componentMu.Lock()
		componentStatusMap[status.Name] = &status
		componentMu.Unlock()

		switch {
		case status.Available:
			available = append(available, status)
		case status.Level == Required:
			reqMissing = append(reqMissing, status)
		case !status.Configured:
			optUnconfigured = append(optUnconfigured, status)
		default:
			// 可选 + 已配置 + 探活失败
			optUnreachable = append(optUnreachable, status)
		}
	}

	// 打印检测报告
	printComponentReport(available, optUnconfigured, optUnreachable, reqMissing)

	return len(reqMissing) > 0
}

// checkSingleComponent 检测单个组件：
//  1. 先看配置是否存在
//  2. 若存在且提供了 Probe，则在 timeout 内做可达性探活
//
// 未提供 Probe 时退化为"仅检查配置"，与旧版本行为兼容。
func checkSingleComponent(comp ComponentDef, timeout time.Duration) ComponentStatus {
	// Step 1: 配置存在性
	configured := false
	for _, key := range comp.ConfigKeys {
		if gaia.GetSafeConfString(key) != "" {
			configured = true
			break
		}
	}
	if !configured {
		return ComponentStatus{
			Name:       comp.Name,
			Available:  false,
			Level:      comp.Level,
			Configured: false,
			Reason:     fmt.Sprintf("配置缺失: %s", strings.Join(comp.ConfigKeys, " 或 ")),
		}
	}

	// Step 2: 未提供 Probe：向后兼容，只要配置存在就视为可用
	if comp.Probe == nil {
		return ComponentStatus{
			Name:       comp.Name,
			Available:  true,
			Level:      comp.Level,
			Configured: true,
			Probed:     false,
		}
	}

	// Step 3: Probe + timeout（使用 recover 保护，Probe panic 视为失败）
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var probeErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				probeErr = fmt.Errorf("probe panic: %v", r)
			}
		}()
		probeErr = comp.Probe(ctx)
	}()

	if probeErr != nil {
		return ComponentStatus{
			Name:       comp.Name,
			Available:  false,
			Level:      comp.Level,
			Configured: true,
			Probed:     true,
			ProbeErr:   probeErr,
			Reason:     fmt.Sprintf("探活失败: %v", probeErr),
		}
	}
	return ComponentStatus{
		Name:       comp.Name,
		Available:  true,
		Level:      comp.Level,
		Configured: true,
		Probed:     true,
	}
}

// printComponentReport 打印组件检测报告
// 区分"可选-未配置"与"可选-已配置但不可达"——后者更需要运维关注
func printComponentReport(available, optUnconfigured, optUnreachable, reqMissing []ComponentStatus) {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString("╔══════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║              GAIA 组件依赖检测报告                      ║\n")
	sb.WriteString("╠══════════════════════════════════════════════════════════╣\n")

	// 已就绪的组件
	if len(available) > 0 {
		sb.WriteString("║  ✅ 已就绪:                                             ║\n")
		for _, s := range available {
			label := s.Name
			if s.Probed {
				label += " (已探活)"
			} else if s.Configured {
				label += " (仅配置检查)"
			}
			line := fmt.Sprintf("║     %-50s ║\n", label)
			sb.WriteString(line)
		}
	}

	// 可选-未配置（仅警告）
	if len(optUnconfigured) > 0 {
		sb.WriteString("║  ⚠️  可选组件未配置（已禁用，不影响运行）:                ║\n")
		for _, s := range optUnconfigured {
			line := fmt.Sprintf("║     %-50s ║\n", fmt.Sprintf("%s — %s", s.Name, s.Reason))
			sb.WriteString(line)
		}
	}

	// 可选-已配置但不可达（更显眼的警告）
	if len(optUnreachable) > 0 {
		sb.WriteString("║  🚨 可选组件配置已填但不可达（运行时将跳过，请排查）:    ║\n")
		for _, s := range optUnreachable {
			line := fmt.Sprintf("║     %-50s ║\n", fmt.Sprintf("%s — %s", s.Name, s.Reason))
			sb.WriteString(line)
		}
	}

	// 必选组件缺失/不可达（致命错误）
	if len(reqMissing) > 0 {
		sb.WriteString("║  ❌ 必选组件不可用（无法启动）:                          ║\n")
		for _, s := range reqMissing {
			line := fmt.Sprintf("║     %-50s ║\n", fmt.Sprintf("%s — %s", s.Name, s.Reason))
			sb.WriteString(line)
		}
	}

	sb.WriteString("╚══════════════════════════════════════════════════════════╝\n")

	// 使用不同级别打印
	switch {
	case len(reqMissing) > 0:
		gaia.Error(sb.String())
	case len(optUnreachable) > 0:
		gaia.Warn(sb.String())
	default:
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
