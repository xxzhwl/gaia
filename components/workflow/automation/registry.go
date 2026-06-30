package automation

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// ProtocolHTTP 表示自动化 worker 使用 HTTP 协议接收任务。
	ProtocolHTTP = "http"
	// ProtocolGRPC 表示自动化 worker 使用 gRPC 协议接收任务。
	ProtocolGRPC = "grpc"
)

// Parameter 描述自动化任务的单个输入或输出参数。
type Parameter struct {
	Key          string
	Name         string
	Type         string
	Required     bool
	DefaultValue any
	Description  string
}

// Task 描述一个可被流程服务节点调用的自动化任务。
type Task struct {
	ServiceID    string
	Key          string
	Name         string
	Description  string
	Method       string
	Endpoint     string
	InputSchema  []Parameter
	OutputSchema []Parameter
}

// Service 描述一个提供 workflow 自动化任务能力的服务。
type Service struct {
	ID           string
	Name         string
	BaseURL      string
	HealthURL    string
	Protocol     string
	Version      string
	Tags         []string
	Tasks        []Task
	RegisteredAt time.Time
	UpdatedAt    time.Time
	TTLSeconds   int
}

// Registry 管理自动化服务注册、发现和 gaia:// 端点解析。
type Registry interface {
	Register(ctx context.Context, service Service) (Service, error)
	Unregister(ctx context.Context, serviceID string) error
	ListServices(ctx context.Context) ([]Service, error)
	ListTasks(ctx context.Context) ([]Task, error)
	GetTask(ctx context.Context, serviceID, taskKey string) (Task, error)
	ResolveTask(ctx context.Context, ref string) (Service, Task, error)
	ResolveEndpoint(ctx context.Context, ref string) (string, error)
}

// MemoryRegistry 是进程内自动化服务注册表。
type MemoryRegistry struct {
	mu       sync.RWMutex
	services map[string]Service
	now      func() time.Time
}

var defaultRegistry = NewMemoryRegistry()

// DefaultRegistry 返回进程级默认自动化服务注册表。
func DefaultRegistry() Registry {
	return defaultRegistry
}

// NewMemoryRegistry 创建进程内自动化服务注册表。
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		services: map[string]Service{},
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Register 注册或刷新一个自动化服务。
func (r *MemoryRegistry) Register(_ context.Context, service Service) (Service, error) {
	service.ID = strings.TrimSpace(service.ID)
	if service.ID == "" {
		return Service{}, fmt.Errorf("automation service id is required")
	}
	if strings.TrimSpace(service.BaseURL) == "" {
		return Service{}, fmt.Errorf("automation service baseURL is required")
	}
	service.Protocol = NormalizeProtocol(service.Protocol)
	now := r.now()
	if service.RegisteredAt.IsZero() {
		service.RegisteredAt = now
	}
	service.UpdatedAt = now
	for i := range service.Tasks {
		service.Tasks[i].ServiceID = service.ID
		service.Tasks[i].Key = strings.TrimSpace(service.Tasks[i].Key)
		if service.Tasks[i].Key == "" {
			return Service{}, fmt.Errorf("automation task key is required")
		}
		if service.Tasks[i].Method == "" {
			service.Tasks[i].Method = http.MethodPost
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[service.ID] = service
	return service, nil
}

// Unregister 注销自动化服务。
func (r *MemoryRegistry) Unregister(_ context.Context, serviceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.services, strings.TrimSpace(serviceID))
	return nil
}

// ListServices 查询未过期的自动化服务。
func (r *MemoryRegistry) ListServices(_ context.Context) ([]Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Service, 0, len(r.services))
	for _, service := range r.services {
		if r.expired(service) {
			continue
		}
		result = append(result, service)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

// ListTasks 查询全部未过期服务暴露的自动化任务。
func (r *MemoryRegistry) ListTasks(ctx context.Context) ([]Task, error) {
	services, err := r.ListServices(ctx)
	if err != nil {
		return nil, err
	}
	var result []Task
	for _, service := range services {
		for _, task := range service.Tasks {
			task.ServiceID = service.ID
			result = append(result, task)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ServiceID == result[j].ServiceID {
			return result[i].Key < result[j].Key
		}
		return result[i].ServiceID < result[j].ServiceID
	})
	return result, nil
}

// GetTask 查询指定服务下的自动化任务。
func (r *MemoryRegistry) GetTask(_ context.Context, serviceID, taskKey string) (Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	service, ok := r.services[strings.TrimSpace(serviceID)]
	if !ok || r.expired(service) {
		return Task{}, fmt.Errorf("automation service %s not found", serviceID)
	}
	for _, task := range service.Tasks {
		if task.Key == strings.TrimSpace(taskKey) {
			task.ServiceID = service.ID
			if task.Endpoint == "" {
				task.Endpoint = joinURL(service.BaseURL, task.Key)
			}
			return task, nil
		}
	}
	return Task{}, fmt.Errorf("automation task %s/%s not found", serviceID, taskKey)
}

// ResolveTask 将 gaia://service/task 解析成自动化服务和任务定义。
func (r *MemoryRegistry) ResolveTask(ctx context.Context, ref string) (Service, Task, error) {
	if !strings.HasPrefix(ref, "gaia://") {
		return Service{}, Task{}, fmt.Errorf("automation ref %q is not a gaia ref", ref)
	}
	serviceID, taskKey, err := ParseGaiaRef(ref)
	if err != nil {
		return Service{}, Task{}, err
	}
	r.mu.RLock()
	service, ok := r.services[serviceID]
	r.mu.RUnlock()
	if !ok || r.expired(service) {
		return Service{}, Task{}, fmt.Errorf("automation service %s not found", serviceID)
	}
	service.Protocol = NormalizeProtocol(service.Protocol)
	task, err := r.GetTask(ctx, serviceID, taskKey)
	if err != nil {
		return Service{}, Task{}, err
	}
	return service, task, nil
}

// ResolveEndpoint 将 gaia://service/task 解析成实际 HTTP 调用地址。
func (r *MemoryRegistry) ResolveEndpoint(ctx context.Context, ref string) (string, error) {
	if !strings.HasPrefix(ref, "gaia://") {
		return ref, nil
	}
	service, task, err := r.ResolveTask(ctx, ref)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(task.Endpoint) != "" {
		return task.Endpoint, nil
	}
	return joinURL(service.BaseURL, task.Key), nil
}

// NormalizeProtocol 将自动化服务协议归一化。
func NormalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ProtocolGRPC:
		return ProtocolGRPC
	default:
		return ProtocolHTTP
	}
}

// ParseGaiaRef 解析 gaia://service/task 自动化任务引用。
func ParseGaiaRef(ref string) (string, string, error) {
	parsed, err := url.Parse(ref)
	if err != nil {
		return "", "", err
	}
	serviceID := parsed.Host
	taskKey := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if serviceID == "" || taskKey == "" {
		return "", "", fmt.Errorf("invalid automation endpoint %q", ref)
	}
	return serviceID, taskKey, nil
}

func (r *MemoryRegistry) expired(service Service) bool {
	return service.TTLSeconds > 0 && r.now().After(service.UpdatedAt.Add(time.Duration(service.TTLSeconds)*time.Second))
}

func joinURL(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	path = strings.TrimLeft(path, "/")
	if path == "" {
		return baseURL
	}
	return baseURL + "/" + path
}
