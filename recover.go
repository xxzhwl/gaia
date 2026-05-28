package gaia

/*
错误拦截处理逻辑
@Author wanlizhan
@Date 2023-03-23
*/

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// CatchPanic 捕获并处理低级别的panic，此类panic通常是因为出现不可预料的异常导致
// 特别是在API服务中，如果代码不够健壮，导致panic，又没有recover捕获，则整个服务进程将崩溃
// 特别注意: panic不能跨goroutine捕获，因此，所创建的每个goroutine，都需要进行异常捕获。
// 该方法处理外层recover传入的参数，并进行告警和运行时错误日志记录，方便事后排查问题
// 函数 CatchPanic在上层逻辑必须以 defer CatchPanic()的形式调用，不可在闭包内调用
func CatchPanic() {
	if r := recover(); r != nil {
		PanicLog(r)
	}
}

// PanicLog 记录Panic异常日志，并返回panic异常信息。
// 行为等价于 PanicLogWithExtra(r, "")，签名保持向后兼容。
func PanicLog(r any) string {
	return PanicLogWithExtra(r, "")
}

// PanicLogWithExtra 在 PanicLog 基础上接受一段调用方上下文（extra）。
//
// 推荐 extra 内容：
//   - HTTP handler:  "POST /api/v1/users"
//   - asynctask:     "task=SendEmail args=..."
//   - cron job:      "job=DailyReport"
//
// 设计要点：
//  1. 告警消息体结构化：拼上 Service / Host / Time / TraceId / LogId / Extra / Stack，
//     运维侧拿到飞书消息能直接定位到服务实例 + 链路 + 业务上下文，不用再回查日志。
//  2. 告警异步发送：SendPanicAlarm 的 IMessage 实现一般是 HTTP POST 到飞书 webhook，
//     P99 数百毫秒。同步走会拖慢 panic 路径的回包，panic 风暴时还会把 worker
//     goroutine 全部堵在 webhook 上，反过来恶化业务。这里 fire-and-forget。
//  3. 告警去重：飞书自定义机器人限频 100 次/分钟，连环 panic 不去重直接把告警通道打爆，
//     超限后 webhook 返回 429，业务也跟着遭殃。窗口内同一 panic（msg + stack 前 5 帧
//     哈希）只发一次，但日志依然每次都写——避免漏掉错误现场。
//  4. 异步发送的 goroutine 内必须自带 recover：messageimpl 自身崩溃不能拖死进程。
func PanicLogWithExtra(r any, extra string) string {
	if r == nil {
		return ""
	}
	errmsg := fmt.Sprintf("encounter panic: %v", r)

	// stack 限制 30 帧。飞书机器人卡片单条 30000 字符上限，正常 stack 10 帧已足够定位，
	// 30 帧给深递归/中间件链留余量；再多出现的概率低，截断 vs 消息被拒丢失，前者更稳。
	stack := GetStackFramesString(2, 30)

	body := buildPanicReport(errmsg, stack, extra)

	// 文本日志：每次都写，不受去重影响（去重只针对告警通道）
	Log(LogErrorLevel, body)

	// 异步发送 + 去重
	asyncSendPanicAlarm("RuntimePanic", body, errmsg, stack)

	return errmsg
}

// buildPanicReport 把 panic 关键上下文拼成统一格式的可读文本。
// 字段顺序按"先定位实例、再定位链路、再看错误"组织，方便人眼扫读。
func buildPanicReport(errmsg, stack, extra string) string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}

	var sb strings.Builder
	sb.Grow(512 + len(stack))
	sb.WriteString("=== Panic Report ===\n")
	sb.WriteString("Service:  ")
	sb.WriteString(GetSystemEnName())
	sb.WriteString("\nHost:     ")
	sb.WriteString(host)
	sb.WriteString("\nTime:     ")
	sb.WriteString(Date(DateTimeMillsFormat))
	if traceID := NewContextTrace().GetTraceId(); traceID != "" {
		sb.WriteString("\nTraceId:  ")
		sb.WriteString(traceID)
	}
	if logID := NewContextTrace().GetId(); logID != "" {
		sb.WriteString("\nLogId:    ")
		sb.WriteString(logID)
	}
	if extra != "" {
		sb.WriteString("\nExtra:    ")
		sb.WriteString(extra)
	}
	sb.WriteString("\n\n>> Error\n")
	sb.WriteString(errmsg)
	sb.WriteString("\n\n>> Stack\n")
	sb.WriteString(stack)
	sb.WriteString("====================\n")
	return sb.String()
}

// ===== 告警去重 =====
//
// 同一 panic 在短时间内反复触发是常态（一个 nil 解引用 bug 在错误请求重试下能秒级
// 触发几百次），如果每次都推飞书：
//   a) 飞书机器人侧 100 次/分钟限频，超出后 webhook 返回 429；
//   b) 接收群被刷屏，运维实际上看不到任何信息。
// 因此引入"窗口内首次触发才发送"的去重。
//
// dedup key = sha1(errmsg + stack 前 5 帧)
//   - 用消息+部分 stack 而不是只用消息：相同 panic 类型出现在不同位置应分别告警；
//   - 取前 5 帧而不是全栈：减少行号偏移/inline 差异带来的抖动；
//   - 用 sha1 而不是直接拼字符串当 key：长 stack 哈希后 key 内存固定 40 字节，
//     防止恶意/异常 stack 把 map 内存撑大。
//
// 窗口：60s。比飞书限频窗口（60s/100 次）一致或更长，配合常见 panic 多样性
// （单次故障常见同时出现 1~5 种 panic）总告警量稳定低于限频。
const (
	panicDedupWindowSec int64 = 60
	panicDedupMaxItems  int   = 256 // 安全阀：极端场景下 unique panic 太多时整体重置
)

var panicDedupMap sync.Map // key: string(sha1 hex), val: int64(unix sec)

// asyncSendPanicAlarm 在去重通过后异步推送告警。
// 调用方所在 goroutine 不会被告警 IO 阻塞。
func asyncSendPanicAlarm(title, body, errmsg, stack string) {
	if !panicDedupAllow(errmsg, stack) {
		return
	}
	go func() {
		// 内层 recover：messageimpl 自身的 panic 不能反过来拖死调用方所在进程。
		// 注意不能直接调 CatchPanic()，否则会形成 PanicLog → asyncSendPanicAlarm
		// → CatchPanic → PanicLog 的递归告警风暴。
		defer func() {
			if rr := recover(); rr != nil {
				Log(LogErrorLevel, fmt.Sprintf("SendPanicAlarm 自身 panic: %v", rr))
			}
		}()
		if err := SendPanicAlarm(title, body); err != nil {
			Log(LogErrorLevel, fmt.Sprintf("SendPanicAlarm 失败: %s", err.Error()))
		}
	}()
}

// panicDedupAllow 返回 true 表示当前这次 panic 应该真正发送告警；
// false 表示落在窗口内、跳过本次推送（但日志仍会写）。
func panicDedupAllow(errmsg, stack string) bool {
	key := panicDedupKey(errmsg, stack)
	now := time.Now().Unix()

	if last, ok := panicDedupMap.Load(key); ok {
		if lastTs, ok2 := last.(int64); ok2 && now-lastTs < panicDedupWindowSec {
			return false
		}
	}
	panicDedupMap.Store(key, now)

	// 顺手清理过期项，避免长跑下 map 无界增长。
	// sync.Map.Range 在写入并发下不保证全量看到，但用于 GC 已足够：
	// 漏清的项最坏情况下会留到下一轮 Store 时再被扫到。
	cnt := 0
	panicDedupMap.Range(func(k, v any) bool {
		cnt++
		if ts, ok := v.(int64); ok && now-ts > panicDedupWindowSec {
			panicDedupMap.Delete(k)
		}
		// 极端兜底：unique panic 数量爆炸时整体收缩，防止 OOM。
		// 256 这个阈值远高于正常服务的 panic 多样性（通常 < 10 种）。
		if cnt > panicDedupMaxItems {
			panicDedupMap.Delete(k)
		}
		return true
	})

	return true
}

// panicDedupKey 计算 dedup key。
// 取 stack 前 5 帧（每帧 2 行：函数名 + 文件:行号 = 共 10 行）。
func panicDedupKey(errmsg, stack string) string {
	const headLines = 10
	idx := 0
	for range headLines {
		next := strings.IndexByte(stack[idx:], '\n')
		if next < 0 {
			idx = len(stack)
			break
		}
		idx += next + 1
	}
	head := stack
	if idx < len(stack) {
		head = stack[:idx]
	}
	h := sha1.Sum([]byte(errmsg + "\n" + head))
	return hex.EncodeToString(h[:])
}
