// Package buffer 包注释
// @author wanlizhan
// @created 2024/6/25
package buffer

import (
	"time"
)

const (
	DefaultBufferSize = 16384

	MinWorkerNumFloor   = 1
	MinWorkerNumTop     = 100
	DefaultMinWorkerNum = 2
	MaxWorkerNumFloor   = 50
	MaxWorkerNumTop     = 1000
	DefaultMaxWorkerNum = 50

	DefaultBatchScanInterval   = time.Millisecond * 50
	DefaultBatchPopDataNum     = 50
	DefaultBatchPopDataTimeOut = time.Millisecond * 30
	DefaultSleepLine           = time.Second * 10
)

type Handler func(data [][]byte) error

type Option func(bufInstance *DataBuffer)

func WithBufferLength(length int) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.bufferLength = length
	}
}

func WithWorkerNum(minNum, maxNum int) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.minWorkerNum = minNum
		bufInstance.maxWorkerNum = maxNum
	}
}

func WithHandler(h Handler) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.handler = h
	}
}

// WithDropOnFull 设置缓冲区满时是否直接丢弃（默认 false 表示阻塞）
// 与 WithPushTimeout 同时设置时，DropOnFull 优先生效。
func WithDropOnFull(drop bool) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.dropOnFull = drop
	}
}

// WithPushTimeout 设置 Push 在缓冲区满时的最长等待时间，超时后丢弃。
// timeout <= 0 表示永久阻塞（与历史行为一致）。
func WithPushTimeout(timeout time.Duration) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.pushTimeout = timeout
	}
}

func WithBatchScanInterval(interval time.Duration) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.batchScanInterval = interval
	}
}

func WithBatchPopDataNum(num int) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.batchPopDataNum = num
	}
}

func WithBatchPopDataTimeOut(interval time.Duration) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.batchPopDataTimeOut = interval
	}
}

func WithSleepLine(interval time.Duration) Option {
	return func(bufInstance *DataBuffer) {
		bufInstance.sleepLine = interval
	}
}
