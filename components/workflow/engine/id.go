package engine

import (
	"crypto/rand"
	"fmt"
	"sync/atomic"
	"time"
)

// IDGenerator 生成带业务前缀的唯一 ID。
type IDGenerator interface {
	Next(prefix string) string
}

// SequenceIDGenerator 生成进程内递增 ID，主要用于测试。
type SequenceIDGenerator struct {
	seq atomic.Uint64
}

// Next 返回下一个递增 ID。
func (g *SequenceIDGenerator) Next(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, g.seq.Add(1))
}

// RandomIDGenerator 生成随机 ID，适合持久化运行时。
type RandomIDGenerator struct{}

// Next 返回下一个随机 ID。
func (g RandomIDGenerator) Next(prefix string) string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%x", prefix, buf[:])
}
