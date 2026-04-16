// Package kafka 包注释
// @author wanlizhan
// @created 2024/5/16
package kafka

import (
	"context"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/xxzhwl/gaia"
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

// WithAuth is deprecated and does not work as expected. Use NewConsumerWithAuth instead.
func (c *Consumer) WithAuth(userName, password string) *Consumer {
	gaia.Warn("WithAuth is deprecated and does not work as expected. Use NewConsumerWithAuth instead.")
	return c
}

// NewConsumerWithAuth creates a consumer with SASL authentication
func NewConsumerWithAuth(endPoints []string, groupId, topic, userName, password string) *Consumer {
	mechanism := plain.Mechanism{
		Username: userName,
		Password: password,
	}
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  endPoints,
		GroupID:  groupId,
		Topic:    topic,
		MinBytes: 64,
		MaxBytes: 5e6,
		MaxWait:  10 * time.Second,
		Dialer: &kafka.Dialer{
			Timeout:       10 * time.Second,
			DualStack:     true,
			SASLMechanism: mechanism,
		},
	})
	return &Consumer{r}
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
