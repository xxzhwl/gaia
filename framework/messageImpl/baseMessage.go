// Package messageImpl 注释
// @author wanlizhan
// @created 2024/5/5
package messageImpl

import (
	"fmt"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
	"strings"
)

type BaseMessage struct {
}

func (b BaseMessage) SendSystemAlarm(title string, content string) error {
	return NewBarkMessage(title, content)
}

func (b BaseMessage) SendPanicAlarm(subject, body string) error {
	return NewBarkMessage(subject, body)
}

func NewBarkMessage(title, message string) error {
	bark := gaia.GetSafeConfString("Message.Bark")
	url := fmt.Sprintf(bark, title, message)
	url = strings.TrimSpace(url)
	_, _, err := httpclient.NewHttpRequest(url).Get()
	return err
}
