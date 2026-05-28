// Package logImpl 注释
// @author wanlizhan
// @created 2026/6/1
//
// CacheLog 模型：缓存读写日志（Redis / 本地 LRU / Memcache 等）。
// 重点诉求是"命中率 / 慢操作 / 穿透"三类指标，因此 Hit 用 *bool：
//   - nil      非读类操作（set/del 等），ES 端按 missing 过滤即可
//   - &true    命中
//   - &false   未命中（回源）
package logImpl

// CacheLogModel 完整的缓存日志 ES 文档。
type CacheLogModel struct {
	LogModel
	CacheLogBaseModel
}

// CacheLogBaseModel 缓存操作专属字段。
//
// Op 取值（保持小写、与 redis 命令家族对齐，便于 grafana 直接做面板）：
//   - "get" / "mget" / "exists"
//   - "set" / "setnx" / "mset"
//   - "del" / "expire" / "ttl"
//   - "incr" / "decr"
//   - "hget" / "hset" / "hdel" / "lpush" / "rpop" / "zadd" ...
//
// Key 默认建议传完整 key，由调用方决定脱敏；如担心高基数（cardinality 爆炸），
// 也可以只传 key prefix（如 "user:profile:*"），ES mapping 时把 key 设成 keyword + 较低 ignore_above。
type CacheLogBaseModel struct {
	Backend        string  `json:"backend"`             // redis / local / memcache ...
	Op             string  `json:"op"`                  // 操作类型，见 doc 注释
	Key            string  `json:"key,omitempty"`       // 缓存 key 或 key prefix
	Hit            *bool   `json:"hit,omitempty"`       // 仅 get 类操作有值
	TTL            int64   `json:"ttl,omitempty"`       // 秒；set 时为新写入的 TTL
	BodySize       int     `json:"body_size,omitempty"` // value 字节数
	StartTime      string  `json:"start_time"`
	EndTime        string  `json:"end_time"`
	Duration       float64 `json:"duration"` // 毫秒
	StartTimeStamp int64   `json:"start_time_stamp"`
	EndTimeStamp   int64   `json:"end_time_stamp"`
	Err            string  `json:"err,omitempty"`
}
