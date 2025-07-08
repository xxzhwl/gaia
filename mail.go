package gaia

import (
	"google.golang.org/appengine/mail"
	"gopkg.in/gomail.v2"
)

type MailConf struct {
	UserName string
	Password string
	Host     string
	Port     int
}

func NewMailConf(userName string, password string, host string, port int) MailConf {
	return MailConf{
		UserName: userName,
		Password: password,
		Host:     host,
		Port:     port,
	}
}

func NewDefaultMailConf() (MailConf, error) {
	return NewMailConfBySchema("Framework.Mail")
}

func NewMailConfBySchema(schema string) (MailConf, error) {
	conf := MailConf{}
	if err := LoadConfToObjWithErr(schema, &conf); err != nil {
		return MailConf{}, err
	}
	return conf, nil
}

type MailMessage struct {
	To         []string
	Cc         []string
	Subject    string
	Body       string
	Attachment []mail.Attachment
}

func (m MailConf) SendMail(message MailMessage) error {
	newMessage := gomail.NewMessage()
	newMessage.SetHeader("From", m.UserName)
	newMessage.SetHeader("To", message.To...)
	newMessage.SetHeader("Cc", message.Cc...)
	newMessage.SetHeader("Subject", message.Subject)
	newMessage.SetBody("text/html", message.Body)

	return gomail.NewDialer(m.Host, m.Port, m.UserName, m.Password).DialAndSend(newMessage)
}
