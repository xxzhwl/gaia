// Package asynctask 注释
// @author wanlizhan
// @created 2026/05/27
package asynctask

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
)

// AlarmThrottle 在固定时间窗口内对相同 key 的告警进行去重，避免告警风暴。
// 默认窗口 5 分钟；同一 key 在窗口内首次触发会真实发送（并附 [N x ...] 聚合数），
// 之后命中只递增计数，到下一个窗口起再发送一次（携带聚合数）。
type AlarmThrottle struct {
	window time.Duration
	mu     sync.Mutex
	state  map[string]*alarmState
}

type alarmState struct {
	firstTime time.Time
	lastTitle string
	hit       int64
	pending   bool   // 是否还有未发送的聚合
	lastBody  string // 最近一次的 body，用于聚合发送
}

// NewAlarmThrottle 创建一个告警限流器；window<=0 时使用默认 5min。
func NewAlarmThrottle(window time.Duration) *AlarmThrottle {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &AlarmThrottle{
		window: window,
		state:  make(map[string]*alarmState),
	}
}

// Send 发送告警；同一窗口内同 key 的告警只发首次，其余仅累加计数，
// 在下一窗口的下一次调用时输出聚合信息。
func (a *AlarmThrottle) Send(title, content string) {
	if a == nil {
		_ = gaia.SendSystemAlarm(title, content)
		return
	}
	key := alarmKey(title, content)
	now := time.Now()

	a.mu.Lock()
	st, ok := a.state[key]
	if !ok || now.Sub(st.firstTime) > a.window {
		// 新窗口：发出去；若上一个窗口有 pending hit，附带聚合数
		var aggregateNote string
		if ok && st.hit > 1 {
			aggregateNote = fmt.Sprintf("\n[聚合] 上一窗口期间(%s)内同类告警共 %d 次（已抑制）",
				a.window.String(), st.hit-1)
		}
		a.state[key] = &alarmState{firstTime: now, lastTitle: title, hit: 1}
		a.mu.Unlock()
		m := GetMetrics()
		m.AlarmFired.Add(context.Background(), 1)
		m.localCounters.alarmFired.Add(1)
		_ = gaia.SendSystemAlarm(title, content+aggregateNote)
		return
	}

	// 命中已有窗口：仅计数
	st.hit++
	st.lastBody = content
	st.pending = true
	a.mu.Unlock()
	m := GetMetrics()
	m.AlarmSuppressed.Add(context.Background(), 1)
	m.localCounters.alarmSuppress.Add(1)
}

// Flush 主动 flush 当前窗口聚合的所有告警（用于退出前结算）。
func (a *AlarmThrottle) Flush() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, st := range a.state {
		if st.pending && st.hit > 1 {
			_ = gaia.SendSystemAlarm(st.lastTitle,
				fmt.Sprintf("[Flush 聚合告警] 共 %d 次\n最近一次内容：%s", st.hit, st.lastBody))
			st.pending = false
		}
		_ = k
	}
}

func alarmKey(title, content string) string {
	h := sha1.New()
	h.Write([]byte(title))
	h.Write([]byte{'\x00'})
	// 仅取前 256 字符做 hash，避免每次 trace_id 不同导致永远不命中
	if len(content) > 256 {
		content = content[:256]
	}
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}
