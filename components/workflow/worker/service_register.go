package worker

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/workflow/automation"
)

// RegisterService 用一份 automation.Service 声明完成"自动服务注册"。
//
// 这是**唯一**的注册入口：业务方只需
//  1. 在业务包 init() 或启动阶段调用 gaia.RegisterProxy(theme, serviceName, proxy)；
//  2. 传入 Service{ Themes: []string{...} } 让 worker 自动展开。
//
// worker 会：
//   - 遍历 svc.Themes，用 gaia.GetServiceProxies(theme) 拿到每个 (theme,serviceName)->proxy；
//   - 反射列出 proxy 上全部**导出**方法，每个方法注册为一个 automation.Task
//     （taskKey = snake_case(方法名)，theme = 当前 theme，description = 方法源码注释）；
//   - 把展开好的 Tasks 塞回 svc.Tasks 返回。
//
// 注意：本方法不会向远端 engine 发送注册请求——那属于传输层职责。调用方拿到返回值后，
// 用 wfgrpc.Client.RegisterAutomationService 或 HTTP POST /automation/services 提交即可。
func (w *Worker) RegisterService(svc automation.Service) (automation.Service, error) {
	if w == nil || w.catalog == nil {
		return automation.Service{}, fmt.Errorf("workflow worker catalog is nil")
	}
	svc.ID = strings.TrimSpace(svc.ID)
	if svc.ID == "" {
		return automation.Service{}, fmt.Errorf("automation service id is required")
	}
	if len(svc.Themes) == 0 {
		return automation.Service{}, fmt.Errorf("automation service %s requires at least one theme in Themes", svc.ID)
	}
	if svc.RegisteredAt.IsZero() {
		svc.RegisteredAt = time.Now().UTC()
	}
	svc.UpdatedAt = time.Now().UTC()

	if err := w.autoRegisterFromThemes(svc.Themes); err != nil {
		return automation.Service{}, err
	}

	// 从 catalog 里拿最终的 Task 列表并回填。
	svc.Tasks = w.catalog.Tasks(svc.ID, taskEndpointForProtocol(svc))
	return svc, nil
}

// autoRegisterFromThemes 扫描 gaia.ProxyRouter 中给定 themes 下的全部 (service, proxy)，
// 把 proxy 的每个导出方法注册为 automation task。
//
// 同一 (theme,serviceName,methodName) 已存在时会被 MethodCatalog 覆盖（幂等）。
func (w *Worker) autoRegisterFromThemes(themes []string) error {
	seen := map[string]struct{}{}
	for _, theme := range themes {
		theme = strings.TrimSpace(theme)
		if theme == "" {
			continue
		}
		proxies := gaia.GetServiceProxies(theme)
		if len(proxies) == 0 {
			gaia.WarnF("workflow worker: theme %q 下没有已注册的 proxy（是否遗漏 gaia.RegisterProxy 调用？）", theme)
			continue
		}
		// 保证注册顺序稳定（便于日志/展示）。
		services := make([]string, 0, len(proxies))
		for name := range proxies {
			services = append(services, name)
		}
		sort.Strings(services)

		for _, serviceName := range services {
			proxy := proxies[serviceName]
			methods := exportedMethodNames(proxy)
			for _, methodName := range methods {
				key := snakeCase(methodName)
				dedupKey := theme + "|" + key
				if _, ok := seen[dedupKey]; ok {
					continue
				}
				seen[dedupKey] = struct{}{}
				if err := w.catalog.RegisterProxyMethod(key, serviceName, proxy, methodName,
					automation.WithTheme(theme),
					automation.WithTaskName(humanize(methodName)),
				); err != nil {
					return fmt.Errorf("auto register %s.%s: %w", serviceName, methodName, err)
				}
				gaia.InfoF("workflow worker: 自动注册 %s / %s.%s -> task %q", theme, serviceName, methodName, key)
			}
		}
	}
	return nil
}

// exportedMethodNames 返回 proxy 上所有导出方法名（按名字排序）。
func exportedMethodNames(proxy any) []string {
	if proxy == nil {
		return nil
	}
	value := reflect.ValueOf(proxy)
	if !value.IsValid() {
		return nil
	}
	typ := value.Type()
	names := make([]string, 0, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if !m.IsExported() {
			continue
		}
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}

// snakeCase 把 CamelCase 方法名转成 snake_case task key。
//
// 例：NormalizeOrder -> normalize_order；HTTPStart -> http_start（连续大写作为一个单词）。
func snakeCase(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			// 单词边界：前一个是小写或数字；或前一个是大写但下一个是小写（末尾大写块进入新单词）。
			if i > 0 {
				prev := runes[i-1]
				next := rune(0)
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && next != 0 && unicode.IsLower(next)) {
					b.WriteByte('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// humanize 把方法名转成人类友好展示名："NormalizeOrder" -> "normalize order"。
func humanize(name string) string {
	return strings.ReplaceAll(snakeCase(name), "_", " ")
}

// taskEndpointForProtocol 返回自动注册时给 Task.Endpoint 的默认值。
//   - gRPC 服务需要显式端点（BaseURL 即 host:port），否则 dispatcher 会拼错；
//   - HTTP 服务留空由 dispatcher 用 baseURL/<taskKey> 兜底。
func taskEndpointForProtocol(svc automation.Service) string {
	if automation.NormalizeProtocol(svc.Protocol) == automation.ProtocolGRPC {
		return svc.BaseURL
	}
	return ""
}
