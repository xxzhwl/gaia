// Package kafka 包注释
// @author wanlizhan
// @created 2024/5/16
package kafka

import (
	"context"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"time"
)

type Consumer struct {
	reader *kafka.Reader
}

func NewConsumer(endPoints []string, groupId, topic string) *Consumer {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  endPoints,
		GroupID:  groupId,
		Topic:    topic,
		MinBytes: 64,
		MaxBytes: 5e6,
		MaxWait:  10 * time.Second,
	})
	return &Consumer{r}
}

func (c *Consumer) WithAuth(userName, password string) *Consumer {
	dialer := c.reader.Config().Dialer
	dialer.SASLMechanism = plain.Mechanism{
		Username: userName,
		Password: password,
	}
	return c
}

func (c *Consumer) GetReader() *kafka.Reader {
	return c.reader
}

func (c *Consumer) Close() error {
	if c.reader == nil {
		return nil
	}
	return c.reader.Close()
}

func (c *Consumer) ReadWithCtx(ctx context.Context) (kafka.Message, error) {
	return c.reader.ReadMessage(ctx)
}

func (c *Consumer) Read() (kafka.Message, error) {
	return c.ReadWithCtx(context.Background())
}
