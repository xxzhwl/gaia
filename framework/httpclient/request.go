// Package httpclient 包注释
// @author wanlizhan
// @created 2024/5/13
package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/tracer"
	"go.opentelemetry.io/otel/attribute"
	otel "go.opentelemetry.io/otel/trace"
)

const (
	DefaultRetryTimes      = 3
	DefaultRetryInterval   = time.Second * 1
	DefaultTimeOut         = 60 * time.Second
	DefaultMaxIdleConns    = 100
	DefaultMaxConnsPerHost = 10
	DefaultIdleConnTimeout = 90 * time.Second
	FullPathKey            = "RequestURL"
	RequestBody            = "CtxRequestBody"
	RequestIdKey           = "CtxRequestId"
	TraceIdKey             = "CtxTraceId"
	RequestMethodKey       = "RequestMethod"
)

var requestBeforeHandler atomic.Value

func init() {
	requestBeforeHandler.Store(ReqBeforeHandler(nil))
}

// SetRequestBeforeHandler 设置请求前置处理器（线程安全）
func SetRequestBeforeHandler(v ReqBeforeHandler) {
	requestBeforeHandler.Store(v)
}

// getRequestBeforeHandler 获取请求前置处理器（线程安全）
func getRequestBeforeHandler() ReqBeforeHandler {
	if v := requestBeforeHandler.Load(); v != nil {
		if handler, ok := v.(ReqBeforeHandler); ok {
			return handler
		}
	}
	return nil
}

// HttpClient 定义HTTP客户端接口
type HttpClient interface {
	Get() ([]byte, int, error)
	Post(data []byte) ([]byte, int, error)
	WithBody(data []byte) *HttpRequest
	WithHeader(headers http.Header) *HttpRequest
	AddHeader(key, value string) *HttpRequest
	WithTimeOut(timeout time.Duration) *HttpRequest
	WithRetryTimes(times int) *HttpRequest
	WithRetryInterval(interval time.Duration) *HttpRequest
	WithMethod(method string) *HttpRequest
	WithCAPem(filePath string) (*HttpRequest, error)
	WithTitle(title string) *HttpRequest
	WithContext(ctx context.Context) *HttpRequest
	Do() ([]byte, int, error)
}

// ClientPool HTTP客户端连接池
type ClientPool struct {
	clients         map[string]*http.Client
	mu              sync.RWMutex
	maxIdleConns    int
	maxConnsPerHost int
	idleConnTimeout time.Duration
}

var defaultPool *ClientPool

func init() {
	defaultPool = &ClientPool{
		clients:         make(map[string]*http.Client),
		maxIdleConns:    DefaultMaxIdleConns,
		maxConnsPerHost: DefaultMaxConnsPerHost,
		idleConnTimeout: DefaultIdleConnTimeout,
	}
}

// GetClient 从连接池获取HTTP客户端
func (p *ClientPool) GetClient(key string) *http.Client {
	p.mu.RLock()
	client, ok := p.clients[key]
	p.mu.RUnlock()

	if ok {
		return client
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 双重检查
	if client, ok := p.clients[key]; ok {
		return client
	}

	// 创建新的HTTP客户端
	transport := &http.Transport{
		MaxIdleConns:    p.maxIdleConns,
		MaxConnsPerHost: p.maxConnsPerHost,
		IdleConnTimeout: p.idleConnTimeout,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	client = &http.Client{
		Timeout:   DefaultTimeOut,
		Transport: transport,
	}

	p.clients[key] = client
	return client
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
	MaxBodySize   int64

	Logger *logImpl.DefaultLogger
	Ctx    context.Context
}

func NewHttpRequest(url string) *HttpRequest {
	logger := logImpl.NewDefaultLogger().SetTitle("HttpRequest")
	return &HttpRequest{
		client:        defaultPool.GetClient("default"),
		RetryTimes:    DefaultRetryTimes,
		RetryInterval: DefaultRetryInterval,
		Method:        http.MethodGet,
		Url:           url,
		Header:        http.Header{},
		Logger:        logger,
		MaxBodySize:   0, // 默认无限制
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

func (h *HttpRequest) WithCAPem(filePath string) (*HttpRequest, error) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取证书文件失败: %w", err)
	}

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

	if len(pemBlocks) == 0 {
		return nil, errors.New("未找到证书块")
	}

	bt := pem.EncodeToMemory(pemBlocks[0])
	c, err := tls.X509KeyPair(bt, pkey)
	if err != nil {
		return nil, fmt.Errorf("解析证书失败: %w", err)
	}

	cfg := &tls.Config{Certificates: []tls.Certificate{c}, InsecureSkipVerify: true}
	tr := &http.Transport{TLSClientConfig: cfg}
	h.client.Transport = tr
	return h, nil
}

func (h *HttpRequest) WithTitle(title string) *HttpRequest {
	h.Title = "HttpRequest-" + title
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

func (h *HttpRequest) WithMaxBodySize(size int64) *HttpRequest {
	h.MaxBodySize = size
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
		// 指数退避重试策略
		retryInterval := h.RetryInterval * time.Duration(i)
		time.Sleep(retryInterval)
		gaia.SendSystemAlarm(h.Title, err.Error())
		h.Logger.WarnF("[%s-%s] Retry Times:%d, Interval:%v", h.Url, h.Title, i, retryInterval)
	}
	return
}

func (h *HttpRequest) do() (respBody []byte, statusCode int, err error) {
	// 检查请求体大小
	if h.MaxBodySize > 0 && int64(len(h.Body)) > h.MaxBodySize {
		return nil, 0, fmt.Errorf("请求体大小超过限制: %d > %d", len(h.Body), h.MaxBodySize)
	}

	//前置处理器发力（线程安全获取）
	if handler := getRequestBeforeHandler(); handler != nil {
		body, code, err := handler(h)
		if err != nil {
			return body, code, err
		}
		//如果有err，不再真正请求，而是返回结果
	}

	reader := bytes.NewReader(h.Body)
	request, err := http.NewRequest(h.Method, h.Url, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("创建请求失败: %w", err)
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
	}

	if h.Ctx == nil {
		h.Ctx = gaia.NewContextTrace().GetParentCtx()
	}

	_, span := tracerInstance.Start(h.Ctx, "out_request-"+title, otel.WithSpanKind(otel.SpanKindClient),
		otel.WithAttributes(attribute.String(FullPathKey, h.Url)),
		otel.WithAttributes(attribute.String(RequestBody, string(h.Body))),
		otel.WithAttributes(attribute.String(RequestMethodKey, h.Method)),
		otel.WithAttributes(attribute.String(RequestIdKey, trace.Id)),
		otel.WithAttributes(attribute.String(TraceIdKey, trace.TraceId)),
	)
	defer span.End()

	resp, err := h.client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("发送请求失败: %w", err)
	}

	defer resp.Body.Close()

	// 检查响应体大小
	if h.MaxBodySize > 0 && resp.ContentLength > h.MaxBodySize {
		return nil, resp.StatusCode, fmt.Errorf("响应体大小超过限制: %d > %d", resp.ContentLength, h.MaxBodySize)
	}

	// 读取响应体
	if h.MaxBodySize > 0 {
		// 限制读取大小
		respBody, err = io.ReadAll(io.LimitReader(resp.Body, h.MaxBodySize))
	} else {
		respBody, err = io.ReadAll(resp.Body)
	}
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应体失败: %w", err)
	}

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
