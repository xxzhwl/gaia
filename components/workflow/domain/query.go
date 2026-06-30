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
}

// TaskListFilter 表示任务列表查询条件。
type TaskListFilter struct {
	PageRequest
	InstanceID string
	Status     TaskStatus
	Type       TaskType
	Assignee   string
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
