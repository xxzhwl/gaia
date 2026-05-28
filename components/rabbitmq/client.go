// Package rabbitmq 对 amqp091-go 做轻量封装，提供 RabbitMQ 消息队列能力。
//
// 典型用法：
//
//	cli, _ := rabbitmq.NewClient(rabbitmq.Config{URL: "amqp://guest:guest@localhost:5672/"})
//	pub := cli.NewPublisher("exchange", "topic")
//	pub.Publish(ctx, "routing.key", []byte("hello"))
//
//	consumer := cli.NewConsumer("queue", "exchange", "routing.key", handler)
//	consumer.Start(ctx)
//
// @author wanlizhan
// @created 2026-04-24
package rabbitmq

import (
	"context"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

// Config RabbitMQ 配置
type Config struct {
	// URL AMQP 连接字符串；必填
	URL string
	// Heartbeat 心跳间隔；0 默认 10 秒
	Heartbeat time.Duration
}

// Client RabbitMQ 客户端
type Client struct {
	conn *amqp.Connection
	cfg  Config
}

// NewClient 创建 RabbitMQ 客户端
func NewClient(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("URL 必填")
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 10 * time.Second
	}

	conn, err := amqp.DialConfig(cfg.URL, amqp.Config{Heartbeat: cfg.Heartbeat})
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}
	return &Client{conn: conn, cfg: cfg}, nil
}

// NewClientWithSchema 从 gaia 配置中读取创建客户端
func NewClientWithSchema(schema string) (*Client, error) {
	url := gaia.GetSafeConfString(schema + ".URL")
	return NewClient(Config{URL: url})
}

// NewFrameworkClient 使用 Framework.RabbitMQ 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.RabbitMQ")
}

// GetConn 返回底层 AMQP 连接
func (c *Client) GetConn() *amqp.Connection {
	return c.conn
}

// Channel 创建新 channel
func (c *Client) Channel() (*amqp.Channel, error) {
	return c.conn.Channel()
}

// Close 关闭连接
func (c *Client) Close() error {
	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn.Close()
	}
	return nil
}

// ================================ Publisher ================================

// Publisher RabbitMQ 消息发布者
type Publisher struct {
	ch           *amqp.Channel
	exchange     string
	exchangeType string
	confirms     <-chan amqp.Confirmation
	logger       *logImpl.DefaultLogger
}

// PublisherOption 发布者选项
type PublisherOption func(*publisherCfg)

type publisherCfg struct {
	confirm bool
}

// WithConfirm 启用 Publisher Confirm 模式（需服务器确认收到才返回）
func WithConfirm() PublisherOption {
	return func(c *publisherCfg) { c.confirm = true }
}

// NewPublisher 创建发布者
func (c *Client) NewPublisher(exchange, exchangeType string, opts ...PublisherOption) (*Publisher, error) {
	pc := &publisherCfg{}
	for _, opt := range opts {
		opt(pc)
	}

	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("创建 channel 失败: %w", err)
	}
	if err := ch.ExchangeDeclare(exchange, exchangeType, true, false, false, false, nil); err != nil {
		ch.Close()
		return nil, fmt.Errorf("声明 exchange 失败: %w", err)
	}

	p := &Publisher{
		ch:           ch,
		exchange:     exchange,
		exchangeType: exchangeType,
		logger:       logImpl.NewDefaultLogger().SetTitle("rabbitmq_pub_" + exchange),
	}
	if pc.confirm {
		if err := ch.Confirm(false); err != nil {
			ch.Close()
			return nil, fmt.Errorf("启用 Confirm 模式失败: %w", err)
		}
		p.confirms = ch.NotifyPublish(make(chan amqp.Confirmation, 1))
	}
	return p, nil
}

// Publish 发布消息。如果 Publisher 启用了 Confirm 模式，本函数会同步等待 broker 确认。
func (p *Publisher) Publish(ctx context.Context, routingKey string, body []byte) error {
	return p.publish(ctx, routingKey, body, nil)
}

// PublishWithHeaders 发布带 headers 的消息
func (p *Publisher) PublishWithHeaders(ctx context.Context, routingKey string, body []byte, headers amqp.Table) error {
	return p.publish(ctx, routingKey, body, headers)
}

func (p *Publisher) publish(ctx context.Context, routingKey string, body []byte, headers amqp.Table) error {
	start := time.Now()
	if err := p.ch.PublishWithContext(ctx, p.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Headers:      headers,
		Body:         body,
	}); err != nil {
		p.emitMqLog("produce", routingKey, "", len(body), start, err)
		return err
	}
	if p.confirms == nil {
		p.emitMqLog("produce", routingKey, "", len(body), start, nil)
		return nil
	}
	select {
	case confirm, ok := <-p.confirms:
		if !ok {
			err := fmt.Errorf("confirm channel 已关闭")
			p.emitMqLog("produce", routingKey, "", len(body), start, err)
			return err
		}
		if !confirm.Ack {
			err := fmt.Errorf("broker 拒绝消息(deliveryTag=%d)", confirm.DeliveryTag)
			p.emitMqLog("produce", routingKey, "", len(body), start, err)
			return err
		}
		p.emitMqLog("produce", routingKey, "", len(body), start, nil)
		return nil
	case <-ctx.Done():
		err := ctx.Err()
		p.emitMqLog("produce", routingKey, "", len(body), start, err)
		return err
	}
}

func (p *Publisher) emitMqLog(direction, key, consumerGroup string, bodySize int, start time.Time, err error) {
	if p.logger == nil {
		return
	}
	end := time.Now()
	body := logImpl.MqLogBaseModel{
		Backend:        "rabbitmq",
		Direction:      direction,
		Topic:          p.exchange,
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
	content := fmt.Sprintf("%s %s", direction, p.exchange)
	if err != nil {
		level = gaia.LogErrorLevel
		body.Err = err.Error()
		content += " err=" + err.Error()
	}
	p.logger.MqLog(level, content)
	p.logger.MqLogBody(level, content, body)
}

// Close 关闭发布者 channel
func (p *Publisher) Close() error {
	return p.ch.Close()
}

// ================================ Consumer ================================

// MessageHandler 消息处理函数
type MessageHandler func(delivery amqp.Delivery) error

// Consumer RabbitMQ 消息消费者
type Consumer struct {
	ch         *amqp.Channel
	queue      string
	exchange   string
	routingKey string
	handler    MessageHandler
	prefetch   int
	logger     *logImpl.DefaultLogger
}

// ConsumerOption 消费者选项
type ConsumerOption func(*Consumer)

// WithPrefetch 设置预取数量
func WithPrefetch(n int) ConsumerOption {
	return func(c *Consumer) { c.prefetch = n }
}

// NewConsumer 创建消费者
func (c *Client) NewConsumer(queue, exchange, routingKey string, handler MessageHandler, opts ...ConsumerOption) (*Consumer, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("创建 channel 失败: %w", err)
	}

	consumer := &Consumer{
		ch:         ch,
		queue:      queue,
		exchange:   exchange,
		routingKey: routingKey,
		handler:    handler,
		prefetch:   10,
		logger:     logImpl.NewDefaultLogger().SetTitle("rabbitmq_consumer_" + queue),
	}
	for _, opt := range opts {
		opt(consumer)
	}

	if err := ch.Qos(consumer.prefetch, 0, false); err != nil {
		ch.Close()
		return nil, fmt.Errorf("设置 QoS 失败: %w", err)
	}

	if _, err := ch.QueueDeclare(queue, true, false, false, false, nil); err != nil {
		ch.Close()
		return nil, fmt.Errorf("声明队列失败: %w", err)
	}

	if exchange != "" {
		if err := ch.QueueBind(queue, routingKey, exchange, false, nil); err != nil {
			ch.Close()
			return nil, fmt.Errorf("绑定队列失败: %w", err)
		}
	}

	return consumer, nil
}

// Start 开始消费（阻塞）
func (c *Consumer) Start(ctx context.Context) error {
	deliveries, err := c.ch.Consume(c.queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("开始消费失败: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("delivery channel 已关闭")
			}
			start := time.Now()
			err := c.handler(d)
			c.emitMqLog(d, start, err)
			if err != nil {
				d.Nack(false, true)
			} else {
				d.Ack(false)
			}
		}
	}
}

func (c *Consumer) emitMqLog(d amqp.Delivery, start time.Time, err error) {
	if c.logger == nil {
		return
	}
	end := time.Now()
	body := logImpl.MqLogBaseModel{
		Backend:        "rabbitmq",
		Direction:      "consume",
		Topic:          c.exchange,
		Key:            d.RoutingKey,
		ConsumerGroup:  c.queue,
		BodySize:       len(d.Body),
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		EndTime:        end.Format(gaia.DateTimeMillsFormat),
		Duration:       float64(end.Sub(start).Milliseconds()),
		StartTimeStamp: start.UnixMilli(),
		EndTimeStamp:   end.UnixMilli(),
	}
	level := gaia.LogInfoLevel
	content := fmt.Sprintf("consume %s", c.queue)
	if err != nil {
		level = gaia.LogErrorLevel
		body.Err = err.Error()
		content += " err=" + err.Error()
	}
	c.logger.MqLog(level, content)
	c.logger.MqLogBody(level, content, body)
}

// Close 关闭消费者 channel
func (c *Consumer) Close() error {
	return c.ch.Close()
}
