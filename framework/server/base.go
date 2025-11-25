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

func (r *Request) C() *app.RequestContext {
	return r.c
}

func (r *Request) BanLogger() {
	r.c.Set(BanLoggerKey, true)
}

func (r *Request) resp(data any, err error) {
	ext := map[string]any{
		"request_id": gaia.GetContextTrace().Id,
	}
	//对于一些严重的错误，需要abort
	if err != nil {
		_, span := tracer.GetTracer().Start(r.TraceContext, "response")
		defer span.End()
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err, trace.WithStackTrace(true), trace.WithTimestamp(time.Now()))

		if errwrap.JudgeMajorErr(err) {
			r.c.Abort()
			r.c.JSON(http.StatusOK, Response{Code: errwrap.GetCode(err), Msg: err.Error(), Data: data, Ext: ext})
			return
		}
		r.c.JSON(http.StatusOK, Response{Code: errwrap.GetCode(err), Msg: err.Error(), Data: data, Ext: ext})
		return
	}
	r.c.JSON(http.StatusOK, Response{Code: 0, Msg: "", Data: data, Ext: ext})
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

type HandlerFunc func(req Request) (any, error)

// MakeHandler 供API
func MakeHandler(handler HandlerFunc) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pc := context.Background()
		if v, ok := c.Get("ParentContext"); ok {
			pc = v.(context.Context)
		}

		req := Request{c: c, TraceContext: pc}
		defer func() {
			if r := recover(); r != nil {
				gaia.PanicLog(r)
				req.resp(nil, fmt.Errorf("encounter panic: %v\n", r))
			}
		}()

		req.resp(handler(req))
	}
}

// MakePlugin 供中间件
func MakePlugin(handler Plugin) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		req := Request{c: c}
		if value, exists := c.Get("ParentContext"); exists {
			if parentCtx, ok := value.(context.Context); ok {
				req.TraceContext = parentCtx
			}
		}
		defer func() {
			if r := recover(); r != nil {
				gaia.PanicLog(r)
				req.resp(nil, fmt.Errorf("encounter panic: %v\n", r))
			}
		}()

		//如果中间件顺利通过，不用resp，但失败的话应该直接resp
		if err := handler(req); err != nil {
			req.resp(nil, err)
		}
	}
}

type Plugin func(arg Request) error
