// Package jobs 告警去重/限流。
// @author wanlizhan
// @created 2026/05/27
package jobs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
)

// JobAlarmThrottle 在固定时间窗口内对相同 key 的告警进行去重，避免告警风暴。
// 默认窗口 5 分钟。
type JobAlarmThrottle struct {
	window time.Duration
	mu     sync.Mutex
	state  map[string]*jobAlarmState
}

type jobAlarmState struct {
	firstTime time.Time
	lastTitle string
	hit       int64
	pending   bool
	lastBody  string
}

// NewJobAlarmThrottle 创建一个告警限流器；window<=0 时使用默认 5min。
func NewJobAlarmThrottle(window time.Duration) *JobAlarmThrottle {
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &JobAlarmThrottle{
		window: window,
		state:  make(map[string]*jobAlarmState),
	}
}

// Send 发送告警；同一窗口内同 key 的告警只发首次，其余仅累加计数。
func (a *JobAlarmThrottle) Send(title, content string) {
	if a == nil {
		_ = gaia.SendSystemAlarm(title, content)
		GetJobsMetrics().AlarmFired.Add(context.Background(), 1)
		GetJobsMetrics().localCounters.alarmFired.Add(1)
		return
	}
	key := jobAlarmKey(title, content)
	now := time.Now()

	a.mu.Lock()
	st, ok := a.state[key]
	if !ok || now.Sub(st.firstTime) > a.window {
		var aggregateNote string
		if ok && st.hit > 1 {
			aggregateNote = fmt.Sprintf("\n[聚合] 上一窗口期间(%s)内同类告警共 %d 次（已抑制）",
				a.window.String(), st.hit-1)
		}
		a.state[key] = &jobAlarmState{firstTime: now, lastTitle: title, hit: 1}
		a.mu.Unlock()
		GetJobsMetrics().AlarmFired.Add(context.Background(), 1)
		GetJobsMetrics().localCounters.alarmFired.Add(1)
		_ = gaia.SendSystemAlarm(title, content+aggregateNote)
		return
	}

	st.hit++
	st.lastBody = content
	st.pending = true
	a.mu.Unlock()
	GetJobsMetrics().AlarmSuppressed.Add(context.Background(), 1)
	GetJobsMetrics().localCounters.alarmSuppressed.Add(1)
}

// Flush 主动 flush 当前窗口聚合的所有告警。
func (a *JobAlarmThrottle) Flush() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, st := range a.state {
		if st.pending && st.hit > 1 {
			_ = gaia.SendSystemAlarm(st.lastTitle,
				fmt.Sprintf("[Flush 聚合告警] 共 %d 次\n最近一次内容：%s", st.hit, st.lastBody))
			st.pending = false
		}
	}
}

func jobAlarmKey(title, content string) string {
	h := sha1.New()
	h.Write([]byte(title))
	h.Write([]byte{'\x00'})
	if len(content) > 256 {
		content = content[:256]
	}
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}
