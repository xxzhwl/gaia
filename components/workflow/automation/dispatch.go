package automation

// DispatchRequest 表示 workflow 调用自动化 worker 的请求。
type DispatchRequest struct {
	TaskID            string         `json:"taskId"`
	ProcessInstanceID string         `json:"processInstanceId"`
	DefinitionID      string         `json:"definitionId"`
	DefinitionKey     string         `json:"definitionKey"`
	DefinitionName    string         `json:"definitionName"`
	DefinitionVersion int            `json:"definitionVersion"`
	InstanceName      string         `json:"instanceName"`
	NodeID            string         `json:"nodeId"`
	NodeName          string         `json:"nodeName"`
	AutomationTaskKey string         `json:"automationTaskKey"`
	Variables         map[string]any `json:"variables"`
	CallbackToken     string         `json:"callbackToken"`
	CallbackURL       string         `json:"callbackUrl"`
	FailCallbackURL   string         `json:"failCallbackUrl"`
	DispatchMode      string         `json:"dispatchMode"`
}

// DispatchResult 表示自动化 worker 执行任务后的结果。
type DispatchResult struct {
	TaskID            string         `json:"taskId"`
	ProcessInstanceID string         `json:"processInstanceId"`
	NodeID            string         `json:"nodeId"`
	Completed         bool           `json:"completed"`
	Variables         map[string]any `json:"variables"`
}
