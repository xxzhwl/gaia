package domain

// PageRequest 表示分页请求参数。
type PageRequest struct {
	Page     int
	PageSize int
}

// PageResult 表示分页查询结果。
type PageResult[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

// DefinitionListFilter 表示流程定义列表查询条件。
type DefinitionListFilter struct {
	PageRequest
	Key    string
	Name   string
	Status DefinitionStatus
}

// InstanceListFilter 表示流程实例列表查询条件。
type InstanceListFilter struct {
	PageRequest
	DefinitionKey string
	Status        InstanceStatus
	// TenantID 非空时仅返回该租户的实例。通常由运行时从请求上下文自动注入，
	// 用于多租户数据隔离，避免跨租户枚举。
	TenantID string
}

// TaskListFilter 表示任务列表查询条件。
type TaskListFilter struct {
	PageRequest
	InstanceID string
	Status     TaskStatus
	Type       TaskType
	Assignee   string
	// TenantID 非空时仅返回该租户实例下的任务。通常由组件门面从请求上下文自动注入，
	// 避免多租户场景下跨租户枚举待办或自动化任务。
	TenantID string
}

// OutboxListFilter 表示 outbox 事件列表查询条件。
type OutboxListFilter struct {
	PageRequest
	EventType string
	Status    OutboxStatus
}

// NormalizePage 规范化分页参数，并限制最大页大小。
func NormalizePage(req PageRequest) PageRequest {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 10
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	return req
}

// PageOffset 计算分页查询的偏移量。
func PageOffset(req PageRequest) int {
	req = NormalizePage(req)
	return (req.Page - 1) * req.PageSize
}
