package gaia

import (
	"fmt"
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
