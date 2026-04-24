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
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xxzhwl/gaia"
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
	mu   sync.Mutex
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
}

// NewPublisher 创建发布者
func (c *Client) NewPublisher(exchange, exchangeType string) (*Publisher, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("创建 channel 失败: %w", err)
	}
	if err := ch.ExchangeDeclare(exchange, exchangeType, true, false, false, false, nil); err != nil {
		ch.Close()
		return nil, fmt.Errorf("声明 exchange 失败: %w", err)
	}
	return &Publisher{ch: ch, exchange: exchange, exchangeType: exchangeType}, nil
}

// Publish 发布消息
func (p *Publisher) Publish(ctx context.Context, routingKey string, body []byte) error {
	return p.ch.PublishWithContext(ctx, p.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	})
}

// PublishWithHeaders 发布带 headers 的消息
func (p *Publisher) PublishWithHeaders(ctx context.Context, routingKey string, body []byte, headers amqp.Table) error {
	return p.ch.PublishWithContext(ctx, p.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Headers:      headers,
		Body:         body,
	})
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
			if err := c.handler(d); err != nil {
				d.Nack(false, true)
			} else {
				d.Ack(false)
			}
		}
	}
}

// Close 关闭消费者 channel
func (c *Consumer) Close() error {
	return c.ch.Close()
}
