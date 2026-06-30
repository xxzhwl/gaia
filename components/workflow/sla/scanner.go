// Package sla 提供工作流任务超时扫描能力。
package sla

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia/components/workflow/engine"
)

// RuntimePort 定义 SLA 扫描器需要调用的运行时端口。
type RuntimePort interface {
	ScanTimeoutTasks(ctx context.Context, req engine.ScanTimeoutTasksRequest) (engine.ScanTimeoutTasksResult, error)
}

// Scanner 定时扫描已到期任务，并通过运行时写入 SLA/通知 outbox。
type Scanner struct {
	runtime      RuntimePort
	pollInterval time.Duration
	batchSize    int
	running      atomic.Bool
}

// Option 用于配置 Scanner。
type Option func(*Scanner)

// WithPollInterval 设置扫描间隔。
func WithPollInterval(interval time.Duration) Option {
	return func(s *Scanner) {
		if interval > 0 {
			s.pollInterval = interval
		}
	}
}

// WithBatchSize 设置每次扫描的最大任务数。
func WithBatchSize(batchSize int) Option {
	return func(s *Scanner) {
		if batchSize > 0 {
			s.batchSize = batchSize
		}
	}
}

// New 创建 SLA 扫描器。
func New(runtime RuntimePort, opts ...Option) *Scanner {
	s := &Scanner{
		runtime:      runtime,
		pollInterval: 30 * time.Second,
		batchSize:    100,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Scan 执行一次超时任务扫描。
func (s *Scanner) Scan(ctx context.Context) (engine.ScanTimeoutTasksResult, error) {
	if s == nil || s.runtime == nil {
		return engine.ScanTimeoutTasksResult{}, fmt.Errorf("sla scanner runtime is nil")
	}
	return s.runtime.ScanTimeoutTasks(ctx, engine.ScanTimeoutTasksRequest{
		Now:   time.Now().UTC(),
		Limit: s.batchSize,
	})
}

// Run 按扫描间隔持续执行 SLA 扫描，直到上下文结束。
func (s *Scanner) Run(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return fmt.Errorf("sla scanner is already running")
	}
	defer s.running.Store(false)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		if _, err := s.Scan(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
