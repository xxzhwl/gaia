// Package server 包注释
// @author wanlizhan
// @created 2025-04-09
package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

const detailInfo = `
=========================================
Request: 
	Time:%s
	Url:%s
	Method:%s
	Args:%s
- - - - - - - - - - - - - - - - - - - - -
Response: 
	Time:%s
	Status:%3d
Duration:%13v
=========================================
`

const simpleInfo = `[%v ~ %v] | %3d | %13v | %s | %s`

const (
	FullPathKey      = "CtxFullPath"
	RequestBody      = "CtxRequestBody"
	RequestIdKey     = "CtxRequestId"
	TraceIdKey       = "CtxTraceId"
	AuthorizationKey = "CtxAuthorization"
	DisableLogKey    = "DisableLog"
)

func (s *Server) defaultServerLogger() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		followFrom := ctx.Request.Header.Get(gaia.FollowFromKey)
		traceId := ctx.Request.Header.Get(gaia.TraceIdKey)
		//构建请求日志id
		gaia.TraceF("构建请求trace")
		gaia.BuildHttpContextTrace(traceId, string(ctx.Request.Host()), followFrom)
		ctx.Set(RequestIdKey, gaia.GetContextTrace().Id)
		ctx.Set(TraceIdKey, gaia.GetContextTrace().TraceId)
		//请求开始时间
		startTime := time.Now()
		defer func() {
			//请求结束时间
			endTime := time.Now()
			//请求方法
			method := string(ctx.Request.Method())

			if method == http.MethodOptions {
				return
			}
			//请求路径
			path := ctx.URI().String()
			//请求参数
			body := ""
			if ctx.GetBool(BanLoggerKey) {
				body = ctx.GetString(BanLoggerContent)
			} else {
				body = string(ctx.Request.Body())
			}

			//返回状态码
			code := ctx.Response.StatusCode()
			//耗时
			dura := endTime.Sub(startTime)
			logger := logImpl.NewDefaultLogger()

			contentType := string(ctx.Request.Header.ContentType())
			if strings.Contains(contentType, "multipart/form-data") {
				body = "[文件上传] 内容已隐藏"
				ctx.Set(BanLoggerKey, true)
				ctx.Set(BanLoggerContent, "文件上传请求")
			}

			content := ""

			if gaia.GetSafeConfBool(s.schema + ".Logger.DetailMode") {
				content = fmt.Sprintf(detailInfo, startTime.Format(gaia.DateTimeMillsFormat), path, method, body,
					endTime.Format(gaia.DateTimeMillsFormat), code, dura)
			} else {
				content = fmt.Sprintf(simpleInfo,
					startTime.Format(gaia.DateTimeMillsFormat),
					endTime.Format(gaia.DateTimeMillsFormat),
					code,
					dura,
					method,
					path,
				)
			}

			if gaia.GetSafeConfBool(s.schema + ".Logger.PrintConsole") {
				logger.ApiLog(gaia.LogInfoLevel, content)
			}
			if gaia.GetSafeConfBool(s.schema+".Logger.EnablePushLog") && !ctx.GetBool(DisabledPushLoggerKey) {
				reqHeader, respHeader := http.Header{}, http.Header{}
				ctx.Request.Header.VisitAll(func(key, value []byte) {
					reqHeader[string(key)] = []string{string(value)}
				})
				ctx.Response.Header.VisitAll(func(key, value []byte) {
					respHeader[string(key)] = []string{string(value)}
				})

				logBody := logImpl.HttpLogModel{
					Url:            path,
					HttpMethod:     method,
					ReqHeader:      reqHeader,
					ReqBody:        body,
					RespHeader:     respHeader,
					RespBody:       string(ctx.Response.BodyBytes()),
					StartTime:      startTime.Format(gaia.DateTimeMillsFormat),
					EndTime:        endTime.Format(gaia.DateTimeMillsFormat),
					StartTimeStamp: startTime.UnixMilli(),
					EndTimeStamp:   endTime.UnixMilli(),
					HttpStatusCode: ctx.Response.StatusCode(),
					Duration:       float64(endTime.Sub(startTime).Milliseconds()),
				}
				logger.ApiLogBody(gaia.LogInfoLevel, content, logBody)
			}
			// 请求结束时清理上下文
			gaia.RemoveContextTrace()
		}()
		ctx.Next(c)
	}
}
