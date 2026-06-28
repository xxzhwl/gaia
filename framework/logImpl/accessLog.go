// Package logImpl access log models.
package logImpl

import (
	"net/http"
	"strconv"
	"strings"
)

const (
	AccessDirectionInbound  = "inbound"
	AccessDirectionOutbound = "outbound"

	AccessProtocolHTTP = "http"
	AccessProtocolGRPC = "grpc"
)

// AccessLogModel is the unified remote document for inbound and outbound access logs.
type AccessLogModel struct {
	LogModel
	AccessLogBaseModel
}

// AccessLogBaseModel stores protocol-neutral access fields plus optional protocol details.
type AccessLogBaseModel struct {
	Direction            string               `json:"direction"`
	Protocol             string               `json:"protocol"`
	Operation            string               `json:"operation"`
	Peer                 string               `json:"peer,omitempty"`
	Status               string               `json:"status"`
	StatusCode           int                  `json:"status_code"`
	Success              bool                 `json:"success"`
	ReqHeader            http.Header          `json:"req_header,omitempty"`
	ReqBody              string               `json:"req_body,omitempty"`
	ReqBodyObjectKey     string               `json:"req_body_object_key,omitempty"`
	ReqBodyObjectURL     string               `json:"req_body_object_url,omitempty"`
	ReqBodyObjectSize    int64                `json:"req_body_object_size,omitempty"`
	ReqBodyObjectSHA256  string               `json:"req_body_object_sha256,omitempty"`
	ReqBodyOffloaded     bool                 `json:"req_body_offloaded,omitempty"`
	RespHeader           http.Header          `json:"resp_header,omitempty"`
	RespBody             string               `json:"resp_body,omitempty"`
	RespBodyObjectKey    string               `json:"resp_body_object_key,omitempty"`
	RespBodyObjectURL    string               `json:"resp_body_object_url,omitempty"`
	RespBodyObjectSize   int64                `json:"resp_body_object_size,omitempty"`
	RespBodyObjectSHA256 string               `json:"resp_body_object_sha256,omitempty"`
	RespBodyOffloaded    bool                 `json:"resp_body_offloaded,omitempty"`
	StartTime            string               `json:"start_time"`
	EndTime              string               `json:"end_time"`
	StartTimeStamp       int64                `json:"start_time_stamp"`
	EndTimeStamp         int64                `json:"end_time_stamp"`
	Duration             float64              `json:"duration"`
	Err                  string               `json:"err,omitempty"`
	HTTP                 *HttpAccessLogFields `json:"http,omitempty"`
	GRPC                 *GrpcAccessLogFields `json:"grpc,omitempty"`
}

type HttpAccessLogFields struct {
	Method     string `json:"method"`
	Url        string `json:"url"`
	StatusCode int    `json:"status_code"`
}

type GrpcAccessLogFields struct {
	FullMethod string `json:"full_method"`
	Service    string `json:"service,omitempty"`
	Method     string `json:"method,omitempty"`
	Kind       string `json:"kind"`
	Code       string `json:"code"`
}

func NewHTTPAccessLog(direction string, body HttpLogModel) AccessLogBaseModel {
	status := strconv.Itoa(body.HttpStatusCode)
	return AccessLogBaseModel{
		Direction:      direction,
		Protocol:       AccessProtocolHTTP,
		Operation:      body.Url,
		Status:         status,
		StatusCode:     body.HttpStatusCode,
		Success:        body.HttpStatusCode >= 200 && body.HttpStatusCode < 400,
		ReqHeader:      body.ReqHeader,
		ReqBody:        body.ReqBody,
		RespHeader:     body.RespHeader,
		RespBody:       body.RespBody,
		StartTime:      body.StartTime,
		EndTime:        body.EndTime,
		StartTimeStamp: body.StartTimeStamp,
		EndTimeStamp:   body.EndTimeStamp,
		Duration:       body.Duration,
		HTTP: &HttpAccessLogFields{
			Method:     body.HttpMethod,
			Url:        body.Url,
			StatusCode: body.HttpStatusCode,
		},
	}
}

func NewGRPCAccessLog(direction string, body GrpcLogBaseModel) AccessLogBaseModel {
	service, method := splitGRPCFullMethod(body.FullMethod)
	return AccessLogBaseModel{
		Direction:      direction,
		Protocol:       AccessProtocolGRPC,
		Operation:      body.FullMethod,
		Peer:           body.Peer,
		Status:         body.Code,
		StatusCode:     grpcCodeNumber(body.Code),
		Success:        body.Code == "OK",
		ReqHeader:      body.Metadata,
		ReqBody:        body.ReqBody,
		RespBody:       body.RespBody,
		StartTime:      body.StartTime,
		EndTime:        body.EndTime,
		StartTimeStamp: body.StartTimeStamp,
		EndTimeStamp:   body.EndTimeStamp,
		Duration:       body.Duration,
		Err:            body.Err,
		GRPC: &GrpcAccessLogFields{
			FullMethod: body.FullMethod,
			Service:    service,
			Method:     method,
			Kind:       body.Kind,
			Code:       body.Code,
		},
	}
}

func splitGRPCFullMethod(fullMethod string) (service, method string) {
	trimmed := strings.TrimPrefix(fullMethod, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) > 0 {
		service = parts[0]
	}
	if len(parts) > 1 {
		method = parts[1]
	}
	return service, method
}

func grpcCodeNumber(code string) int {
	switch code {
	case "OK":
		return 0
	case "Canceled":
		return 1
	case "Unknown":
		return 2
	case "InvalidArgument":
		return 3
	case "DeadlineExceeded":
		return 4
	case "NotFound":
		return 5
	case "AlreadyExists":
		return 6
	case "PermissionDenied":
		return 7
	case "ResourceExhausted":
		return 8
	case "FailedPrecondition":
		return 9
	case "Aborted":
		return 10
	case "OutOfRange":
		return 11
	case "Unimplemented":
		return 12
	case "Internal":
		return 13
	case "Unavailable":
		return 14
	case "DataLoss":
		return 15
	case "Unauthenticated":
		return 16
	default:
		return 2
	}
}
