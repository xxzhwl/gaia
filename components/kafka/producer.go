// Package kafka 包注释
// @author wanlizhan
// @created 2024/5/16
package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

type Producer struct {
	writer *kafka.Writer
	topic  string
	logger *logImpl.DefaultLogger
}

func NewProducer(endPoints []string, topic string) *Producer {
	k := &kafka.Writer{
		Addr:         kafka.TCP(endPoints...),
		Topic:        topic,
		RequiredAcks: kafka.RequireAll,
	}
	return &Producer{writer: k, topic: topic, logger: logImpl.NewDefaultLogger().SetTitle("kafka_producer_" + topic)}
}

func (p *Producer) SetAsync(async bool) *Producer {
	p.writer.Async = async
	return p
}

// SetCompletion 设置异步发送完成回调。
// 当 SetAsync(true) 时 WriteMessages 始终返回 nil，必须通过此回调感知发送结果。
//   - messages: 本次发送的消息（成功/失败均会回调）
//   - err: 非 nil 表示该批消息发送失败
func (p *Producer) SetCompletion(fn func(messages []kafka.Message, err error)) *Producer {
	p.writer.Completion = fn
	return p
}

func (p *Producer) GetWriter() *kafka.Writer {
	return p.writer
}

// WriteSimpleMsg 使用默认 context.Background() 写入；阻塞行为可能导致挂死，
// 生产环境建议改用 WriteSimpleMsgWithCtx 显式控制超时。
func (p *Producer) WriteSimpleMsg(msg ...string) error {
	return p.WriteSimpleMsgWithCtx(context.Background(), msg...)
}

// WriteSimpleMsgWithCtx 带 context 的字符串消息写入
func (p *Producer) WriteSimpleMsgWithCtx(ctx context.Context, msg ...string) error {
	start := time.Now()
	kafkaMsgs := make([]kafka.Message, 0, len(msg))
	bodySize := 0
	for _, s := range msg {
		bodySize += len(s)
		kafkaMsgs = append(kafkaMsgs, kafka.Message{Value: []byte(s), Time: time.Now()})
	}
	err := p.writer.WriteMessages(ctx, kafkaMsgs...)
	p.emitMqLog("produce", "", "", bodySize, start, err)
	return err
}

type KvMsg struct {
	Key   string
	Value string
}

// WriteKvMsg 使用默认 context.Background() 写入；建议改用 WriteKvMsgWithCtx
func (p *Producer) WriteKvMsg(msg []KvMsg) error {
	return p.WriteKvMsgWithCtx(context.Background(), msg)
}

// WriteKvMsgWithCtx 带 context 的 KV 消息写入
func (p *Producer) WriteKvMsgWithCtx(ctx context.Context, msg []KvMsg) error {
	start := time.Now()
	kafkaMsgs := make([]kafka.Message, 0, len(msg))
	bodySize := 0
	key := ""
	for _, v := range msg {
		bodySize += len(v.Value)
		if key == "" {
			key = v.Key
		}
		kafkaMsgs = append(kafkaMsgs, kafka.Message{Key: []byte(v.Key), Value: []byte(v.Value), Time: time.Now()})
	}
	err := p.writer.WriteMessages(ctx, kafkaMsgs...)
	p.emitMqLog("produce", key, "", bodySize, start, err)
	return err
}

func (p *Producer) emitMqLog(direction, key, consumerGroup string, bodySize int, start time.Time, err error) {
	if p.logger == nil {
		return
	}
	end := time.Now()
	body := logImpl.MqLogBaseModel{
		Backend:        "kafka",
		Direction:      direction,
		Topic:          p.topic,
		Key:            key,
		ConsumerGroup:  consumerGroup,
		BodySize:       bodySize,
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		EndTime:        end.Format(gaia.DateTimeMillsFormat),
		Duration:       float64(end.Sub(start).Milliseconds()),
		StartTimeStamp: start.UnixMilli(),
		EndTimeStamp:   end.UnixMilli(),
	}
	level := gaia.LogInfoLevel
	content := fmt.Sprintf("%s %s", direction, p.topic)
	if err != nil {
		level = gaia.LogErrorLevel
		body.Err = err.Error()
		content += " err=" + err.Error()
	}
	p.logger.MqLog(level, content)
	p.logger.MqLogBody(level, content, body)
}

func (p *Producer) Close() error {
	if p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
