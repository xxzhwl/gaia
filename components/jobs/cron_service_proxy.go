// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/20
package jobs

import (
	"errors"
	"sync"
)

var CronServiceMap map[string]any
var locker sync.RWMutex

func init() {
	CronServiceMap = map[string]any{}
}

func RegisterCronService(serviceName string, service any) error {
	locker.Lock()
	defer locker.Unlock()
	if _, ok := CronServiceMap[serviceName]; ok {
		return errors.New("duplicated cron service")
	}
	CronServiceMap[serviceName] = service
	return nil
}

func GetCronService(serviceName string) (any, error) {
	locker.Lock()
	defer locker.Unlock()
	if v, ok := CronServiceMap[serviceName]; ok {
		return v, nil
	}
	return nil, errors.New("no such cron service")
}
