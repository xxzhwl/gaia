// Package databus 包注释
// @author wanlizhan
// @created 2024/7/16
package databus

type Subscriber struct {
	theme string

	dataChan chan []byte
	chanSize int
}

func NewSubscriber(theme string, size int) *Subscriber {
	return &Subscriber{
		theme:    theme,
		dataChan: make(chan []byte, size),
		chanSize: size,
	}
}

func (s *Subscriber) DataChan() chan []byte {
	return s.dataChan
}
