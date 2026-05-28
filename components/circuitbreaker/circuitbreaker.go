// Package circuitbreaker 提供进程内熔断器（Circuit Breaker）。
//
// # 设计目标
//
// 熔断器与限流器是互补关系：
//   - 限流器：保护本服务，控制流入速率（"我能处理多少"）；
//   - 熔断器：保护下游/快速失败（"下游挂了，我就别再去打它了"）；
//
// 实现遵循经典 Three-State 状态机：
//
//	Closed（关闭，正常放行）
//	  │ 失败率/失败数达阈值
//	  ▼
//	Open（打开，全部快速失败）
//	  │ 经过 OpenTimeout 后
//	  ▼
//	HalfOpen（半开，放行少量探活请求）
//	  │ 探活成功 → Closed
//	  │ 探活失败 → Open（重新计时）
//
// # 与已有限流器的关系
//
// ratelimiter 包提供 QPS/并发控制；本包专注于"故障隔离"。两者在 server 中作为
// 两个独立中间件存在，调用关系为：限流（前置） → 熔断（后置） → 业务。
//
// # 典型用法
//
//	cb := circuitbreaker.New(circuitbreaker.Settings{
//	    Name:           "downstream-A",
//	    FailureThreshold: 0.5,            // 失败率 50% 触发
//	    MinRequests:    20,               // 最少 20 个请求才参与统计
//	    OpenTimeout:    10 * time.Second, // open 持续 10s 后转 halfopen
//	    HalfOpenMaxCalls: 3,              // halfopen 最多探活 3 次
//	})
//
//	// 1) 函数包装风格
//	err := cb.Execute(ctx, func() error { return callDownstream(ctx) })
//
//	// 2) 手动接管
//	if err := cb.Allow(); err != nil { return err } // 已熔断
//	err := callDownstream(ctx)
//	cb.Report(err)
//
// @author wanlizhan
// @created 2026-06-01
package circuitbreaker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// State 熔断器状态。
type State int32

const (
	// StateClosed 关闭状态（正常放行，统计失败率）。
	StateClosed State = iota
	// StateOpen 打开状态（快速失败，所有请求直接返回 ErrOpen）。
	StateOpen
	// StateHalfOpen 半开状态（放行最多 HalfOpenMaxCalls 次探活）。
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// 预定义错误。
var (
	// ErrOpen 熔断器处于 Open 状态，请求被快速失败。
	ErrOpen = errors.New("circuit breaker is open")
	// ErrTooManyRequests HalfOpen 状态下并发探活请求超额。
	ErrTooManyRequests = errors.New("circuit breaker half-open: too many in-flight probes")
)

// Settings 熔断器配置。
type Settings struct {
	// Name 名称（用于日志、指标 label）；建议每个下游一个独立 Name。
	Name string

	// FailureThreshold 失败率阈值（0.0 ~ 1.0），达到后从 Closed 转 Open。
	// 默认 0.5（50%）。仅在 Requests >= MinRequests 时才会评估。
	FailureThreshold float64

	// MinRequests 触发熔断评估的最少请求数；样本不足时即使全部失败也不熔断。
	// 默认 20。
	MinRequests uint32

	// ConsecutiveFailures 连续失败次数阈值；达到后立即转 Open（与失败率取或）。
	// 默认 0（关闭"连续失败"判据，仅按失败率）。
	ConsecutiveFailures uint32

	// OpenTimeout Open 状态持续时长，到期后转 HalfOpen 进行探活。
	// 默认 10s。
	OpenTimeout time.Duration

	// HalfOpenMaxCalls HalfOpen 状态下最多放行的探活请求数。
	// 默认 1（保守探活；高并发可调高，但任一失败立即重新 Open）。
	HalfOpenMaxCalls uint32

	// CountWindow 统计窗口时长；窗口内的请求/失败次数会被周期性清零，
	// 防止"历史失败"长期占用统计。<=0 表示不滑窗（仅在状态切换时清零）。
	// 默认 10s。
	CountWindow time.Duration

	// IsFailure 自定义失败判定函数；为 nil 时认为 err != nil 即失败。
	// 业务可通过此钩子排除"业务级 4xx"等不应触发熔断的错误。
	IsFailure func(err error) bool

	// OnStateChange 状态切换回调；可用于打日志、上报指标。
	OnStateChange func(name string, from, to State)
}

// Breaker 熔断器主体（goroutine-safe）。
type Breaker struct {
	cfg Settings

	mu sync.Mutex // 守护下方所有状态字段（state 用 atomic 保证 Allow 快路径无锁）

	state               atomic.Int32 // 实际类型 State；用 atomic 让 Allow() 快路径无需上锁
	generation          uint64       // 每次状态切换 +1，用于丢弃过期窗口的统计上报
	requests            uint32
	failures            uint32
	successes           uint32
	consecutiveFailures uint32
	openExpireAt        time.Time // Open 状态到期时间
	halfOpenInFlight    uint32    // HalfOpen 时正在探活的请求数
	windowExpireAt      time.Time // CountWindow 滑窗到期时间
}

// New 创建熔断器并应用默认值。
func New(s Settings) *Breaker {
	if s.FailureThreshold <= 0 || s.FailureThreshold > 1 {
		s.FailureThreshold = 0.5
	}
	if s.MinRequests == 0 {
		s.MinRequests = 20
	}
	if s.OpenTimeout <= 0 {
		s.OpenTimeout = 10 * time.Second
	}
	if s.HalfOpenMaxCalls == 0 {
		s.HalfOpenMaxCalls = 1
	}
	if s.CountWindow < 0 {
		s.CountWindow = 0
	} else if s.CountWindow == 0 {
		s.CountWindow = 10 * time.Second
	}
	if s.IsFailure == nil {
		s.IsFailure = func(err error) bool { return err != nil }
	}

	b := &Breaker{cfg: s}
	b.state.Store(int32(StateClosed))
	if s.CountWindow > 0 {
		b.windowExpireAt = time.Now().Add(s.CountWindow)
	}
	return b
}

// State 返回当前状态（无锁，仅供观测；不要据此做关键决策）。
func (b *Breaker) State() State {
	return State(b.state.Load())
}

// Name 返回熔断器名称。
func (b *Breaker) Name() string { return b.cfg.Name }

// Allow 询问是否允许本次请求通过。
//
// 返回的 generation 必须原样回传给 Report，用于丢弃跨状态切换的过期上报，
// 避免"上一代的失败"污染新一代的统计。
func (b *Breaker) Allow() (generation uint64, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	switch State(b.state.Load()) {
	case StateClosed:
		b.maybeResetWindowLocked(now)
		return b.generation, nil

	case StateOpen:
		// Open 状态过期 → 转 HalfOpen
		if now.After(b.openExpireAt) {
			b.toStateLocked(StateHalfOpen, now)
			b.halfOpenInFlight = 1
			return b.generation, nil
		}
		return b.generation, ErrOpen

	case StateHalfOpen:
		if b.halfOpenInFlight >= b.cfg.HalfOpenMaxCalls {
			return b.generation, ErrTooManyRequests
		}
		b.halfOpenInFlight++
		return b.generation, nil
	}
	return b.generation, nil
}

// Report 上报本次请求结果。generation 必须是 Allow 返回的同一个值。
func (b *Breaker) Report(generation uint64, err error) {
	failed := b.cfg.IsFailure(err)

	b.mu.Lock()
	defer b.mu.Unlock()

	// 跨代上报丢弃（状态在 Allow 之后切换过，本次结果不再有意义）
	if generation != b.generation {
		return
	}

	now := time.Now()
	state := State(b.state.Load())

	switch state {
	case StateClosed:
		b.maybeResetWindowLocked(now)
		b.requests++
		if failed {
			b.failures++
			b.consecutiveFailures++
		} else {
			b.successes++
			b.consecutiveFailures = 0
		}
		if b.shouldTripLocked() {
			b.toStateLocked(StateOpen, now)
		}

	case StateHalfOpen:
		if b.halfOpenInFlight > 0 {
			b.halfOpenInFlight--
		}
		if failed {
			// 探活失败 → 重新 Open
			b.toStateLocked(StateOpen, now)
			return
		}
		b.successes++
		// 探活成功且达到 HalfOpenMaxCalls → 关闭熔断
		if b.successes >= b.cfg.HalfOpenMaxCalls {
			b.toStateLocked(StateClosed, now)
		}

	case StateOpen:
		// Open 期间不应有 Report（Allow 会拦截），忽略。
	}
}

// Execute 函数包装：自动执行 Allow → fn → Report。
//
// 当熔断器处于 Open 状态时直接返回 ErrOpen，不会调用 fn，
// 这正是熔断器的核心价值——快速失败而不是阻塞调用方。
func (b *Breaker) Execute(_ context.Context, fn func() error) error {
	gen, err := b.Allow()
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			// panic 视为失败上报，并向上抛出
			b.Report(gen, errors.New("panic in breaker.Execute"))
			panic(r)
		}
	}()
	err = fn()
	b.Report(gen, err)
	return err
}

// Reset 强制把熔断器重置为 Closed（管理接口/单测用）。
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toStateLocked(StateClosed, time.Now())
}

// Snapshot 返回一份当前状态快照，便于打日志/暴露指标。
type Snapshot struct {
	Name                string
	State               State
	Requests            uint32
	Failures            uint32
	Successes           uint32
	ConsecutiveFailures uint32
}

// Snapshot 取一份当前统计快照（线程安全）。
func (b *Breaker) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Snapshot{
		Name:                b.cfg.Name,
		State:               State(b.state.Load()),
		Requests:            b.requests,
		Failures:            b.failures,
		Successes:           b.successes,
		ConsecutiveFailures: b.consecutiveFailures,
	}
}

// shouldTripLocked 判定是否应该触发熔断（必须持锁）。
func (b *Breaker) shouldTripLocked() bool {
	// 1) 连续失败判据
	if b.cfg.ConsecutiveFailures > 0 && b.consecutiveFailures >= b.cfg.ConsecutiveFailures {
		return true
	}
	// 2) 失败率判据：样本不足时不评估
	if b.requests < b.cfg.MinRequests {
		return false
	}
	rate := float64(b.failures) / float64(b.requests)
	return rate >= b.cfg.FailureThreshold
}

// toStateLocked 状态切换 + 计数器复位 + 回调（必须持锁）。
func (b *Breaker) toStateLocked(target State, now time.Time) {
	from := State(b.state.Load())
	if from == target {
		return
	}
	b.state.Store(int32(target))
	b.generation++
	b.requests = 0
	b.failures = 0
	b.successes = 0
	b.consecutiveFailures = 0
	b.halfOpenInFlight = 0

	switch target {
	case StateOpen:
		b.openExpireAt = now.Add(b.cfg.OpenTimeout)
	case StateClosed, StateHalfOpen:
		if b.cfg.CountWindow > 0 {
			b.windowExpireAt = now.Add(b.cfg.CountWindow)
		}
	}

	if cb := b.cfg.OnStateChange; cb != nil {
		// 回调放在持锁内，避免 from/to 错乱；调用方需保证回调不会反向调用 Breaker。
		cb(b.cfg.Name, from, target)
	}
}

// maybeResetWindowLocked Closed 状态下到期清零滑窗（持锁）。
func (b *Breaker) maybeResetWindowLocked(now time.Time) {
	if b.cfg.CountWindow <= 0 {
		return
	}
	if now.Before(b.windowExpireAt) {
		return
	}
	b.requests = 0
	b.failures = 0
	b.successes = 0
	// consecutiveFailures 不清零：连续失败是跨窗口语义。
	b.windowExpireAt = now.Add(b.cfg.CountWindow)
}
