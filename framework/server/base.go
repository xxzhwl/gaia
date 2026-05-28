// Package server 包注释
// @author wanlizhan
// @created 2025-04-09
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/errwrap"
	"github.com/xxzhwl/gaia/framework/tracer"
)

const (
	DefaultCommonQueryFolder    = gaia.DefaultConfigDir + gaia.Sep + "common" + gaia.Sep + "search" + gaia.Sep
	DefaultCommonOperateFolder  = gaia.DefaultConfigDir + gaia.Sep + "common" + gaia.Sep + "operate" + gaia.Sep
	DefaultCommonQueryFileFmt   = DefaultCommonQueryFolder + "%s" + ".json"
	DefaultCommonOperateFileFmt = DefaultCommonOperateFolder + "%s" + ".json"
)

var BanLoggerKey = "BanLogger"
var BanLoggerContent = "BanLoggerContent"
var DisabledPushLoggerKey = "DisabledPushLogger"

type Request struct {
	c *app.RequestContext

	TraceContext context.Context
}

func NewRequest(c *app.RequestContext) *Request {
	ctx := context.Background()
	if v, ok := c.Get("ParentContext"); ok {
		if parentCtx, ok := v.(context.Context); ok {
			ctx = parentCtx
		}
	}
	return &Request{c: c, TraceContext: ctx}
}

func (r *Request) BindJson(obj any) error {
	if err := r.c.BindJSON(obj); err != nil {
		return errwrap.PrefixError("", errors.New("参数解析失败:"+err.Error()))
	}
	return nil
}

func (r *Request) BindJsonWithChecker(obj any) error {
	if err := r.BindJson(obj); err != nil {
		return err
	}

	checker := gaia.NewDataChecker()
	return checker.CheckStructDataValid(obj)
}

// GetUrlParam
//
//	router. GET("/ user/:id", func(c *gin. Context) {
//	   // a GET request to / user/ john
//	   id := c. Param("id") // id == "john"
//	   // a GET request to / user/ john/
//	   id := c. Param("id") // id == "/ john/"
//	})
func (r *Request) GetUrlParam(key string) string {
	return r.c.Param(key)
}

// GetUrlQuery
// GET / path?id=1234&name=Manu&value=
// c. Query("id") == "1234"
// c. Query("name") == "Manu"
// c. Query("value") == ""
// c. Query("wtf") == ""
func (r *Request) GetUrlQuery(key string) string {
	return r.c.Query(key)
}

func (r *Request) GetUrlQueryArray(key string) []string {
	values := make([]string, 0)
	r.c.QueryArgs().VisitAll(func(k, v []byte) {
		if string(k) == key {
			values = append(values, string(v))
		}
	})
	return values
}

func (r *Request) C() *app.RequestContext {
	return r.c
}

func (r *Request) BanLogger(reason string, disabledPush bool) {
	r.c.Set(BanLoggerKey, true)
	r.c.Set(BanLoggerContent, reason)
	r.c.Set(DisabledPushLoggerKey, disabledPush)
}

func (r *Request) resp(data any, err error) {
	requestId := ""
	if trace := gaia.GetContextTrace(); trace != nil {
		requestId = trace.Id
	}
	ext := map[string]any{
		"request_id": requestId,
		"timestamp":  time.Now().UnixMilli(),
	}
	//对于一些严重的错误，需要abort
	if err != nil {
		// tracer 可能因 SetupTracer 失败而为 nil，这里做防御，避免 panic
		if tracerInstance := tracer.GetTracer(); tracerInstance != nil {
			_, span := tracerInstance.Start(r.TraceContext, "response")
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err, trace.WithStackTrace(true), trace.WithTimestamp(time.Now()))
			span.End()
		}

		errorCode := errwrap.GetCode(err)
		if errorCode <= 0 {
			errorCode = 500 // 默认服务器错误码
		}

		// 根据错误码设置 HTTP 状态码：
		// - 业务错误码若是合法 HTTP 状态码（4xx/5xx），直接复用；
		//   这样能精确还原 401 / 403 / 404 / 409 / 422 / 429 等语义。
		// - 业务码 < 1000（小段位）按段映射；其余统一 200，由 Code 字段承担语义。
		httpStatus := http.StatusOK
		switch {
		case errorCode >= 400 && errorCode < 600:
			httpStatus = int(errorCode)
		case errorCode >= 600 && errorCode < 1000:
			// 600~999 之间不是合法 HTTP 状态码，但属于"业务错误"语义，
			// 统一回退为 400（客户端错误），避免下发非法 status code。
			httpStatus = http.StatusBadRequest
		}

		if errorCode < 1000 && errorCode > 0 {
			r.c.Abort()
			r.c.JSON(httpStatus, Response{Code: errorCode, Msg: err.Error(), Data: data, Ext: ext})
			return
		}
		r.c.JSON(httpStatus, Response{Code: errorCode, Msg: err.Error(), Data: data, Ext: ext})
		return
	}
	r.c.JSON(http.StatusOK, Response{Code: 0, Msg: "操作成功", Data: data, Ext: ext})
}

type Response struct {
	Code int64          `json:"code"`
	Msg  string         `json:"msg"`
	Data any            `json:"data"`
	Ext  map[string]any `json:"ext"`
}

type ListResponse[E any] struct {
	List    []E   `json:"list"`
	HasNext bool  `json:"has_next"`
	Total   int64 `json:"total"`
}

type HandlerFunc[Resp any] func(req Request) (Resp, error)

// MakeHandler 供API
func MakeHandler[Resp any](handler HandlerFunc[Resp]) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pc := ctx
		if v, ok := c.Get("ParentContext"); ok {
			if parentCtx, ok := v.(context.Context); ok {
				pc = parentCtx
			}
		}

		req := Request{c: c, TraceContext: pc}
		defer func() {
			if r := recover(); r != nil {
				// 把"METHOD path"作为业务上下文带进告警，飞书消息能直接看到出问题的接口，
				// 不用再回查 access log 反推。注意 path 不带 query string，避免敏感参数泄漏。
				gaia.PanicLogWithExtra(r, fmt.Sprintf("%s %s",
					string(c.Request.Method()), string(c.Request.URI().Path())))
				req.resp(nil, fmt.Errorf("encounter panic: %v", r))
			}
		}()

		req.resp(handler(req))
	}
}

// MakePlugin 供中间件
func MakePlugin(handler Plugin) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		req := Request{c: c, TraceContext: ctx}
		if value, exists := c.Get("ParentContext"); exists {
			if parentCtx, ok := value.(context.Context); ok {
				req.TraceContext = parentCtx
			}
		}
		defer func() {
			if r := recover(); r != nil {
				gaia.PanicLogWithExtra(r, fmt.Sprintf("[plugin] %s %s",
					string(c.Request.Method()), string(c.Request.URI().Path())))
				req.resp(nil, fmt.Errorf("encounter panic: %v", r))
			}
		}()

		//如果中间件顺利通过，不用resp，但失败的话应该直接resp并阻断后续handler
		if err := handler(req); err != nil {
			req.resp(nil, err)
			c.Abort()
		}
	}
}

type Plugin func(arg Request) error

// ErrorHandler 统一错误处理插件
func ErrorHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if r := recover(); r != nil {
				gaia.PanicLogWithExtra(r, fmt.Sprintf("[error-handler] %s %s",
					string(c.Request.Method()), string(c.Request.URI().Path())))
				req := NewRequest(c)
				req.resp(nil, fmt.Errorf("服务器内部错误: %v", r))
			}
		}()

		c.Next(ctx)
	}
}
