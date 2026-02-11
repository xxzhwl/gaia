// Package kafka 包注释
// @author wanlizhan
// @created 2024/5/16
package kafka

import (
	"github.com/segmentio/kafka-go"
	"net"
	"strconv"
)

type Client struct {
	kafkaClient *kafka.Conn
}

func NewClient(endPoint string) (*Client, error) {
	conn, err := kafka.Dial("tcp", endPoint)
	if err != nil {
		return nil, err
	}

	broker, err := conn.Controller()
	if err != nil {
		return nil, err
	}
	var controllerConn *kafka.Conn
	controllerConn, err = kafka.Dial("tcp", net.JoinHostPort(broker.Host, strconv.Itoa(broker.Port)))
	if err != nil {
		return nil, err
	}

	return &Client{kafkaClient: controllerConn}, nil
}

func (c *Client) CreateTopic(topic string, partitionNum int, replicationFactor int) error {
	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             topic,
			NumPartitions:     partitionNum,
			ReplicationFactor: replicationFactor,
		},
	}
	return c.kafkaClient.CreateTopics(topicConfigs...)
}
