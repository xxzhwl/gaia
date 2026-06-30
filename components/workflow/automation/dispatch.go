package automation

// DispatchRequest 表示 workflow 调用自动化 worker 的请求。
type DispatchRequest struct {
	TaskID            string         `json:"taskId"`
	ProcessInstanceID string         `json:"processInstanceId"`
	NodeID            string         `json:"nodeId"`
	AutomationTaskKey string         `json:"automationTaskKey"`
	Variables         map[string]any `json:"variables"`
	CallbackToken     string         `json:"callbackToken"`
	CallbackURL       string         `json:"callbackUrl"`
}

// DispatchResult 表示自动化 worker 执行任务后的结果。
type DispatchResult struct {
	TaskID            string         `json:"taskId"`
	ProcessInstanceID string         `json:"processInstanceId"`
	NodeID            string         `json:"nodeId"`
	Completed         bool           `json:"completed"`
	Variables         map[string]any `json:"variables"`
}
