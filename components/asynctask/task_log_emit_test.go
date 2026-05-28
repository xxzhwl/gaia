// Package asynctask 包注释
// @author wanlizhan
// @created 2026/6/1
package asynctask

import "testing"

// TestMapAsyncTaskStatusToPhase 锁定 status→phase 映射契约。
//
// async_task_log 的 phase 用于：
//   - "drop"    监控"已耗尽重试且仍失败"的任务（需要人工介入）
//   - "retry"   监控"短期波动可自愈"的任务
//   - "fail"    保留：未来如果 executor 出现"还能重试但本次记 fail"的语义
//   - "success" 吞吐基线
// fail / drop 区分的关键是：drop 需要立即告警，fail 只需要观察。
// 一旦映射改坏，告警规则会沉默或误告，因此用测试锁死。
func TestMapAsyncTaskStatusToPhase(t *testing.T) {
	cases := []struct {
		name   string
		status string
		info   TaskModel
		want   string
	}{
		{
			name:   "success",
			status: TaskStatusSuccess.String(),
			info:   TaskModel{},
			want:   "success",
		},
		{
			name:   "retry",
			status: TaskStatusRetry.String(),
			info:   TaskModel{},
			want:   "retry",
		},
		{
			name:   "failed-with-room → fail",
			status: TaskStatusFailed.String(),
			info:   TaskModel{TaskBaseInfo: TaskBaseInfo{MaxRetryTime: 3}, RetryTime: 0},
			want:   "fail",
		},
		{
			name:   "failed-no-room → drop",
			status: TaskStatusFailed.String(),
			info:   TaskModel{TaskBaseInfo: TaskBaseInfo{MaxRetryTime: 3}, RetryTime: 2},
			want:   "drop",
		},
		{
			name:   "failed-no-retry-config → fail",
			status: TaskStatusFailed.String(),
			info:   TaskModel{TaskBaseInfo: TaskBaseInfo{MaxRetryTime: 0}, RetryTime: 0},
			want:   "fail",
		},
		{
			name:   "passthrough unknown",
			status: "Weird",
			info:   TaskModel{},
			want:   "Weird",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mapAsyncTaskStatusToPhase(c.status, c.info); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestDisplayTaskName 验证 fallback 逻辑——TaskName 必须比 service.method 优先。
// ES 聚合 key 不稳的话告警面板就乱了。
func TestDisplayTaskName(t *testing.T) {
	if got := displayTaskName(TaskModel{TaskBaseInfo: TaskBaseInfo{TaskName: "send_email"}}); got != "send_email" {
		t.Errorf("want send_email, got %q", got)
	}
	m := TaskModel{TaskBaseInfo: TaskBaseInfo{ServiceName: "User", MethodName: "Notify"}}
	if got := displayTaskName(m); got != "User.Notify" {
		t.Errorf("want User.Notify, got %q", got)
	}
	if got := displayTaskName(TaskModel{}); got != "unknown" {
		t.Errorf("want unknown, got %q", got)
	}
}
