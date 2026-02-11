// Package httpclient 包注释
// @author wanlizhan
// @created 2024-12-17
package httpclient

import (
	"github.com/xxzhwl/gaia"
)

type ReqBeforeHandler func(req *HttpRequest) (newRespBody []byte, newStatusCode int, newErr error)

var DefaultHandler ReqBeforeHandler = func(req *HttpRequest) (newRespBody []byte, newStatusCode int, newErr error) {
	//如果当前环境有traceId，那就用traceId加进去
	//如果当前没有traceId，那就新建一个traceId进去
	trace := gaia.GetContextTrace()
	if trace == nil {
		gaia.BuildContextTrace()
		trace = gaia.GetContextTrace()
	}
	if trace == nil {
		return
	}
	req.AddHeader(gaia.TraceIdKey, trace.TraceId)
	return
}
