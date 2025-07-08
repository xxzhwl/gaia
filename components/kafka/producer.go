// Package kafka 包注释
// @author wanlizhan
// @created 2024/5/16
package kafka

import (
	"context"
	"github.com/segmentio/kafka-go"
	"time"
)

type Producer struct {
	writer *kafka.Writer
}

func NewProducer(endPoints []string, topic string) *Producer {
	k := &kafka.Writer{
		Addr:         kafka.TCP(endPoints...),
		Topic:        topic,
		RequiredAcks: kafka.RequireAll,
	}
	return &Producer{k}
}

func (p *Producer) SetAsync(async bool) *Producer {
	p.writer.Async = async
	return p
}

func (p *Producer) GetWriter() *kafka.Writer {
	return p.writer
}

func (p *Producer) WriteSimpleMsg(msg ...string) error {
	kafkaMsgs := make([]kafka.Message, 0)
	for _, s := range msg {
		kafkaMsgs = append(kafkaMsgs, kafka.Message{Value: []byte(s), Time: time.Now()})
	}
	return p.writer.WriteMessages(context.Background(), kafkaMsgs...)
}

type KvMsg struct {
	Key   string
	Value string
}

func (p *Producer) WriteKvMsg(msg []KvMsg) error {
	kafkaMsgs := make([]kafka.Message, 0)
	for _, v := range msg {
		kafkaMsgs = append(kafkaMsgs, kafka.Message{Key: []byte(v.Key), Value: []byte(v.Value), Time: time.Now()})
	}
	return p.writer.WriteMessages(context.Background(), kafkaMsgs...)
}
func (p *Producer) Close() error {
	if p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
