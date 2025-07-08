// Package asynctask 包注释
// @author wanlizhan
// @created 2024/5/8
package asynctask

import (
	"time"
)

func (s *Scheduler) Apply(options ...SchedulerOption) {
	for _, option := range options {
		option(s)
	}
}

func WithAlarmFunc(alarmFunc AlarmHandlerFunc) SchedulerOption {
	return func(temp *Scheduler) {
		temp.AlarmHandler = alarmFunc
	}
}

func WithWorkerNum(workerNum int) SchedulerOption {
	return func(temp *Scheduler) {
		temp.WorkerNum = workerNum
	}
}

func WithScanTaskInterval(duration time.Duration) SchedulerOption {
	return func(temp *Scheduler) {
		temp.ScanTaskInterval = duration
	}
}

func WithHearBeatInterval(duration time.Duration) SchedulerOption {
	return func(temp *Scheduler) {
		temp.HeartBeatInterval = duration
	}
}

func WithTaskIdChanLength(length int) SchedulerOption {
	return func(temp *Scheduler) {
		temp.TaskIdChanLength = length
	}
}

func WithScanTaskNum(num int) SchedulerOption {
	return func(temp *Scheduler) {
		temp.ScanTaskNum = num
	}
}

func WithPreHandler(handler PreHandlerFunc) SchedulerOption {
	return func(temp *Scheduler) {
		temp.PreHandler = handler
	}
}

func WithPostHandler(handler PostHandlerFunc) SchedulerOption {
	return func(temp *Scheduler) {
		temp.PostHandler = handler
	}
}
