// Package messageImpl 注释
// @author wanlizhan
// @created 2024/5/5
package messageImpl

import (
	"encoding/json"
	"errors"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
)

const (
	MsgTypeText     = "text"
	MsgTypeRichText = "post"
	MsgTypeImg      = "image"
	MsgTypeCard     = "interactive"
	MsgTypeShare    = "share_chat"
)

type FeiShuRobot struct {
	Hook string
}

func (r FeiShuRobot) SendSystemAlarm(title string, content string) error {
	return r.SendRichText(title, []Content{{Text: content, Tag: "text"}})
}

func (r FeiShuRobot) SendPanicAlarm(subject, body string) error {
	return r.SendRichText(subject, []Content{{Text: body, Tag: "text"}})
}

type RobotResp struct {
	Code int64  `json:"code"`
	Msg  string `json:"msg"`
}

func NewFeiShuRobot() FeiShuRobot {
	hook := gaia.GetSafeConfString("Message.FeiShuRobot")
	return NewFeiShuRobotWithHook(hook)
}

func NewFeiShuRobotWithHook(hook string) FeiShuRobot {
	return FeiShuRobot{hook}
}

func (r FeiShuRobot) SendText(content string) error {
	return r.request(MsgTypeText, map[string]any{MsgTypeText: content})
}

type Content struct {
	Tag    string `json:"tag"`
	Text   string `json:"text,omitempty"`
	Href   string `json:"href,omitempty"`
	UserId string `json:"user_id,omitempty"`
}

func (r FeiShuRobot) SendRichText(title string, content []Content) error {
	return r.request(MsgTypeRichText, map[string]any{
		MsgTypeRichText: map[string]any{
			"zh_cn": map[string]any{
				"title":   title,
				"content": [][]Content{content},
			}}})
}

func (r FeiShuRobot) request(msgType string, content map[string]any) error {
	if len(r.Hook) == 0 {
		return errors.New("Robot-Hook is empty")
	}

	msg, err := json.Marshal(map[string]any{"msg_type": msgType, "content": content})
	if err != nil {
		return err
	}
	_, _, err = httpclient.NewHttpRequest(r.Hook).WithTitle("FeiShuRobot").Post(msg)
	if err != nil {
		return err
	}
	return nil
}
