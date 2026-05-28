package gaia

/*
recover.go 的单元测试

覆盖场景：
  1. 异步发送：PanicLog 不被慢 webhook 阻塞
  2. 窗口内去重：同一 panic 多次只发一次
  3. 不同 key：不同 panic 各自告警
  4. 窗口过期：60s 后同一 panic 可重新告警
  5. Extra 拼接：报告体中出现调用方上下文
  6. sender 隔离：messageimpl 自身 panic / 返回 error 不影响主流程
  7. nil 输入：no-op，不写日志不发告警
  8. dedup key 稳定性：白盒验证 sha1 输入设计

通过 mockMessage 替换全局 IMessage 实现来观测告警行为，
通过直接操纵 panicDedupMap 的时间戳来避免真实等待 60s。

并发同步说明：
  asyncSendPanicAlarm 内部 fire-and-forget 启动 goroutine 调用 SendPanicAlarm，
  该 goroutine 会读取全局 Message 变量。如果 cleanup 直接执行 `Message = old`
  而不等 goroutine 退出，race detector 会报告读写竞争。所以 mock 上挂了 pending
  原子计数：goroutine 进入 SendPanicAlarm 时 +1、退出时 -1（defer 保证 panic
  路径也归零）；cleanup 通过 atomic.Load 等到 pending=0 再写 Message=old，
  atomic 操作天然建立 happens-before 边，消除 race。

@Author wanlizhan
@Date   2026-06-01
*/

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockMessage 是 IMessage 的测试替身，记录每次调用，可控延迟/错误/panic。
type mockMessage struct {
	mu       sync.Mutex
	calls    atomic.Int32 // 成功完成的调用次数（panic 路径不计）
	pending  atomic.Int32 // 进行中的 SendPanicAlarm 数（含 panic 路径）
	panicked atomic.Int32 // 主动 panic 的次数
	titles   []string
	bodies   []string
	delay    time.Duration // 模拟慢 webhook
	err      error         // SendPanicAlarm 返回的错误
	panicMsg string        // 非空时 SendPanicAlarm 主动 panic
}

func (m *mockMessage) SendSystemAlarm(_, _ string) error { return nil }

func (m *mockMessage) SendNotify(_, _ string) error { return nil }

func (m *mockMessage) SendPanicAlarm(title, body string) error {
	m.pending.Add(1)
	defer m.pending.Add(-1)

	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.panicMsg != "" {
		// 在 panic 之前打点，便于测试用 waitFor 同步到"goroutine 已进入并执行到这里"。
		// 注意 defer pending.Add(-1) 在 panic 路径上也会触发。
		m.panicked.Add(1)
		panic(m.panicMsg)
	}

	m.calls.Add(1)
	m.mu.Lock()
	m.titles = append(m.titles, title)
	m.bodies = append(m.bodies, body)
	m.mu.Unlock()
	return m.err
}

func (m *mockMessage) callCount() int { return int(m.calls.Load()) }

func (m *mockMessage) firstBody() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.bodies) == 0 {
		return ""
	}
	return m.bodies[0]
}

// installMockMessage 注入 mock 并清空 dedup map。
// 使用 t.Cleanup 等异步 goroutine 退出后再恢复全局 Message，避免 data race。
// recover.go 里 panicDedupMap 是包级变量，每次也都重置，避免测试间互相污染。
func installMockMessage(t *testing.T) *mockMessage {
	t.Helper()
	old := Message
	mock := &mockMessage{}
	Message = mock
	resetPanicDedup()

	t.Cleanup(func() {
		// 等所有 async send goroutine 退出。等待靠 atomic.Load，与 goroutine 内
		// pending.Add(-1) 形成 happens-before，保证后续 Message=old 写入安全。
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if mock.pending.Load() == 0 {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		Message = old
		resetPanicDedup()
	})
	return mock
}

func resetPanicDedup() {
	panicDedupMap.Range(func(k, _ any) bool {
		panicDedupMap.Delete(k)
		return true
	})
}

// waitFor 在 timeout 内轮询 cond，超时则 t.Fatalf。
// 用 polling 而非 channel 是因为我们要观察的状态在 mock 内部，
// 不想为单测污染 mock 的接口语义（生产路径不需要 chan 通知）。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("waitFor timeout: %s", msg)
}

// TestPanicLog_AsyncNonBlocking 验证异步发送：即使 webhook 很慢，PanicLog 立即返回。
//
// 这是改造的核心收益——同步实现下，handler goroutine 会在 webhook 上被绑死 200ms+，
// panic 风暴时整个进程的请求处理能力被打掉。
func TestPanicLog_AsyncNonBlocking(t *testing.T) {
	mock := installMockMessage(t)
	mock.delay = 200 * time.Millisecond

	start := time.Now()
	_ = PanicLog("nil ptr deref")
	elapsed := time.Since(start)

	// 留充足 buffer：异步路径里只跑去重判断 + go 关键字，远小于 mock.delay。
	// 50ms 阈值在 CI 上也很宽松。
	if elapsed > 50*time.Millisecond {
		t.Fatalf("PanicLog 被阻塞: 耗时 %v，远高于预期（mock.delay=%v 不应同步等待）", elapsed, mock.delay)
	}

	// 确认告警最终还是发出去了，不是被错误地丢弃
	waitFor(t, 2*time.Second, func() bool { return mock.callCount() == 1 }, "等待异步告警发送")
}

// TestPanicLog_DedupHit 验证窗口内去重：同一 panic 连续 5 次只发 1 条告警。
func TestPanicLog_DedupHit(t *testing.T) {
	mock := installMockMessage(t)

	for range 5 {
		_ = PanicLog("same boom")
	}

	// 等首次发送完成
	waitFor(t, 2*time.Second, func() bool { return mock.callCount() >= 1 }, "至少一次发送")
	// 再 sleep 一段时间，确认后续 4 次确实没有溢出到 mock
	time.Sleep(50 * time.Millisecond)

	if got := mock.callCount(); got != 1 {
		t.Fatalf("dedup 未生效: 期望 1 次告警，实际 %d 次", got)
	}
}

// TestPanicLog_DedupDifferentKey 验证不同 panic 产生不同 dedup key，分别告警。
//
// 这点很重要：如果 dedup 用了过宽的 key（比如只用类型/只用文件名），
// 多种独立故障会被合并成一条，运维拿到的告警信息严重失真。
func TestPanicLog_DedupDifferentKey(t *testing.T) {
	mock := installMockMessage(t)

	_ = PanicLog("boom A")
	_ = PanicLog("boom B")
	_ = PanicLog("boom C")

	waitFor(t, 2*time.Second, func() bool { return mock.callCount() == 3 },
		"3 个 unique panic 应分别告警 3 次")
}

// TestPanicLog_DedupWindowExpiry 验证窗口过期后同一 panic 重新可发。
//
// 直接把 dedup map 中所有项的时间戳调到窗口外，避免真实 sleep 60s 拖慢 CI。
// 这种"通过包内可见性操纵状态"是合理的白盒测试手段——它依赖
// panicDedupMap 是包级 sync.Map 这一实现细节，但同一个包内验证内部行为是测试的本职。
func TestPanicLog_DedupWindowExpiry(t *testing.T) {
	mock := installMockMessage(t)

	_ = PanicLog("recurring boom")
	waitFor(t, 2*time.Second, func() bool { return mock.callCount() == 1 }, "首次告警")

	// 把所有项时间戳推到窗口外
	stale := time.Now().Unix() - panicDedupWindowSec - 1
	panicDedupMap.Range(func(k, _ any) bool {
		panicDedupMap.Store(k, stale)
		return true
	})

	_ = PanicLog("recurring boom")
	waitFor(t, 2*time.Second, func() bool { return mock.callCount() == 2 },
		"窗口过期后应重新告警，期望 2 次")
}

// TestPanicLogWithExtra_BodyContainsExtra 验证 extra 被正确拼进告警 body，
// 同时 body 包含必要的结构化字段（Service/Stack/错误信息）。
func TestPanicLogWithExtra_BodyContainsExtra(t *testing.T) {
	mock := installMockMessage(t)

	extra := "POST /api/v1/test"
	errmsg := PanicLogWithExtra("custom boom", extra)

	if !strings.Contains(errmsg, "custom boom") {
		t.Fatalf("PanicLog 返回值应包含原始 panic 信息，实际: %s", errmsg)
	}

	waitFor(t, 2*time.Second, func() bool { return mock.callCount() == 1 }, "等待告警发送")

	body := mock.firstBody()
	wants := []string{
		"=== Panic Report ===",
		"Service:",
		"Host:",
		"Time:",
		"Extra:    " + extra,
		">> Error",
		"encounter panic: custom boom",
		">> Stack",
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("告警 body 缺失片段 %q\n实际 body:\n%s", w, body)
		}
	}
}

// TestPanicLog_NoExtraNoExtraField 验证 extra 为空串时 body 不出现 "Extra:" 行（避免空字段噪音）。
func TestPanicLog_NoExtraNoExtraField(t *testing.T) {
	mock := installMockMessage(t)

	_ = PanicLog("plain boom")
	waitFor(t, 2*time.Second, func() bool { return mock.callCount() == 1 }, "等待告警发送")

	body := mock.firstBody()
	if strings.Contains(body, "Extra:") {
		t.Errorf("空 extra 不应在 body 出现 Extra: 字段\nbody:\n%s", body)
	}
}

// TestPanicLog_SenderPanicIsolated 验证 messageimpl 自身崩溃不会扩散：
//   - 不会让 PanicLog 主调用 panic
//   - 不会触发 PanicLog → asyncSendPanicAlarm → 又一次 PanicLog 的递归
//
// 这条防线由 asyncSendPanicAlarm 内层的 defer recover() 提供。
// mock 在 panic 前 panicked.Add(1)，测试用 waitFor 等到该计数 ≥1
// 来同步"goroutine 已经走过 SendPanicAlarm 的 panic 点"。
func TestPanicLog_SenderPanicIsolated(t *testing.T) {
	mock := installMockMessage(t)
	mock.panicMsg = "messageimpl crashed"

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PanicLog 把 sender 的 panic 透传到了主流程: %v", r)
		}
	}()

	_ = PanicLog("trigger sender panic")

	// 用 atomic 计数同步，而不是 time.Sleep。
	// 这样 cleanup 里 pending=0 的等待真正能保证之前所有读取 Message 都完成。
	waitFor(t, 2*time.Second, func() bool { return mock.panicked.Load() >= 1 },
		"等 sender goroutine 进入 panic 路径")
}

// TestPanicLog_SenderErrorTolerated 验证 sender 返回 error（如飞书 webhook 429）不影响主流程。
func TestPanicLog_SenderErrorTolerated(t *testing.T) {
	mock := installMockMessage(t)
	mock.err = errors.New("feishu webhook 429 too many requests")

	got := PanicLog("error path")
	if !strings.Contains(got, "error path") {
		t.Fatalf("PanicLog 返回值不正确: %s", got)
	}
	waitFor(t, 2*time.Second, func() bool { return mock.callCount() == 1 },
		"sender 返回 error 但仍应尝试调用一次")
}

// TestPanicLog_NilNoop 验证 r==nil 是 no-op：不写日志、不发告警、返回空串。
//
// 必要性：CatchPanic 内部 if recover() != nil 已经过滤了 nil，
// 但 PanicLog 作为公开 API 可能被业务代码直接调用，nil 兜底防止脏告警。
func TestPanicLog_NilNoop(t *testing.T) {
	mock := installMockMessage(t)

	got := PanicLog(nil)
	if got != "" {
		t.Fatalf("PanicLog(nil) 应返回空串，实际 %q", got)
	}
	// 留时间让可能错误产生的 goroutine 跑完
	time.Sleep(20 * time.Millisecond)
	if c := mock.callCount(); c != 0 {
		t.Fatalf("PanicLog(nil) 不应触发告警，实际发送 %d 次", c)
	}
}

// TestPanicDedupKey_Stable 验证 dedup key 的稳定性：
// 同样的 (errmsg, stack) 每次哈希结果一致；不同输入 key 不冲突。
// 这是白盒测试，直接验证内部函数。
func TestPanicDedupKey_Stable(t *testing.T) {
	stack1 := "frame1\nfile1.go:10\nframe2\nfile2.go:20\n"
	stack2 := "frameX\nfileX.go:99\n"

	if panicDedupKey("a", stack1) != panicDedupKey("a", stack1) {
		t.Fatalf("相同输入 key 应稳定")
	}
	if panicDedupKey("a", stack1) == panicDedupKey("b", stack1) {
		t.Fatalf("不同 errmsg 不应产生相同 key")
	}
	if panicDedupKey("a", stack1) == panicDedupKey("a", stack2) {
		t.Fatalf("不同 stack 不应产生相同 key")
	}
}
