// Package kafka 包注释
// @author wanlizhan
// @created 2024/5/16
package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

type Consumer struct {
	reader *kafka.Reader
	topic  string
	group  string
	logger *logImpl.DefaultLogger
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
	return &Consumer{reader: r, topic: topic, group: groupId, logger: logImpl.NewDefaultLogger().SetTitle("kafka_consumer_" + topic)}
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
	return &Consumer{reader: r, topic: topic, group: groupId, logger: logImpl.NewDefaultLogger().SetTitle("kafka_consumer_" + topic)}
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
	start := time.Now()
	msg, err := c.reader.ReadMessage(ctx)
	bodySize := 0
	key := ""
	if err == nil {
		bodySize = len(msg.Value)
		key = string(msg.Key)
	}
	c.emitMqLog(msg, key, bodySize, start, err)
	return msg, err
}

func (c *Consumer) Read() (kafka.Message, error) {
	return c.ReadWithCtx(context.Background())
}

func (c *Consumer) emitMqLog(msg kafka.Message, key string, bodySize int, start time.Time, err error) {
	if c.logger == nil {
		return
	}
	end := time.Now()
	body := logImpl.MqLogBaseModel{
		Backend:        "kafka",
		Direction:      "consume",
		Topic:          c.topic,
		Partition:      int32(msg.Partition),
		Offset:         msg.Offset,
		Key:            key,
		ConsumerGroup:  c.group,
		BodySize:       bodySize,
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		EndTime:        end.Format(gaia.DateTimeMillsFormat),
		Duration:       float64(end.Sub(start).Milliseconds()),
		StartTimeStamp: start.UnixMilli(),
		EndTimeStamp:   end.UnixMilli(),
	}
	level := gaia.LogInfoLevel
	content := fmt.Sprintf("consume %s", c.topic)
	if err != nil {
		level = gaia.LogErrorLevel
		body.Err = err.Error()
		content += " err=" + err.Error()
	}
	c.logger.MqLog(level, content)
	c.logger.MqLogBody(level, content, body)
}
