// Package ai 公共类型定义
// @author wanlizhan
// @created 2026-05-28
package ai

// Role 是 LLM 消息角色
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 单条对话消息
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// ToolCallID 仅当 Role=tool 时使用，关联到 assistant 的 tool_call_id
	ToolCallID string `json:"tool_call_id,omitempty"`
	// Name 可选发送者名（部分模型支持）
	Name string `json:"name,omitempty"`

	// Images 多模态：当 Role=user 时，可附带图片输入。
	// 同时设置 Content 与 Images 时会拼成 [text, image_url, image_url...]。
	Images []ImageInput `json:"images,omitempty"`

	// ToolCalls 仅当 Role=assistant 且模型本轮决定调用 tools 时，由模型回填。
	// 调用方在传入历史消息时也可携带（用于把上一轮 assistant 的 tool_calls 重新喂给模型）。
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ImageInput 多模态图片输入
type ImageInput struct {
	// URL 图片地址（http/https），或 data URL（data:image/png;base64,xxx）
	URL string `json:"url"`
	// Detail 细节程度：""/auto/low/high
	Detail string `json:"detail,omitempty"`
}

// ToolCall 模型生成的一次工具调用
type ToolCall struct {
	// ID OpenAI 分配的调用 ID（继续对话时必须原样回传）
	ID string `json:"id"`
	// Name 函数名
	Name string `json:"name"`
	// Arguments 模型生成的参数 JSON 串（注意：模型不一定生成合法 JSON，调用方需校验）
	Arguments string `json:"arguments"`
}

// Usage Token 用量信息
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// ChatRequest 通用 Chat 请求参数
//
// 只暴露最常用的字段，避免与底层 SDK 强耦合。
// 如有特殊参数需求，可直接使用 OpenAIClient.RawClient() 拿原生 SDK。
type ChatRequest struct {
	// Model 模型名，为空则使用 OpenAIClient.DefaultModel
	Model string
	// Messages 消息列表，至少要有一条 user 消息
	Messages []Message

	// Temperature 采样温度，nil 不传
	Temperature *float64
	// TopP nucleus 采样参数，nil 不传
	TopP *float64
	// MaxTokens 最大输出 token，0 不传
	MaxTokens int64
	// Stop 停止词
	Stop []string
	// Seed 随机种子，nil 不传
	Seed *int64
	// User 终端用户标识（用于服务端审计/缓存）
	User string

	// JSONMode 是否要求模型返回 JSON 对象（response_format = json_object）
	JSONMode bool

	// Tools 可选的函数声明列表（OpenAI tool calling）
	Tools []ToolDef
	// ToolChoice 可选取值：""/"auto"/"none"/"required"/"<func name>"
	ToolChoice string

	// UseCache 是否对本次请求启用结果缓存（需 client.CacheEnable=true 且建议 Temperature=0）
	UseCache bool
}

// ChatResult 普通（非流式）Chat 调用结果
type ChatResult struct {
	// Content 模型返回的文本（取 Choices[0].Message.Content）
	Content string
	// FinishReason 结束原因：stop / length / tool_calls 等
	FinishReason string
	// Model 真实使用的模型名
	Model string
	// Usage Token 用量
	Usage Usage
	// ToolCalls 当 FinishReason=tool_calls 时，包含模型希望调用的工具列表
	ToolCalls []ToolCall
	// Raw 原始响应 JSON，便于排查
	Raw string
}

// StreamEvent 流式事件
//
// 三种状态（互斥，且按时间顺序）：
//  1. 普通增量：Delta 非空，Done=false，Err=nil
//  2. 正常结束：Done=true，Err=nil，FinishReason 有值，Usage 可能有值
//  3. 异常结束：Done=true，Err 非 nil
//
// 调用方建议直接 for ev := range ch，根据 ev.Err / ev.Done 处理。
type StreamEvent struct {
	Delta        string
	FinishReason string
	Done         bool
	Err          error
	// Usage 仅当 Done=true 且服务端返回 usage 时填充
	Usage Usage
}
