// Package logImpl 注释
// @author wanlizhan
// @created 2026/6/1
//
// MqLog 模型：消息队列（Kafka / RocketMQ / Pulsar 等）生产 & 消费日志。
// 设计上把"方向（produce/consume）"做成字段而不是两个 LogType，
// 因为绝大多数 MQ 监控/告警是按 topic + consumer_group 维度聚合的，
// 同 index 内做 term agg 比跨 index join 更友好。
package logImpl

// MqLogModel 完整的 MQ 日志 ES 文档（公共 LogModel + MQ 专属字段）。
type MqLogModel struct {
	LogModel
	MqLogBaseModel
}

// MqLogBaseModel 消息队列专属字段。
//
// Direction 取值：
//   - "produce"  生产侧，对应 send 一条消息
//   - "consume"  消费侧，对应一次 handler 处理（含失败/重试）
//
// 对于消费失败时，错误信息写到 Err；上层应把 logLevel 提到 Error 以便聚合告警。
type MqLogBaseModel struct {
	Backend        string  `json:"backend"`                  // kafka / rocketmq / pulsar / nsq ...
	Direction      string  `json:"direction"`                // produce / consume
	Topic          string  `json:"topic"`                    // topic 名
	Partition      int32   `json:"partition,omitempty"`      // 分区号（部分中间件无意义则 0）
	Offset         int64   `json:"offset,omitempty"`         // 消息偏移
	Key            string  `json:"key,omitempty"`            // 消息 key（可用于路由 / 顺序）
	ConsumerGroup  string  `json:"consumer_group,omitempty"` // 仅 consume 端有意义
	BodySize       int     `json:"body_size,omitempty"`      // 消息体字节数（不入 body 防止 ES 文档膨胀）
	StartTime      string  `json:"start_time"`
	EndTime        string  `json:"end_time"`
	Duration       float64 `json:"duration"` // 毫秒
	StartTimeStamp int64   `json:"start_time_stamp"`
	EndTimeStamp   int64   `json:"end_time_stamp"`
	Err            string  `json:"err,omitempty"`
}
