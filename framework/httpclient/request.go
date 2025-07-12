// Package httpclient 包注释
// @author wanlizhan
// @created 2024/5/13
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel/attribute"
	otel "go.opentelemetry.io/otel/trace"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	DefaultRetryTimes    = 3
	DefaultRetryInterval = time.Second * 1
	DefaultTimeOut       = 60 * time.Second
	FullPathKey          = "RequestURL"
	RequestBody          = "CtxRequestBody"
	RequestIdKey         = "CtxRequestId"
	TraceIdKey           = "CtxTraceId"
	RequestMethodKey     = "RequestMethod"
)

var requestBeforeHandler ReqBeforeHandler

func SetRequestBeforeHandler(v ReqBeforeHandler) {
	requestBeforeHandler = v
}

type HttpRequest struct {
	client        *http.Client
	Method        string
	Title         string
	Url           string
	RetryTimes    int
	RetryInterval time.Duration
	Body          []byte
	Header        http.Header

	Logger *logImpl.DefaultLogger
	Ctx    context.Context
}

func NewHttpRequest(url string) *HttpRequest {
	logger := logImpl.NewDefaultLogger().SetTitle("HttpRequest")
	return &HttpRequest{
		client:        &http.Client{Timeout: DefaultTimeOut},
		RetryTimes:    DefaultRetryTimes,
		RetryInterval: DefaultRetryInterval,
		Method:        http.MethodGet,
		Url:           url,
		Header:        http.Header{},
		Logger:        logger,
	}
}

func (h *HttpRequest) Post(data []byte) (respBody []byte, statusCode int, err error) {
	h.Method = http.MethodPost
	return h.WithBody(data).Do()
}

func (h *HttpRequest) Get() (respBody []byte, statusCode int, err error) {
	h.Method = http.MethodGet
	return h.Do()
}

func (h *HttpRequest) WithCAPem(filePath string) *HttpRequest {
	b, _ := os.ReadFile(filePath)
	pem.Decode(b)
	var pemBlocks []*pem.Block
	var v *pem.Block
	var pkey []byte
	for {
		v, b = pem.Decode(b)
		if v == nil {
			break
		}
		if v.Type == "PRIVATE KEY" {
			pkey = pem.EncodeToMemory(v)
		} else {
			pemBlocks = append(pemBlocks, v)
		}
	}

	bt := pem.EncodeToMemory(pemBlocks[0])
	//keyString := string(pkey)
	//CertString := string(bytes)
	//pool := x509.NewCertPool()
	c, _ := tls.X509KeyPair(bt, pkey)
	//pool.AppendCertsFromPEM(b)
	cfg := &tls.Config{Certificates: []tls.Certificate{c}, InsecureSkipVerify: true}
	tr := http.Transport{TLSClientConfig: cfg}
	h.client.Transport = &tr
	return h
}

func (h *HttpRequest) WithTitle(title string) *HttpRequest {
	h.Title = title
	h.Logger.SetTitle(title)
	return h
}

func (h *HttpRequest) WithUrl(url string) *HttpRequest {
	h.Url = url
	return h
}

func (h *HttpRequest) WithContext(ctx context.Context) *HttpRequest {
	h.Ctx = ctx
	return h
}

func (h *HttpRequest) WithRetryDuration(duration time.Duration) *HttpRequest {
	h.RetryInterval = duration
	return h
}

func (h *HttpRequest) WithBody(body []byte) *HttpRequest {
	h.Body = body
	return h
}

func (h *HttpRequest) WithTimeOut(timeout time.Duration) *HttpRequest {
	h.client.Timeout = timeout
	return h
}

func (h *HttpRequest) WithRetryTimes(times int) *HttpRequest {
	h.RetryTimes = times
	return h
}

func (h *HttpRequest) WithMethod(method string) *HttpRequest {
	h.Method = method
	return h
}

func (h *HttpRequest) WithHeader(headers http.Header) *HttpRequest {
	h.Header = headers
	return h
}

func (h *HttpRequest) AddHeader(key, value string) *HttpRequest {
	h.Header.Add(key, value)
	return h
}

func (h *HttpRequest) Do() (respBody []byte, statusCode int, err error) {
	for i := 1; i <= h.RetryTimes+1; i++ {
		respBody, statusCode, err = h.do()
		if err == nil {
			return
		}
		time.Sleep(h.RetryInterval)
		h.Logger.WarnF("[%s-%s] Retry Times:%d", h.Url, h.Title, i)
	}
	return
}

func (h *HttpRequest) do() (respBody []byte, statusCode int, err error) {
	//前置处理器发力
	if requestBeforeHandler != nil {
		body, code, err := requestBeforeHandler(h)
		if err != nil {
			return body, code, err
		}
		//如果有err，不再真正请求，而是返回结果
	}

	reader := bytes.NewReader(h.Body)
	request, err := http.NewRequest(h.Method, h.Url, reader)
	if err != nil {
		return
	}
	request.Header = h.Header

	startTime := time.Now()

	trace := gaia.GetContextTrace()
	if trace == nil {
		gaia.BuildContextTrace()
		defer gaia.RemoveContextTrace()
		trace = gaia.GetContextTrace()
	}
	title := h.Title
	if len(title) == 0 {
		title = h.Method + "-" + h.Url
	}
	tracerInstance := tracer.GetTracer()
	if tracerInstance == nil {
		_, err = tracer.SetupTracer(context.Background(), gaia.GetSystemEnName())
		if err != nil {
			gaia.WarnF("SetUpTracer error: %s", err.Error())
		}
		tracerInstance = tracer.GetTracer()
	} else {
		_, span := tracerInstance.Start(h.Ctx, "out_request-"+title, otel.WithSpanKind(otel.SpanKindClient),
			otel.WithAttributes(attribute.String(FullPathKey, h.Url)),
			otel.WithAttributes(attribute.String(RequestBody, string(h.Body))),
			otel.WithAttributes(attribute.String(RequestMethodKey, h.Method)),
			otel.WithAttributes(attribute.String(RequestIdKey, trace.Id)),
			otel.WithAttributes(attribute.String(TraceIdKey, trace.TraceId)),
		)
		defer span.End()
	}

	resp, err := h.client.Do(request)
	if err != nil {
		return
	}

	respBody, err = io.ReadAll(resp.Body)
	statusCode = resp.StatusCode

	endTime := time.Now()
	logBody := logImpl.HttpLogModel{
		Url:            h.Url,
		HttpMethod:     h.Method,
		ReqHeader:      h.Header,
		ReqBody:        string(h.Body),
		RespHeader:     resp.Header,
		RespBody:       string(respBody),
		StartTime:      startTime.Format("2006-01-02 15:04:05.000"),
		EndTime:        endTime.Format("2006-01-02 15:04:05.000"),
		StartTimeStamp: startTime.UnixMilli(),
		EndTimeStamp:   endTime.UnixMilli(),
		HttpStatusCode: statusCode,
		Duration:       float64(endTime.Sub(startTime).Milliseconds()),
	}
	content := fmt.Sprintf("%s-%s | %.2fms | %s | %s",
		startTime.Format("2006-01-02 15:04:05.000"), endTime.Format("2006-01-02 15:04:05.000"),
		float64(endTime.Sub(startTime).Milliseconds()), h.Method, h.Url)
	h.Logger.OutLog(gaia.LogInfoLevel, content)
	h.Logger.OutLogBody(gaia.LogInfoLevel, content, logBody)
	return
}
