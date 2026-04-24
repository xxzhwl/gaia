// Package mqtt 对 Eclipse Paho MQTT 做轻量封装，提供 IoT/即时消息能力。
//
// 典型用法：
//
//	cli, _ := mqtt.NewClient(mqtt.Config{
//	    Broker: "tcp://localhost:1883", ClientID: "gaia-01",
//	})
//	cli.Subscribe("topic/test", 1, func(topic string, payload []byte) { ... })
//	cli.Publish("topic/test", 1, false, []byte("hello"))
//
// @author wanlizhan
// @created 2026-04-24
package mqtt

import (
	"fmt"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/xxzhwl/gaia"
)

// Config MQTT 客户端配置
type Config struct {
	// Broker MQTT 服务器地址；必填（如 "tcp://localhost:1883"）
	Broker string
	// ClientID 客户端标识；必填
	ClientID string
	// Username/Password 认证信息
	Username string
	Password string
	// CleanSession 是否清除会话；默认 true
	CleanSession *bool
	// KeepAlive 心跳间隔；0 默认 60 秒
	KeepAlive time.Duration
	// ConnectTimeout 连接超时；0 默认 10 秒
	ConnectTimeout time.Duration
	// AutoReconnect 断线自动重连；默认 true
	AutoReconnect *bool
}

// MessageHandler 消息处理回调
type MessageHandler func(topic string, payload []byte)

// Client MQTT 客户端
type Client struct {
	cli pahomqtt.Client
	cfg Config
}

// NewClient 创建 MQTT 客户端
func NewClient(cfg Config) (*Client, error) {
	if cfg.Broker == "" {
		return nil, fmt.Errorf("Broker 必填")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("ClientID 必填")
	}
	if cfg.KeepAlive <= 0 {
		cfg.KeepAlive = 60 * time.Second
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}

	opts := pahomqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetKeepAlive(cfg.KeepAlive).
		SetConnectTimeout(cfg.ConnectTimeout)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}
	if cfg.CleanSession != nil {
		opts.SetCleanSession(*cfg.CleanSession)
	}
	if cfg.AutoReconnect != nil {
		opts.SetAutoReconnect(*cfg.AutoReconnect)
	} else {
		opts.SetAutoReconnect(true)
	}

	client := pahomqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("连接 MQTT Broker 失败: %w", err)
	}

	return &Client{cli: client, cfg: cfg}, nil
}

// NewClientWithSchema 从 gaia 配置中读取创建客户端
func NewClientWithSchema(schema string) (*Client, error) {
	return NewClient(Config{
		Broker:   gaia.GetSafeConfString(schema + ".Broker"),
		ClientID: gaia.GetSafeConfString(schema + ".ClientID"),
		Username: gaia.GetSafeConfString(schema + ".Username"),
		Password: gaia.GetSafeConfString(schema + ".Password"),
	})
}

// NewFrameworkClient 使用 Framework.MQTT 配置
func NewFrameworkClient() (*Client, error) {
	return NewClientWithSchema("Framework.MQTT")
}

// GetCli 返回底层 Paho 客户端
func (c *Client) GetCli() pahomqtt.Client {
	return c.cli
}

// Publish 发布消息
//   - qos: 0=至多一次, 1=至少一次, 2=恰好一次
//   - retained: 是否保留消息
func (c *Client) Publish(topic string, qos byte, retained bool, payload []byte) error {
	token := c.cli.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
}

// Subscribe 订阅主题
func (c *Client) Subscribe(topic string, qos byte, handler MessageHandler) error {
	callback := func(client pahomqtt.Client, msg pahomqtt.Message) {
		handler(msg.Topic(), msg.Payload())
	}
	token := c.cli.Subscribe(topic, qos, callback)
	token.Wait()
	return token.Error()
}

// SubscribeMultiple 批量订阅
func (c *Client) SubscribeMultiple(topics map[string]byte, handler MessageHandler) error {
	callback := func(client pahomqtt.Client, msg pahomqtt.Message) {
		handler(msg.Topic(), msg.Payload())
	}
	token := c.cli.SubscribeMultiple(topics, callback)
	token.Wait()
	return token.Error()
}

// Unsubscribe 取消订阅
func (c *Client) Unsubscribe(topics ...string) error {
	token := c.cli.Unsubscribe(topics...)
	token.Wait()
	return token.Error()
}

// IsConnected 检查是否连接
func (c *Client) IsConnected() bool {
	return c.cli.IsConnected()
}

// Close 断开连接（等待 250ms 让未发送的消息发出）
func (c *Client) Close() {
	c.cli.Disconnect(250)
}
