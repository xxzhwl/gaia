package gaia

import (
	"bytes"
	"io"

	"gopkg.in/gomail.v2"
)

// MailConf 邮件服务器配置
type MailConf struct {
	UserName string
	Password string
	Host     string
	Port     int
}

// NewMailConf 创建邮件配置实例
func NewMailConf(userName string, password string, host string, port int) MailConf {
	return MailConf{
		UserName: userName,
		Password: password,
		Host:     host,
		Port:     port,
	}
}

// NewDefaultMailConf 创建默认邮件配置（从框架配置读取）
func NewDefaultMailConf() (MailConf, error) {
	return NewMailConfBySchema("Framework.Mail")
}

// NewMailConfBySchema 根据配置schema创建邮件配置
func NewMailConfBySchema(schema string) (MailConf, error) {
	conf := MailConf{}
	if err := LoadConfToObjWithErr(schema, &conf); err != nil {
		return MailConf{}, err
	}
	return conf, nil
}

// Attachment 邮件附件结构体
type Attachment struct {
	FileName string
	Content  []byte
	Reader   io.Reader
	FilePath string
}

// MailMessage 邮件消息结构体
type MailMessage struct {
	To          []string
	Cc          []string
	Subject     string
	Body        string
	Attachments []Attachment
}

// SendMail 发送邮件消息
func (m MailConf) SendMail(message MailMessage) error {
	newMessage := gomail.NewMessage()
	newMessage.SetHeader("From", m.UserName)
	newMessage.SetHeader("To", message.To...)
	newMessage.SetHeader("Cc", message.Cc...)
	newMessage.SetHeader("Subject", message.Subject)
	newMessage.SetBody("text/html", message.Body)

	// 添加附件
	for _, attachment := range message.Attachments {
		if attachment.FilePath != "" {
			// 从文件路径添加附件
			newMessage.Attach(attachment.FilePath)
		} else if attachment.Reader != nil || len(attachment.Content) > 0 {
			// 从io.Reader或字节数组添加附件
			var reader io.Reader
			if attachment.Reader != nil {
				reader = attachment.Reader
			} else {
				reader = bytes.NewReader(attachment.Content)
			}
			// 使用Attach方法的文件设置功能，从reader添加附件
			newMessage.Attach(attachment.FileName, gomail.SetCopyFunc(func(w io.Writer) error {
				_, err := io.Copy(w, reader)
				return err
			}))
		}
	}

	return gomail.NewDialer(m.Host, m.Port, m.UserName, m.Password).DialAndSend(newMessage)
}
