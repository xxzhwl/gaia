// Package messageImpl 注释
// @author wanlizhan
// @created 2024/5/5
package messageImpl

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
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
	barkUrl := fmt.Sprintf(bark, url.QueryEscape(title), url.QueryEscape(message))
	barkUrl = strings.TrimSpace(barkUrl)
	_, _, err := httpclient.NewHttpRequest(barkUrl).Get()
	return err
}
