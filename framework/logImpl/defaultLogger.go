// Package logImpl 注释
// @author wanlizhan
// @created 2024/4/27
package logImpl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/color"
	"github.com/xxzhwl/gaia/components/buffer"
	"github.com/xxzhwl/gaia/components/es"
	"github.com/xxzhwl/gaia/g"
)

var logTimeFormat = "2006-01-02 15:04:05.000"

var DefaultLoggerPath = "var" + gaia.Sep + "logs" + gaia.Sep

// remoteLogEnabled 指示远程日志推送是否启用。
//
// 状态由 framework.Init 设置，后台 watcher 可能周期性地切换：
//   - 0 (false) 表示禁用：PushLog 直接 no-op，不会尝试连 ES
//   - 1 (true)  表示启用：走正常推送路径
//
// 默认值 0 (禁用) —— 保守策略：在 framework.Init 明确判定 ES 可用之前，
// 先不做任何远程推送，避免启动期产生雪崩式的 "创建ES客户端失败" 错误日志。
// 使用 atomic.Int32 以支持无锁并发读写（PushLog 位于热路径）。
var remoteLogEnabled atomic.Int32

// SetRemoteLogEnabled 开关远程日志推送（由 framework 层调用）。
// 该函数幂等：重复设置相同值不会产生副作用。
// 用户可以通过配置 "Logger.DisableRemote=true" 硬性禁用（优先级最高，框架无法覆盖）。
func SetRemoteLogEnabled(enabled bool) {
	var newVal int32
	if enabled {
		newVal = 1
	}
	old := remoteLogEnabled.Swap(newVal)
	if old == newVal {
		return
	}
	if enabled {
		gaia.Println(gaia.LogInfoLevel, "[Logger] 远程日志推送已启用")
	} else {
		gaia.Println(gaia.LogWarnLevel, "[Logger] 远程日志推送已禁用")
	}
}

// IsRemoteLogEnabled 返回远程日志推送是否启用。
func IsRemoteLogEnabled() bool {
	return remoteLogEnabled.Load() == 1
}

// SanitizeHttpHeaders 复用于外部调用者（如 server 中间件）的公共脱敏：
// 涉及身份 / 令牌 / Cookie 类字段统一被替换为 [REDACTED]。
func SanitizeHttpHeaders(headers http.Header) http.Header {
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

const (
	InLogIndex        = "in_log"
	OutLogIndex       = "out_log"
	SysLogIndex       = "sys_log"
	DbLogIndex        = "db_log"
	MqLogIndex        = "mq_log"
	CacheLogIndex     = "cache_log"
	AsyncTaskLogIndex = "async_task_log"
	JobLogIndex       = "job_log"
)

type LogModel struct {
	LogId               string `json:"log_id"`
	TraceId             string `json:"trace_id"`
	PId                 string `json:"p_id"`
	GoId                string `json:"go_id"`
	SystemName          string `json:"system_name"`
	LogType             string `json:"log_type"`
	LogTitle            string `json:"log_title"`
	LogLevel            string `json:"log_level"`
	Content             string `json:"content"`
	ContentObjectKey    string `json:"content_object_key,omitempty"`
	ContentObjectURL    string `json:"content_object_url,omitempty"`
	ContentObjectSize   int64  `json:"content_object_size,omitempty"`
	ContentObjectSHA256 string `json:"content_object_sha256,omitempty"`
	ContentOffloaded    bool   `json:"content_offloaded,omitempty"`
	LogTime             string `json:"log_time"`
	LogTimeStamp        int64  `json:"log_time_stamp"`
	TraceStack          string `json:"trace_stack,omitempty"`
}

type HttpLogModel struct {
	Url            string      `json:"url"`
	HttpMethod     string      `json:"http_method"`
	ReqHeader      http.Header `json:"req_header,omitempty"`
	ReqBody        string      `json:"req_body,omitempty"`
	RespHeader     http.Header `json:"resp_header,omitempty"`
	RespBody       string      `json:"resp_body,omitempty"`
	StartTime      string      `json:"start_time"`
	EndTime        string      `json:"end_time"`
	StartTimeStamp int64       `json:"start_time_stamp"`
	EndTimeStamp   int64       `json:"end_time_stamp"`
	HttpStatusCode int         `json:"http_status_code"`
	Duration       float64     `json:"duration"`
}

type DefaultLogger struct {
	title      string
	timeFormat string
	writer     io.Writer

	logId string

	locker sync.RWMutex

	ShowLoggerLevel gaia.LogLevel

	logChan  chan string
	stopChan chan struct{}
	doneChan chan struct{} // flusher goroutine 退出后关闭
	stopOnce sync.Once

	// 可配置参数
	bufferSize    int
	flushInterval time.Duration
	maxFileSize   int64
	maxFileCount  int
	enableColor   bool // 实例级别的颜色配置

	// 启动时缓存日志根目录，避免运行时 os.Getwd 受 chdir 影响
	baseDir string
}

// LoggerConfig 日志配置结构体
type LoggerConfig struct {
	BufferSize      int
	FlushInterval   time.Duration
	MaxFileSize     int64
	MaxFileCount    int
	ShowLoggerLevel gaia.LogLevel
	EnableColor     bool // 实例级别的颜色配置
}

func NewDefaultLogger() *DefaultLogger {
	return NewDefaultLoggerWithConfig(LoggerConfig{
		BufferSize:      1000,
		FlushInterval:   2 * time.Second,
		MaxFileSize:     100 * 1024 * 1024, // 100MB
		MaxFileCount:    10,
		ShowLoggerLevel: gaia.LogInfoLevel,
		EnableColor:     false,
	})
}

// NewDefaultLoggerWithConfig 创建带配置的默认日志记录器
func NewDefaultLoggerWithConfig(config LoggerConfig) *DefaultLogger {
	if config.BufferSize <= 0 {
		config.BufferSize = 1000
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 2 * time.Second
	}
	if config.MaxFileSize <= 0 {
		config.MaxFileSize = 100 * 1024 * 1024 // 100MB
	}
	if config.MaxFileCount <= 0 {
		config.MaxFileCount = 10
	}
	if config.ShowLoggerLevel == 0 {
		config.ShowLoggerLevel = gaia.LogInfoLevel
	}

	// 启动时记录日志根目录，避免运行时 os.Chdir 影响
	baseDir, err := os.Getwd()
	if err != nil {
		gaia.Println(gaia.LogWarnLevel, fmt.Sprintf("[Logger] os.Getwd 失败，使用相对路径: %v", err))
		baseDir = "."
	}

	logger := &DefaultLogger{
		timeFormat:      logTimeFormat,
		title:           "Default",
		ShowLoggerLevel: config.ShowLoggerLevel,
		logChan:         make(chan string, config.BufferSize),
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
		bufferSize:      config.BufferSize,
		flushInterval:   config.FlushInterval,
		maxFileSize:     config.MaxFileSize,
		maxFileCount:    config.MaxFileCount,
		enableColor:     config.EnableColor, // 使用实例级别的颜色配置
		baseDir:         baseDir,
	}
	logger.startLogFlusher()
	return logger
}

func NewLogger(title string) gaia.IBaseLog {
	logger := NewDefaultLogger()
	logger.SetTitle(title)
	return logger
}

// NewLoggerWithConfig 创建带配置的日志记录器
func NewLoggerWithConfig(title string, config LoggerConfig) gaia.IBaseLog {
	logger := NewDefaultLoggerWithConfig(config)
	logger.SetTitle(title)
	return logger
}

func (d *DefaultLogger) SetShowLoggerLevel(level gaia.LogLevel) {
	d.ShowLoggerLevel = level
}

func (d *DefaultLogger) SetTitle(title string) *DefaultLogger {
	d.locker.Lock()
	defer d.locker.Unlock()
	d.title = title
	return d
}

// CloneWithTitle 派生一个新的 logger 视图，仅 title 不同；
// 底层共享 logChan / flusher goroutine / baseDir 等运行时资源，开销极小。
// 用于请求级（如 HttpRequest 单次请求）需要独立 title 又不想启动新 goroutine 的场景。
// 派生 logger 不应再调用 Stop()。
func (d *DefaultLogger) CloneWithTitle(title string) *DefaultLogger {
	if d == nil {
		return nil
	}
	d.locker.RLock()
	parentTimeFormat := d.timeFormat
	parentWriter := d.writer
	parentLevel := d.ShowLoggerLevel
	parentLogChan := d.logChan
	parentStopChan := d.stopChan
	parentDoneChan := d.doneChan
	parentBaseDir := d.baseDir
	parentBufSize := d.bufferSize
	parentFlush := d.flushInterval
	parentMaxFile := d.maxFileSize
	parentMaxCnt := d.maxFileCount
	parentColor := d.enableColor
	d.locker.RUnlock()

	return &DefaultLogger{
		title:           title,
		timeFormat:      parentTimeFormat,
		writer:          parentWriter,
		ShowLoggerLevel: parentLevel,
		logChan:         parentLogChan,
		stopChan:        parentStopChan,
		doneChan:        parentDoneChan,
		baseDir:         parentBaseDir,
		bufferSize:      parentBufSize,
		flushInterval:   parentFlush,
		maxFileSize:     parentMaxFile,
		maxFileCount:    parentMaxCnt,
		enableColor:     parentColor,
	}
}

func (d *DefaultLogger) GetTitle() string {
	d.locker.RLock()
	defer d.locker.RUnlock()
	return d.title
}

func (d *DefaultLogger) SetTimeFormat(timeFormat string) *DefaultLogger {
	d.locker.Lock()
	defer d.locker.Unlock()
	d.timeFormat = timeFormat
	return d
}

func (d *DefaultLogger) SetWriter(writer io.Writer) *DefaultLogger {
	d.locker.Lock()
	defer d.locker.Unlock()
	d.writer = writer
	return d
}

// SetEnableColor 设置是否启用颜色（实例级别）
func (d *DefaultLogger) SetEnableColor(enable bool) *DefaultLogger {
	d.locker.Lock()
	defer d.locker.Unlock()
	d.enableColor = enable
	return d
}

func (d *DefaultLogger) log(logString string) {
	// stdout 写失败时直接打到 stderr，不要再调 gaia.Error（会再次进 logChan，雪崩）
	if _, err := os.Stdout.Write([]byte(logString)); err != nil {
		fmt.Fprintf(os.Stderr, "[Logger] write stdout failed: %v\n", err)
	}
	if d.writer != nil {
		if _, err := d.writer.Write([]byte(logString)); err != nil {
			fmt.Fprintf(os.Stderr, "[Logger] write writer failed: %v\n", err)
		}
	}

	if !gaia.GetSafeConfBool("Logger.DisableLocalFile") {
		select {
		case d.logChan <- logString:
		default:
			// logChan 满了直接打到 stderr，避免再次进 logChan 形成洪水放大
			fmt.Fprintln(os.Stderr, "[Logger] logChan is full, dropping log")
		}
	}
}

func (d *DefaultLogger) startLogFlusher() {
	go func() {
		// 用 Ticker 周期性触发 flush，避免 Timer 漏 Reset 的问题
		ticker := time.NewTicker(d.flushInterval)
		defer ticker.Stop()
		defer close(d.doneChan)

		logBuffer := make([]string, 0, d.bufferSize)

		flush := func() {
			if len(logBuffer) == 0 {
				return
			}
			d.flushLogs(logBuffer)
			logBuffer = make([]string, 0, d.bufferSize)
		}

		for {
			select {
			case logString := <-d.logChan:
				logBuffer = append(logBuffer, logString)
				if len(logBuffer) >= d.bufferSize {
					flush()
				}
			case <-ticker.C:
				flush()
			case <-d.stopChan:
				// 收到 stop 信号后 drain 剩余日志再退出
				for {
					select {
					case logString := <-d.logChan:
						logBuffer = append(logBuffer, logString)
						if len(logBuffer) >= d.bufferSize {
							flush()
						}
					default:
						flush()
						return
					}
				}
			}
		}
	}()
}

func (d *DefaultLogger) flushLogs(logs []string) {
	if len(logs) == 0 {
		return
	}

	// 使用启动时缓存的根目录，避免运行时被 chdir 影响
	path := d.baseDir + gaia.Sep + DefaultLoggerPath + time.Now().Format("2006-01-02")
	fileName := time.Now().Format("15") + ".log"
	filePath := path + gaia.Sep + fileName

	if err := gaia.MkDirAll(path, os.ModePerm); err != nil {
		fmt.Fprintf(os.Stderr, "[Logger] 创建日志目录失败: %v\n", err)
		return
	}

	// 检查文件大小，需要时进行轮转
	if d.maxFileSize > 0 {
		if info, err := os.Stat(filePath); err == nil && info.Size() >= d.maxFileSize {
			// 进行文件轮转
			rotatedFile := filePath + "." + time.Now().Format("0405")
			if err := os.Rename(filePath, rotatedFile); err != nil {
				gaia.ErrorF("日志文件轮转失败: %v", err)
			}
			// 清理过期日志文件
			d.cleanupOldLogs(path)
		}
	}

	content := strings.Join(logs, "")
	if err := gaia.FileAppendContent(filePath, content); err != nil {
		fmt.Fprintf(os.Stderr, "[Logger] 写入日志文件失败: %v\n", err)
		return
	}
}

// cleanupOldLogs 清理过期的日志文件
func (d *DefaultLogger) cleanupOldLogs(dir string) {
	files, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Logger] 读取日志目录失败: %v\n", err)
		return
	}

	// 只挑出 *.log 或 *.log.* 形式的文件，避免误删 .log.bak 之外的奇怪文件
	logFiles := make([]os.DirEntry, 0)
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		name := file.Name()
		if strings.HasSuffix(name, ".log") || strings.Contains(name, ".log.") {
			logFiles = append(logFiles, file)
		}
	}

	// 清理超过最大文件数的旧文件
	if len(logFiles) > d.maxFileCount {
		// 按修改时间排序，保留最新的
		sort.Slice(logFiles, func(i, j int) bool {
			iInfo, _ := logFiles[i].Info()
			jInfo, _ := logFiles[j].Info()
			return iInfo.ModTime().After(jInfo.ModTime())
		})

		for i := d.maxFileCount; i < len(logFiles); i++ {
			filePath := dir + gaia.Sep + logFiles[i].Name()
			if err := os.Remove(filePath); err != nil {
				fmt.Fprintf(os.Stderr, "[Logger] 删除过期日志文件失败: %v\n", err)
			}
		}
	}
}

// Stop 停止日志服务，确保所有日志都被刷新。
// Stop 不再自己读 logChan，而是通知 flusher goroutine 自行 drain，
// 避免与 flusher 并发读 channel 导致漏写。
func (d *DefaultLogger) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopChan)
		// 等待 flusher goroutine 真正退出，最多等 5 秒
		select {
		case <-d.doneChan:
		case <-time.After(5 * time.Second):
			fmt.Fprintln(os.Stderr, "[Logger] Stop 超时，部分日志可能未刷盘")
		}
	})
}

// collectStack 在 warn+ 级别收集调用栈，用于 4 类日志统一调用
func collectStack(logLevel gaia.LogLevel) string {
	if logLevel >= gaia.LogWarnLevel {
		return gaia.GetStackFramesString(3, 30)
	}
	return ""
}

func (d *DefaultLogger) Log(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogSysType.String(), logLevel.String(), content)
	d.log(logString)

	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogSysType,
		docBody:    nil,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

// InLog 仅写本地（控制台 + 文件）。远程 ES 推送由 InLogBody 负责。
// 调用方一般在不同开关下分别调用 InLog / InLogBody，所以这里不做远程推送，
// 避免双倍写入。OutLog/DbLog 同此约定。
func (d *DefaultLogger) InLog(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogInType.String(), logLevel.String(), content)
	d.log(logString)
}

// InLogBody 仅推送远程 ES，不写本地。本地写入请额外调用 InLog。
func (d *DefaultLogger) InLogBody(logLevel gaia.LogLevel, content string, body AccessLogBaseModel) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogInType,
		docBody:    body,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

// OutLog 仅写本地。远程 ES 推送由 OutLogBody 负责。
func (d *DefaultLogger) OutLog(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogOutType.String(), logLevel.String(), content)
	d.log(logString)
}

// OutLogBody 仅推送远程 ES，不写本地。
func (d *DefaultLogger) OutLogBody(logLevel gaia.LogLevel, content string, body AccessLogBaseModel) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogOutType,
		docBody:    body,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

// DbLog 仅写本地。远程 ES 推送由 DbLogBody 负责。
func (d *DefaultLogger) DbLog(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogDbType.String(), logLevel.String(), content)
	d.log(logString)
}

// DbLogBody 仅推送远程 ES，不写本地。
func (d *DefaultLogger) DbLogBody(logLevel gaia.LogLevel, content string, body DbLogBaseModel) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogDbType,
		docBody:    body,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

// MqLog 仅写本地。远程 ES 推送由 MqLogBody 负责。
func (d *DefaultLogger) MqLog(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogMqType.String(), logLevel.String(), content)
	d.log(logString)
}

// MqLogBody 仅推送远程 ES，不写本地。
func (d *DefaultLogger) MqLogBody(logLevel gaia.LogLevel, content string, body MqLogBaseModel) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogMqType,
		docBody:    body,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

// CacheLog 仅写本地。远程 ES 推送由 CacheLogBody 负责。
func (d *DefaultLogger) CacheLog(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogCacheType.String(), logLevel.String(), content)
	d.log(logString)
}

// CacheLogBody 仅推送远程 ES，不写本地。
func (d *DefaultLogger) CacheLogBody(logLevel gaia.LogLevel, content string, body CacheLogBaseModel) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogCacheType,
		docBody:    body,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

// AsyncTaskLog 仅写本地。远程 ES 推送由 AsyncTaskLogBody 负责。
func (d *DefaultLogger) AsyncTaskLog(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogAsyncTaskType.String(), logLevel.String(), content)
	d.log(logString)
}

// AsyncTaskLogBody 仅推送远程 ES，不写本地。
func (d *DefaultLogger) AsyncTaskLogBody(logLevel gaia.LogLevel, content string, body AsyncTaskLogBaseModel) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogAsyncTaskType,
		docBody:    body,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

// JobLog 仅写本地。远程 ES 推送由 JobLogBody 负责。
func (d *DefaultLogger) JobLog(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	logString := d.GetLogString(d.timeFormat, gaia.LogJobType.String(), logLevel.String(), content)
	d.log(logString)
}

// JobLogBody 仅推送远程 ES，不写本地。
func (d *DefaultLogger) JobLogBody(logLevel gaia.LogLevel, content string, body JobLogBaseModel) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogJobType,
		docBody:    body,
		logLevel:   logLevel,
		content:    content,
		traceStack: collectStack(logLevel),
	})
}

func (d *DefaultLogger) GetLogBody(logTimeFormat, logType, logLevel, content, traceStack string) LogModel {
	traceData := gaia.GetContextTrace()

	logModel := LogModel{
		PId:          strconv.Itoa(os.Getpid()),
		GoId:         gaia.GetGoRoutineId(),
		SystemName:   gaia.GetSystemEnName(),
		LogType:      logType,
		LogTitle:     d.GetTitle(),
		LogLevel:     logLevel,
		Content:      content,
		LogTime:      time.Now().Format(logTimeFormat),
		LogTimeStamp: time.Now().UnixMilli(),
		TraceStack:   traceStack,
	}

	if traceData != nil {
		logModel.LogId = traceData.Id
		logModel.TraceId = traceData.TraceId
	}

	return logModel
}

func (d *DefaultLogger) Debug(content string) {
	d.Log(gaia.LogDebugLevel, content)
}

func (d *DefaultLogger) DebugF(format string, args ...any) {
	d.Debug(fmt.Sprintf(format, args...))
}

func (d *DefaultLogger) Trace(content string) {
	d.Log(gaia.LogTraceLevel, content)
}

func (d *DefaultLogger) TraceF(format string, args ...any) {
	d.Trace(fmt.Sprintf(format, args...))
}

func (d *DefaultLogger) Info(content string) {
	d.Log(gaia.LogInfoLevel, content)
}

func (d *DefaultLogger) InfoF(format string, args ...any) {
	d.Info(fmt.Sprintf(format, args...))
}

func (d *DefaultLogger) Warn(content string) {
	d.Log(gaia.LogWarnLevel, content)
}

func (d *DefaultLogger) WarnF(format string, args ...any) {
	d.Warn(fmt.Sprintf(format, args...))
}

func (d *DefaultLogger) Error(content string) {
	d.Log(gaia.LogErrorLevel, content)
}

func (d *DefaultLogger) ErrorF(format string, args ...any) {
	d.Error(fmt.Sprintf(format, args...))
}

func (d *DefaultLogger) GetLogString(logTimeFormat, logType, logLevel, content string) string {
	if logTimeFormat == "" {
		logTimeFormat = time.DateTime
	}
	builder := strings.Builder{}
	traceData := gaia.GetContextTrace()
	// 使用实例级别的颜色配置
	if d.enableColor {
		switch logLevel {
		case gaia.LogInfoLevel.String():
			logLevel = color.Blue + logLevel + color.Reset
		case gaia.LogWarnLevel.String():
			logLevel = color.Yellow + logLevel + color.Reset
		case gaia.LogErrorLevel.String():
			logLevel = color.Red + logLevel + color.Reset
		}
	}

	builder.WriteString("[" + logLevel + "]" + " ")
	builder.WriteString(time.Now().Format(logTimeFormat) + " ")
	builder.WriteString("[PId:" + strconv.Itoa(os.Getpid()) + "]" + " ")
	builder.WriteString("[GoId:" + gaia.GetGoRoutineId() + "]" + " ")
	if traceData != nil {
		builder.WriteString("[LogId:" + traceData.Id + "]" + " ")
		// 输出全链路 TraceId（与 LogId 区分：LogId 每跳独立，TraceId 全链路统一），
		// 便于日志采集器（Promtail/Alloy）提取后在 Grafana 中与 Tempo trace 关联。
		if traceData.TraceId != "" {
			builder.WriteString("[TraceId:" + traceData.TraceId + "]" + " ")
		}
	}

	builder.WriteString("[" + logType + "]" + " ")
	builder.WriteString("[" + d.GetTitle() + "]" + " ")
	builder.WriteString(content + "\n")
	return builder.String()
}

type createRemoteLogDocArg struct {
	logType    gaia.LogType
	docBody    any
	logLevel   gaia.LogLevel
	content    string
	traceStack string
}

type remoteLogDoc struct {
	logType  string
	logIndex string
	docBody  any
}

func (d *DefaultLogger) CreateRemoteLogDoc(arg createRemoteLogDocArg) {
	if arg.logLevel < gaia.LogInfoLevel {
		return
	}
	g.Go(func() {
		doc := d.buildRemoteLogDoc(arg)
		if err := d.PushLog(doc.logType, doc.logIndex, doc.docBody); err != nil {
			gaia.Println(gaia.LogErrorLevel, err.Error())
		}
	})
}

func (d *DefaultLogger) buildRemoteLogDoc(arg createRemoteLogDocArg) remoteLogDoc {
	logModel := d.GetLogBody(d.timeFormat, arg.logType.String(), arg.logLevel.String(), arg.content, arg.traceStack)
	doc := remoteLogDoc{
		logType:  arg.logType.String(),
		logIndex: SysLogIndex,
		docBody:  logModel,
	}

	switch arg.logType {
	case gaia.LogSysType:
		doc.docBody = offloadLargeRemoteLogFields(doc.logType, doc.docBody)
		return doc
	case gaia.LogInType:
		doc.logIndex = InLogIndex
		if accessLog, ok := arg.docBody.(AccessLogBaseModel); ok {
			doc.docBody = AccessLogModel{
				LogModel:           logModel,
				AccessLogBaseModel: accessLog,
			}
		}
	case gaia.LogOutType:
		doc.logIndex = OutLogIndex
		if accessLog, ok := arg.docBody.(AccessLogBaseModel); ok {
			doc.docBody = AccessLogModel{
				LogModel:           logModel,
				AccessLogBaseModel: accessLog,
			}
		}
	case gaia.LogDbType:
		doc.logIndex = DbLogIndex
		if dbLog, ok := arg.docBody.(DbLogBaseModel); ok {
			doc.docBody = DbLoggerModel{
				LogModel:       logModel,
				DbLogBaseModel: dbLog,
			}
		}
	case gaia.LogMqType:
		doc.logIndex = MqLogIndex
		if mqLog, ok := arg.docBody.(MqLogBaseModel); ok {
			doc.docBody = MqLogModel{
				LogModel:       logModel,
				MqLogBaseModel: mqLog,
			}
		}
	case gaia.LogCacheType:
		doc.logIndex = CacheLogIndex
		if cacheLog, ok := arg.docBody.(CacheLogBaseModel); ok {
			doc.docBody = CacheLogModel{
				LogModel:          logModel,
				CacheLogBaseModel: cacheLog,
			}
		}
	case gaia.LogAsyncTaskType:
		doc.logIndex = AsyncTaskLogIndex
		if taskLog, ok := arg.docBody.(AsyncTaskLogBaseModel); ok {
			doc.docBody = AsyncTaskLogModel{
				LogModel:              logModel,
				AsyncTaskLogBaseModel: taskLog,
			}
		}
	case gaia.LogJobType:
		doc.logIndex = JobLogIndex
		if jobLog, ok := arg.docBody.(JobLogBaseModel); ok {
			doc.docBody = JobLogModel{
				LogModel:        logModel,
				JobLogBaseModel: jobLog,
			}
		}
	}
	doc.docBody = offloadLargeRemoteLogFields(doc.logType, doc.docBody)
	return doc
}

func (d *DefaultLogger) PushLog(logType string, logIndex string, doc any) error {
	// 硬性总闸：用户配置 Logger.DisableRemote=true 时，永久禁用远程日志（优先级最高）
	if gaia.GetSafeConfBool("Logger.DisableRemote") {
		return nil
	}

	// 运行时开关：由 framework 层根据 ES 配置状态 / watcher 动态维护
	if !IsRemoteLogEnabled() {
		return nil
	}

	// 序列化日志文档
	marshal, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("序列化日志文档失败: %w", err)
	}

	// 获取数据缓冲区，使用批量发送
	dataBuffer, err := buffer.GetDataBuffer(logType, func(data [][]byte) error {
		if len(data) == 0 {
			return nil
		}

		// 防御：buffer 的 worker goroutine 持续存在，可能在远程日志被关闭后依旧被调度。
		// 在真正连 ES 之前再检查一次开关状态，避免产生无谓的连接失败日志。
		if !IsRemoteLogEnabled() || gaia.GetSafeConfBool("Logger.DisableRemote") {
			return nil
		}

		// 创建ES客户端
		workEs, err := es.NewFrameWorkEs()
		if err != nil {
			// 直接打到控制台，不能走 gaia.ErrorF ——否则错误日志又会进入 PushLog 形成雪崩。
			// 这里也说明运行时 ES 状态出现异常，watcher 会在下一轮检测到并关闭远程推送。
			gaia.Println(gaia.LogErrorLevel, fmt.Sprintf("创建ES客户端失败: %v", err))
			return fmt.Errorf("创建ES客户端失败: %w", err)
		}

		// 批量发送日志
		errors := make([]error, 0)
		for _, datum := range data {
			m := map[string]any{}
			if err := json.Unmarshal(datum, &m); err != nil {
				errors = append(errors, fmt.Errorf("反序列化日志数据失败: %w", err))
				continue
			}

			_, err = workEs.CreateDoc(logIndex, m)
			if err != nil {
				errors = append(errors, fmt.Errorf("发送日志到ES失败: %w", err))
			}
		}

		// 处理错误
		if len(errors) > 0 {
			for _, err := range errors {
				gaia.Error(err.Error())
			}
			return fmt.Errorf("批量发送日志时出现 %d 个错误", len(errors))
		}

		return nil
	})

	if err != nil {
		gaia.ErrorF("获取数据缓冲区失败: %v", err)
		return fmt.Errorf("获取数据缓冲区失败: %w", err)
	}

	if dataBuffer == nil {
		return fmt.Errorf("初始化数据缓冲区 [%s] 失败", logType)
	}

	// 推送日志数据
	dataBuffer.Push(marshal)
	return nil
}
