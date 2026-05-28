// Package logImpl gRPC access log payloads.
// @author gaia-framework
// @created 2026-06-24
//
// gRPC uses this transport-shaped payload before it is normalized into
// AccessLogBaseModel and stored in in_log or out_log.
package logImpl

import "net/http"

// GrpcLogBaseModel gRPC 调用专属字段。
type GrpcLogBaseModel struct {
	FullMethod     string      `json:"full_method"`        // 完整方法名，如 /demo.OrderService/CreateOrder
	Kind           string      `json:"kind"`               // unary / stream
	Peer           string      `json:"peer,omitempty"`     // 对端地址 host:port
	Code           string      `json:"code"`               // gRPC 状态码字符串：OK / Internal / Unauthenticated ...
	Metadata       http.Header `json:"metadata,omitempty"` // 脱敏后的 incoming metadata
	ReqBody        string      `json:"req_body,omitempty"`
	RespBody       string      `json:"resp_body,omitempty"`
	StartTime      string      `json:"start_time"`
	EndTime        string      `json:"end_time"`
	StartTimeStamp int64       `json:"start_time_stamp"`
	EndTimeStamp   int64       `json:"end_time_stamp"`
	Duration       float64     `json:"duration"` // 毫秒
	Err            string      `json:"err,omitempty"`
}
