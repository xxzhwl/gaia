// Package jobs 包注释
// @author wanlizhan
// @created 2024/6/20
package jobs

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/xxzhwl/gaia"
)

var CronServiceMap map[string]any
var CronServiceMethodsMap map[string][]string
var locker sync.RWMutex

func init() {
	CronServiceMap = map[string]any{}
	CronServiceMethodsMap = map[string][]string{}
}

func makeCronServiceKey(systemName, serviceName string) string {
	return fmt.Sprintf("%s/%s", systemName, serviceName)
}

func normalizeSystemName(systemName string) string {
	if systemName == "" {
		return gaia.GetSystemEnName()
	}
	return systemName
}

func RegisterCronService(serviceName string, service any) error {
	systemName := gaia.GetSystemEnName()
	return RegisterCronServiceWithSystem(systemName, serviceName, service)
}

// RegisterCronServiceWithSystem 注册指定系统下可执行的 cron service。
func RegisterCronServiceWithSystem(systemName, serviceName string, service any) error {
	locker.Lock()
	defer locker.Unlock()
	systemName = normalizeSystemName(systemName)
	if serviceName == "" {
		return errors.New("empty cron service name")
	}
	if service == nil {
		return errors.New("nil cron service: " + serviceName)
	}
	key := makeCronServiceKey(systemName, serviceName)
	if _, ok := CronServiceMap[key]; ok {
		return errors.New("duplicated cron service: " + key)
	}
	CronServiceMap[key] = service
	CronServiceMethodsMap[key] = ListCronServiceMethods(service)
	return nil
}

func GetCronService(serviceName string) (any, error) {
	return GetCronServiceForSystem(gaia.GetSystemEnName(), serviceName)
}

func GetCronServiceForSystem(systemName, serviceName string) (any, error) {
	systemName = normalizeSystemName(systemName)
	locker.RLock()
	defer locker.RUnlock()
	key := makeCronServiceKey(systemName, serviceName)
	if v, ok := CronServiceMap[key]; ok {
		return v, nil
	}
	return nil, errors.New("no such cron service: " + key)
}

// RegisterCronServiceMeta 注册管理面可见的 service 元数据，不参与本进程任务执行。
func RegisterCronServiceMeta(systemName, serviceName string, methods []string) error {
	systemName = normalizeSystemName(systemName)
	if serviceName == "" {
		return errors.New("empty cron service name")
	}
	locker.Lock()
	defer locker.Unlock()
	key := makeCronServiceKey(systemName, serviceName)
	CronServiceMethodsMap[key] = normalizeMethods(methods)
	return nil
}

// ListCronServiceMethods enumerates exported methods on a cron service struct.
func ListCronServiceMethods(service any) []string {
	if service == nil {
		return nil
	}
	t := reflect.TypeOf(service)
	var methods []string
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if !m.IsExported() {
			continue
		}
		methods = append(methods, m.Name)
	}
	slices.Sort(methods)
	return methods
}

type RegisteredCronService struct {
	SystemName  string   `json:"system_name"`
	ServiceName string   `json:"service_name"`
	Methods     []string `json:"methods"`
	Executable  bool     `json:"executable"`
}

func ListRegisteredCronServices(systemName string) []RegisteredCronService {
	locker.RLock()
	defer locker.RUnlock()

	seen := make(map[string]struct{})
	services := make([]RegisteredCronService, 0, len(CronServiceMethodsMap))
	add := func(key string, executable bool) {
		if _, ok := seen[key]; ok {
			return
		}
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			return
		}
		if systemName != "" && parts[0] != systemName {
			return
		}
		seen[key] = struct{}{}
		services = append(services, RegisteredCronService{
			SystemName:  parts[0],
			ServiceName: parts[1],
			Methods:     slices.Clone(CronServiceMethodsMap[key]),
			Executable:  executable,
		})
	}
	for key := range CronServiceMap {
		add(key, true)
	}
	for key := range CronServiceMethodsMap {
		_, executable := CronServiceMap[key]
		add(key, executable)
	}
	return services
}

func normalizeMethods(methods []string) []string {
	if len(methods) == 0 {
		return nil
	}
	result := make([]string, 0, len(methods))
	seen := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		if method == "" {
			continue
		}
		if _, ok := seen[method]; ok {
			continue
		}
		seen[method] = struct{}{}
		result = append(result, method)
	}
	slices.Sort(result)
	return result
}
