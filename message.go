package gaia

import (
	"fmt"
	"sort"
	"strings"
)

/*
消息发送服务基础实现
@author wanlizhan
@created 2023-03-03
*/

// IMessage 定义消息发送标准接口
type IMessage interface {
	// SendSystemAlarm 发送系统告警
	SendSystemAlarm(title string, content string) error

	// SendPanicAlarm 发送panic异常告警
	SendPanicAlarm(subject, body string) error

	// SendNotify 发送通用通知（如服务启动/停止）
	SendNotify(title string, content string) error
}

// Message 提供外部逻辑实现注入，具体实现逻辑跟具体系统场景相关，统一在com/messageimpl包中实现
var Message IMessage

// SendSystemAlarm 发送非邮件告警
func SendSystemAlarm(title, content string) error {
	if Message == nil {
		Log(LogWarnLevel, "IMessage interface not implemented")
		return nil
	}
	titleTpl := fmt.Sprintf("【%s】%s\n", GetSystemEnName(), title)
	contentTpl := fmt.Sprintf("%s\n日志Id:[%s]\nTraceId:[%s]\n时间:[%s]",
		content,
		NewContextTrace().GetId(),
		NewContextTrace().GetTraceId(),
		Date(DateTimeMillsFormat))
	return Message.SendSystemAlarm(titleTpl, contentTpl)
}

// SendPanicAlarm 发送panic异常告警
func SendPanicAlarm(title, body string) error {
	if Message == nil {
		Log(LogWarnLevel, "IMessage interface not implemented")
		return nil
	}
	return Message.SendPanicAlarm(title, body)
}

// SendNotify 发送通用通知（服务启动、停止等）
func SendNotify(title, content string) error {
	if Message == nil {
		Log(LogWarnLevel, "IMessage interface not implemented")
		return nil
	}
	titleTpl := fmt.Sprintf("【%s】%s\n", GetSystemEnName(), title)
	contentTpl := fmt.Sprintf("%s\n时间:[%s]",
		content,
		Date(DateTimeMillsFormat))
	return Message.SendNotify(titleTpl, contentTpl)
}

// LifecycleNotifyInfo 描述服务或组件生命周期事件。
type LifecycleNotifyInfo struct {
	Kind      string         `json:"kind"`      // service/component
	Name      string         `json:"name"`      // HTTP/gRPC/Jobs/AsyncTask
	Action    string         `json:"action"`    // 启动/停止/恢复
	Status    string         `json:"status"`    // running/stopped/failed
	Protocol  string         `json:"protocol"`  // HTTP/HTTPS/gRPC
	Address   string         `json:"address"`   // 监听地址
	Schema    string         `json:"schema"`    // 配置 schema
	Component string         `json:"component"` // 组件名
	Instance  string         `json:"instance"`  // 实例 ID
	Fields    map[string]any `json:"fields"`    // 扩展字段
}

// SendLifecycleNotify 发送统一的服务/组件生命周期通知。
func SendLifecycleNotify(info LifecycleNotifyInfo) error {
	if info.Action == "" {
		info.Action = "状态变更"
	}
	name := info.Name
	if name == "" {
		name = info.Component
	}
	if name == "" {
		name = info.Protocol
	}
	if name == "" {
		name = "Gaia"
	}

	title := fmt.Sprintf("%s%s通知", name, info.Action)
	lines := make([]string, 0, 12+len(info.Fields))
	appendLine := func(label, value string) {
		if value != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", label, value))
		}
	}
	appendLine("类型", info.Kind)
	appendLine("名称", name)
	appendLine("组件", info.Component)
	appendLine("协议", info.Protocol)
	appendLine("地址", info.Address)
	appendLine("Schema", info.Schema)
	appendLine("动作", info.Action)
	appendLine("状态", info.Status)
	appendLine("实例", info.Instance)
	appendLine("环境", GetEnvFlag())

	if len(info.Fields) > 0 {
		keys := make([]string, 0, len(info.Fields))
		for key := range info.Fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("%s: %v", key, info.Fields[key]))
		}
	}

	return SendNotify(title, strings.Join(lines, "\n"))
}
