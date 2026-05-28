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
	"github.com/xxzhwl/gaia/framework/logImpl"
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
	// OperationTimeout Publish/Subscribe 等同步操作的超时；0 默认 30 秒
	OperationTimeout time.Duration
	// DisconnectQuiesce Close() 时会等待多久（毫秒）以便未发出的消息走完；0 默认 250
	DisconnectQuiesce uint
}

// MessageHandler 消息处理回调
type MessageHandler func(topic string, payload []byte)

// Client MQTT 客户端
type Client struct {
	cli    pahomqtt.Client
	cfg    Config
	logger *logImpl.DefaultLogger
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
	if cfg.OperationTimeout <= 0 {
		cfg.OperationTimeout = 30 * time.Second
	}
	if cfg.DisconnectQuiesce == 0 {
		cfg.DisconnectQuiesce = 250
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

	return &Client{cli: client, cfg: cfg, logger: logImpl.NewDefaultLogger().SetTitle("mqtt_" + cfg.ClientID)}, nil
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

// waitToken 等待 token 完成，超过 OperationTimeout 后返回超时错误，避免无限阻塞。
func (c *Client) waitToken(token pahomqtt.Token) error {
	if !token.WaitTimeout(c.cfg.OperationTimeout) {
		return fmt.Errorf("MQTT 操作超时(%s)", c.cfg.OperationTimeout)
	}
	return token.Error()
}

// Publish 发布消息
//   - qos: 0=至多一次, 1=至少一次, 2=恰好一次
//   - retained: 是否保留消息
func (c *Client) Publish(topic string, qos byte, retained bool, payload []byte) error {
	start := time.Now()
	err := c.waitToken(c.cli.Publish(topic, qos, retained, payload))
	c.emitMqLog("produce", topic, "", len(payload), start, err)
	return err
}

// Subscribe 订阅主题
func (c *Client) Subscribe(topic string, qos byte, handler MessageHandler) error {
	callback := func(client pahomqtt.Client, msg pahomqtt.Message) {
		start := time.Now()
		handler(msg.Topic(), msg.Payload())
		c.emitMqLog("consume", msg.Topic(), c.cfg.ClientID, len(msg.Payload()), start, nil)
	}
	return c.waitToken(c.cli.Subscribe(topic, qos, callback))
}

// SubscribeMultiple 批量订阅
func (c *Client) SubscribeMultiple(topics map[string]byte, handler MessageHandler) error {
	callback := func(client pahomqtt.Client, msg pahomqtt.Message) {
		start := time.Now()
		handler(msg.Topic(), msg.Payload())
		c.emitMqLog("consume", msg.Topic(), c.cfg.ClientID, len(msg.Payload()), start, nil)
	}
	return c.waitToken(c.cli.SubscribeMultiple(topics, callback))
}

func (c *Client) emitMqLog(direction, topic, consumerGroup string, bodySize int, start time.Time, err error) {
	if c.logger == nil {
		return
	}
	end := time.Now()
	body := logImpl.MqLogBaseModel{
		Backend:        "mqtt",
		Direction:      direction,
		Topic:          topic,
		ConsumerGroup:  consumerGroup,
		BodySize:       bodySize,
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		EndTime:        end.Format(gaia.DateTimeMillsFormat),
		Duration:       float64(end.Sub(start).Milliseconds()),
		StartTimeStamp: start.UnixMilli(),
		EndTimeStamp:   end.UnixMilli(),
	}
	level := gaia.LogInfoLevel
	content := fmt.Sprintf("%s %s", direction, topic)
	if err != nil {
		level = gaia.LogErrorLevel
		body.Err = err.Error()
		content += " err=" + err.Error()
	}
	c.logger.MqLog(level, content)
	c.logger.MqLogBody(level, content, body)
}

// Unsubscribe 取消订阅
func (c *Client) Unsubscribe(topics ...string) error {
	return c.waitToken(c.cli.Unsubscribe(topics...))
}

// IsConnected 检查是否连接
func (c *Client) IsConnected() bool {
	return c.cli.IsConnected()
}

// Close 断开连接（等待 cfg.DisconnectQuiesce 毫秒以便未发送的消息发出）
func (c *Client) Close() {
	c.cli.Disconnect(c.cfg.DisconnectQuiesce)
}
