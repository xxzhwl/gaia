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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/tracer"
	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
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

func newDefaultTransport(maxIdleConns, maxConnsPerHost int, idleConnTimeout time.Duration) *http.Transport {
	return &http.Transport{
		MaxIdleConns:    maxIdleConns,
		MaxConnsPerHost: maxConnsPerHost,
		IdleConnTimeout: idleConnTimeout,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

func cloneRoundTripper(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		return newDefaultTransport(DefaultMaxIdleConns, DefaultMaxConnsPerHost, DefaultIdleConnTimeout)
	}
	if tr, ok := rt.(*http.Transport); ok {
		return tr.Clone()
	}
	return rt
}

func cloneHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		return &http.Client{
			Timeout:   DefaultTimeOut,
			Transport: newDefaultTransport(DefaultMaxIdleConns, DefaultMaxConnsPerHost, DefaultIdleConnTimeout),
		}
	}

	c := *base
	c.Transport = cloneRoundTripper(base.Transport)
	return &c
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
	transport := newDefaultTransport(p.maxIdleConns, p.maxConnsPerHost, p.idleConnTimeout)

	client = &http.Client{
		Timeout:   DefaultTimeOut,
		Transport: transport,
	}

	p.clients[key] = client
	return client
}

type HttpRequest struct {
	client        *http.Client
	customTimeout bool
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

	// 仅在 do() 内部使用，供 defer writeOutLog 读取响应头
	lastRespHeader http.Header
}

// 包级共享 Logger，避免每个请求创建新实例
var httpRequestLogger *logImpl.DefaultLogger

func init() {
	httpRequestLogger = logImpl.NewDefaultLogger().SetTitle("HttpRequest")
}

func NewHttpRequest(url string) *HttpRequest {
	baseClient := defaultPool.GetClient("default")
	return &HttpRequest{
		client:        cloneHTTPClient(baseClient),
		RetryTimes:    DefaultRetryTimes,
		RetryInterval: DefaultRetryInterval,
		Method:        http.MethodGet,
		Url:           url,
		Header:        http.Header{},
		Logger:        httpRequestLogger,
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
	h.client = &http.Client{
		Timeout:   h.client.Timeout,
		Transport: tr,
	}
	return h, nil
}

func (h *HttpRequest) WithTitle(title string) *HttpRequest {
	h.Title = "HttpRequest-" + title
	// 不能在包级共享 logger 上调 SetTitle（会被并发请求互相覆盖）
	// 这里返回一个轻量子 logger，仅 title 不同，底层共享 logChan/goroutine
	if h.Logger != nil {
		h.Logger = h.Logger.CloneWithTitle("HttpRequest-" + title)
	}
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
	if !h.customTimeout {
		h.client = &http.Client{
			Timeout:   timeout,
			Transport: h.client.Transport,
		}
		h.customTimeout = true
	} else {
		h.client.Timeout = timeout
	}
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
	maxAttempts := h.RetryTimes + 1
	for i := 1; i <= maxAttempts; i++ {
		respBody, statusCode, err = h.do()
		if err == nil {
			return
		}
		if i >= maxAttempts {
			break
		}
		retryInterval := h.RetryInterval * time.Duration(i)
		time.Sleep(retryInterval)
		h.Logger.WarnF("[%s-%s] Retry Times:%d, Interval:%v, Error:%s", h.Url, h.Title, i, retryInterval, err.Error())
	}
	if err != nil {
		gaia.SendSystemAlarm(h.Title, fmt.Sprintf("请求最终失败(重试%d次): %s", h.RetryTimes, err.Error()))
	}
	return
}

func (h *HttpRequest) do() (respBody []byte, statusCode int, err error) {
	// 记录请求起止时间与上下文，使用 defer 保证任何退出路径都会记录 OutLog
	startTime := time.Now()
	var reqHeader http.Header
	defer func() {
		h.writeOutLog(startTime, time.Now(), reqHeader, nil, respBody, statusCode, err)
	}()

	// 检查请求体大小
	if h.MaxBodySize > 0 && int64(len(h.Body)) > h.MaxBodySize {
		err = fmt.Errorf("请求体大小超过限制: %d > %d", len(h.Body), h.MaxBodySize)
		return nil, 0, err
	}

	//前置处理器发力（线程安全获取）
	if handler := getRequestBeforeHandler(); handler != nil {
		body, code, handlerErr := handler(h)
		if handlerErr != nil {
			respBody, statusCode, err = body, code, handlerErr
			return
		}
		if len(body) > 0 || code > 0 {
			respBody, statusCode, err = body, code, nil
			return
		}
	}

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
		_, terr := tracer.SetupTracer(context.Background(), gaia.GetSystemEnName())
		if terr != nil {
			gaia.WarnF("SetUpTracer error: %s", terr.Error())
		}
		tracerInstance = tracer.GetTracer()
	}

	if h.Ctx == nil {
		h.Ctx = gaia.NewContextTrace().GetParentCtx()
		if h.Ctx == nil {
			h.Ctx = context.Background()
		}
	}

	requestCtx, span := tracerInstance.Start(h.Ctx, "out_request-"+title, otel.WithSpanKind(otel.SpanKindClient),
		otel.WithAttributes(attribute.String(FullPathKey, h.Url)),
		otel.WithAttributes(attribute.String(RequestMethodKey, h.Method)),
		otel.WithAttributes(attribute.String(RequestIdKey, trace.Id)),
		otel.WithAttributes(attribute.String(TraceIdKey, trace.TraceId)),
	)
	defer span.End()

	reader := bytes.NewReader(h.Body)
	request, reqErr := http.NewRequestWithContext(requestCtx, h.Method, h.Url, reader)
	if reqErr != nil {
		err = fmt.Errorf("创建请求失败: %w", reqErr)
		return nil, 0, err
	}
	if h.Header != nil {
		request.Header = h.Header
	} else {
		h.Header = request.Header
	}
	reqHeader = request.Header
	otelglobal.GetTextMapPropagator().Inject(requestCtx, propagation.HeaderCarrier(request.Header))

	resp, doErr := h.client.Do(request)
	if doErr != nil {
		err = fmt.Errorf("发送请求失败: %w", doErr)
		return nil, 0, err
	}
	defer resp.Body.Close()

	// 检查响应体大小
	if h.MaxBodySize > 0 && resp.ContentLength > h.MaxBodySize {
		statusCode = resp.StatusCode
		err = fmt.Errorf("响应体大小超过限制: %d > %d", resp.ContentLength, h.MaxBodySize)
		return nil, statusCode, err
	}

	// 读取响应体
	var readErr error
	if h.MaxBodySize > 0 {
		respBody, readErr = io.ReadAll(io.LimitReader(resp.Body, h.MaxBodySize))
	} else {
		respBody, readErr = io.ReadAll(resp.Body)
	}
	if readErr != nil {
		statusCode = resp.StatusCode
		err = fmt.Errorf("读取响应体失败: %w", readErr)
		return nil, statusCode, err
	}

	statusCode = resp.StatusCode
	// 供 defer 使用，writeOutLog 里以参数传入 respHeader
	h.lastRespHeader = resp.Header
	return
}

// writeOutLog 统一写出向请求日志，覆盖成功 / 失败 / 超限 / 超时等所有路径。
// level 根据 statusCode 与 err 动态选择，避免 4xx/5xx 被当 Info 需人肉过滤。
func (h *HttpRequest) writeOutLog(startTime, endTime time.Time, reqHeader http.Header, respHeader http.Header, respBody []byte, statusCode int, err error) {
	if respHeader == nil {
		respHeader = h.lastRespHeader
	}
	if reqHeader == nil {
		reqHeader = h.Header
	}

	level := gaia.LogInfoLevel
	switch {
	case err != nil, statusCode >= 500:
		level = gaia.LogErrorLevel
	case statusCode >= 400:
		level = gaia.LogWarnLevel
	}

	content := fmt.Sprintf("%s-%s | %.2fms | %d | %s | %s",
		startTime.Format("2006-01-02 15:04:05.000"), endTime.Format("2006-01-02 15:04:05.000"),
		float64(endTime.Sub(startTime).Milliseconds()), statusCode, h.Method, h.Url)
	if err != nil {
		content = content + " | err=" + err.Error()
	}

	logBody := logImpl.HttpLogModel{
		Url:            h.Url,
		HttpMethod:     h.Method,
		ReqHeader:      sanitizeHeaders(reqHeader),
		ReqBody:        sanitizeBodyForLog(h.Body),
		RespHeader:     sanitizeHeaders(respHeader),
		RespBody:       sanitizeBodyForLog(respBody),
		StartTime:      startTime.Format("2006-01-02 15:04:05.000"),
		EndTime:        endTime.Format("2006-01-02 15:04:05.000"),
		StartTimeStamp: startTime.UnixMilli(),
		EndTimeStamp:   endTime.UnixMilli(),
		HttpStatusCode: statusCode,
		Duration:       float64(endTime.Sub(startTime).Milliseconds()),
	}
	h.Logger.OutLog(level, content)
	h.Logger.OutLogBody(level, content, logImpl.NewHTTPAccessLog(logImpl.AccessDirectionOutbound, logBody))
}

func sanitizeHeaders(headers http.Header) http.Header {
	if headers == nil {
		return nil
	}

	safe := make(http.Header, len(headers))
	for key, values := range headers {
		lowerKey := strings.ToLower(key)
		switch lowerKey {
		case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-access-token":
			safe[key] = []string{"[REDACTED]"}
		default:
			copied := make([]string, len(values))
			copy(copied, values)
			safe[key] = copied
		}
	}

	return safe
}

func sanitizeBodyForLog(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	if !gaia.GetSafeConfBool("HttpClient.LogBody") {
		return fmt.Sprintf("[REDACTED len=%d]", len(body))
	}

	const maxLogBodyLength = 4096
	if len(body) <= maxLogBodyLength {
		return string(body)
	}

	return fmt.Sprintf("%s...[truncated %d bytes]", string(body[:maxLogBodyLength]), len(body)-maxLogBodyLength)
}
