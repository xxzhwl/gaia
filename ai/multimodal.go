// Package ai 多模态相关辅助
// @author wanlizhan
// @created 2026-05-28
package ai

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// UserMessageWithImages 构造一个带图片的 user 消息
//
// text 是说明文本（可为空），images 是图片输入列表（URL 或 data URL）。
// 模型需要支持视觉能力（如 gpt-4o / GLM-4V / qwen-vl 等）。
func UserMessageWithImages(text string, images ...ImageInput) Message {
	return Message{
		Role:    RoleUser,
		Content: text,
		Images:  images,
	}
}

// ImageFromURL 构造一条 URL 形式的图片输入
func ImageFromURL(url string, detail ...string) ImageInput {
	d := ""
	if len(detail) > 0 {
		d = detail[0]
	}
	return ImageInput{URL: url, Detail: d}
}

// ImageFromBase64 构造一条 base64/data URL 形式的图片输入
//
// mimeType 例如 "image/png" / "image/jpeg"。
// 如果传入的 data 已经是 "data:..." 开头会原样使用。
func ImageFromBase64(mimeType string, data []byte, detail ...string) ImageInput {
	var url string
	asStr := string(data)
	if strings.HasPrefix(asStr, "data:") {
		url = asStr
	} else {
		// 探测是否已经是 base64 字符串
		if isBase64String(data) {
			url = fmt.Sprintf("data:%s;base64,%s", mimeType, asStr)
		} else {
			url = fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
		}
	}
	d := ""
	if len(detail) > 0 {
		d = detail[0]
	}
	return ImageInput{URL: url, Detail: d}
}

// isBase64String 简单检测 data 是否就是 base64 字符串本身
func isBase64String(data []byte) bool {
	if len(data) == 0 || len(data)%4 != 0 {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(string(data))
	return err == nil
}
