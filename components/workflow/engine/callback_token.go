package engine

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"

	"github.com/xxzhwl/gaia/components/workflow/domain"
)

// newCallbackToken 生成不可预测的回调令牌，用于校验外部 worker 的任务完成回调。
//
// 令牌使用加密安全随机源生成；在极端情况下随机源不可用时，回退到带前缀的 ID 生成器，
// 以保证任务创建流程不被阻断。
func newCallbackToken(ids IDGenerator) string {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return ids.Next("cb")
	}
	return fmt.Sprintf("cb_%x", buf[:])
}

// verifyCallbackToken 校验外部回调路径携带的回调令牌。
//
// 约定：仅当 provided 非空（即请求来自不可信的外部回调路径）时才强制校验；
// provided 为空表示请求来自宿主鉴权中间件保护的可信内部路径，不做令牌校验。
// 比对使用常量时间算法，避免时序侧信道泄漏。
func verifyCallbackToken(task domain.Task, provided string) error {
	if provided == "" {
		return nil
	}
	if task.CallbackToken == "" {
		return fmt.Errorf("task %s does not accept callback token completion", task.ID)
	}
	if subtle.ConstantTimeCompare([]byte(task.CallbackToken), []byte(provided)) != 1 {
		return fmt.Errorf("task %s callback token mismatch", task.ID)
	}
	return nil
}
