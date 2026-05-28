// Package framework 运行时就绪探针。
//
// 与 component_check.go 中的 checkComponents（Init 期一次性的组件配置 + 探活报告）
// 不同，本文件提供"运行时实时探活"接口，专为 K8s `/readyz` 这类需要按需重新评估
// 依赖可达性的场景设计。
//
// 设计取舍：
//  1. 探针结果不缓存：每次调用都对所有已注册且提供了 Probe 的 Optional/Required
//     组件并发探活；调用方（http handler）应自行做"调用频率限制"以避免压垮下游。
//  2. 默认 timeout 复用 Gaia.ProbeTimeout 配置（不可超过 ctx 提供的 deadline）。
//  3. 仅 Required 组件失败会让 Ready=false；Optional 组件即使探活失败也只是
//     reason 字段标注，整体仍 Ready，符合"可选组件不影响存活"的语义。
//
// @author gaia-framework
// @created 2026-06-01
package framework

import (
	"context"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
)

// ReadinessReport 运行时就绪报告。
type ReadinessReport struct {
	// Ready 是否整体就绪：所有 Required 组件 Probe 通过即为 true。
	Ready bool `json:"ready"`
	// Components 每个有 Probe 的组件的实时探活结果。
	Components []ReadinessComponent `json:"components"`
	// CheckedAt 报告生成时间（RFC3339）。
	CheckedAt string `json:"checked_at"`
	// DurationMs 全部探针并发执行的总耗时（毫秒）。
	DurationMs int64 `json:"duration_ms"`
}

// ReadinessComponent 单个组件的探针结果。
type ReadinessComponent struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	OK       bool   `json:"ok"`
	// Reason 失败原因，OK=true 时为空。
	Reason string `json:"reason,omitempty"`
	// LatencyMs 单个 Probe 的耗时。
	LatencyMs int64 `json:"latency_ms"`
	// Config 组件配置地址/连接串，方便排查问题。
	Config string `json:"config,omitempty"`
}

// RunReadinessProbes 对所有已注册且提供了 Probe 的组件执行一次实时探活。
//
// 行为说明：
//   - 仅对"配置存在 + Probe!=nil"的组件做探活；未配置的可选组件不计入报告，
//     与 Init 期 checkComponents 的展示语义一致。
//   - 单个 Probe 的超时取 min(ctx 剩余时间, Gaia.ProbeTimeout 默认 3s)。
//   - 任一 Required 组件失败 → Report.Ready = false。
//   - 全部 Optional 失败 → Report.Ready 仍为 true（可选组件不影响 ready）。
//
// 该函数不会改变 component_check.go 缓存的 componentStatusMap，避免运行时探活
// 与 Init 报告产生数据竞争。
func RunReadinessProbes(ctx context.Context) ReadinessReport {
	start := time.Now()

	componentMu.RLock()
	registry := make([]ComponentDef, len(componentRegistry))
	copy(registry, componentRegistry)
	componentMu.RUnlock()

	// 计算单 Probe 超时
	probeTimeout := time.Duration(
		gaia.GetSafeConfInt64WithDefault("Gaia.ProbeTimeout", int64(defaultProbeTimeout/time.Second)),
	) * time.Second
	if probeTimeout <= 0 {
		probeTimeout = defaultProbeTimeout
	}
	if dl, ok := ctx.Deadline(); ok {
		if remain := time.Until(dl); remain > 0 && remain < probeTimeout {
			probeTimeout = remain
		}
	}

	type slot struct {
		valid bool
		comp  ReadinessComponent
	}
	results := make([]slot, len(registry))

	var wg sync.WaitGroup
	for i, comp := range registry {
		// 跳过未配置的组件
		configured := false
		for _, key := range comp.ConfigKeys {
			if gaia.GetSafeConfString(key) != "" {
				configured = true
				break
			}
		}
		if !configured || comp.Probe == nil {
			continue
		}

		wg.Add(1)
		go func(idx int, c ComponentDef) {
			defer wg.Done()
			results[idx] = slot{valid: true, comp: runSingleProbe(ctx, c, probeTimeout)}
		}(i, comp)
	}
	wg.Wait()

	report := ReadinessReport{
		Ready:      true,
		Components: make([]ReadinessComponent, 0, len(registry)),
		CheckedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, r := range results {
		if !r.valid {
			continue
		}
		report.Components = append(report.Components, r.comp)
		if !r.comp.OK && r.comp.Required {
			report.Ready = false
		}
	}
	report.DurationMs = time.Since(start).Milliseconds()
	return report
}

// runSingleProbe 单组件实时探活，带 panic 兜底。
func runSingleProbe(parent context.Context, comp ComponentDef, timeout time.Duration) ReadinessComponent {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	res := ReadinessComponent{
		Name:     comp.Name,
		Required: comp.Level == Required,
	}

	// 解析组件配置地址供排查
	for _, key := range comp.ConfigKeys {
		if v := gaia.GetSafeConfString(key); v != "" {
			res.Config = v
			break
		}
	}

	start := time.Now()

	var probeErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				probeErr = errProbePanicked(r)
			}
		}()
		probeErr = comp.Probe(ctx)
	}()

	res.LatencyMs = time.Since(start).Milliseconds()
	if probeErr != nil {
		res.OK = false
		res.Reason = probeErr.Error()
		return res
	}
	res.OK = true
	return res
}

// errProbePanicked 把 panic value 包成 error，避免 fmt.Errorf 引入额外依赖循环。
func errProbePanicked(v any) error {
	return &probePanicErr{v: v}
}

type probePanicErr struct{ v any }

func (e *probePanicErr) Error() string {
	return "probe panic: " + sprintAny(e.v)
}

// sprintAny 用 gaia 的轻量打印，避免 import fmt 仅为 panic 文案。
func sprintAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if s, ok := v.(error); ok {
		return s.Error()
	}
	type stringer interface{ String() string }
	if s, ok := v.(stringer); ok {
		return s.String()
	}
	return "non-string panic value"
}
