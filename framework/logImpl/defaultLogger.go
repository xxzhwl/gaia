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
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/color"
	"github.com/xxzhwl/gaia/components/buffer"
	"github.com/xxzhwl/gaia/components/es"
	"github.com/xxzhwl/gaia/g"
)

var logTimeFormat = "2006-01-02 15:04:05.000"

var DefaultLoggerPath = "var" + gaia.Sep + "logs" + gaia.Sep

const (
	ApiLogIndex        = "api_log"
	SysLogIndex        = "sys_log"
	OutRequestLogIndex = "out_request_log"
	DbLogIndex         = "db_log"
)

type LogModel struct {
	LogId        string
	TraceId      string
	PId          string
	GoId         string
	LogType      string
	LogTitle     string
	LogLevel     string
	Content      string
	LogTime      string
	LogTimeStamp int64
	TraceStack   string
}

type ApiLogModel struct {
	LogModel
	HttpLogModel
}

type HttpLogModel struct {
	Url            string
	HttpMethod     string
	ReqHeader      http.Header
	ReqBody        string
	RespHeader     http.Header
	RespBody       string
	StartTime      string
	EndTime        string
	StartTimeStamp int64
	EndTimeStamp   int64
	HttpStatusCode int
	Duration       float64
}

type OutLogModel ApiLogModel

type DefaultLogger struct {
	title      string
	timeFormat string
	writer     io.Writer

	logId string

	locker sync.RWMutex

	ShowLoggerLevel gaia.LogLevel

	logChan    chan string
	flushTimer *time.Timer
	stopChan   chan struct{}

	// 可配置参数
	bufferSize    int
	flushInterval time.Duration
	maxFileSize   int64
	maxFileCount  int
	enableColor   bool // 实例级别的颜色配置
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

	logger := &DefaultLogger{
		timeFormat:      logTimeFormat,
		title:           "Default",
		ShowLoggerLevel: config.ShowLoggerLevel,
		logChan:         make(chan string, config.BufferSize),
		stopChan:        make(chan struct{}),
		bufferSize:      config.BufferSize,
		flushInterval:   config.FlushInterval,
		maxFileSize:     config.MaxFileSize,
		maxFileCount:    config.MaxFileCount,
		enableColor:     config.EnableColor, // 使用实例级别的颜色配置
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
	_, err := os.Stdout.Write([]byte(logString))
	if err != nil {
		gaia.Error(err.Error())
	}
	if d.writer != nil {
		_, err = d.writer.Write([]byte(logString))
		if err != nil {
			gaia.Error(err.Error())
		}
	}

	if !gaia.GetSafeConfBool("Logger.DisableLocalFile") {
		select {
		case d.logChan <- logString:
		default:
			gaia.Error("logChan is full, dropping log")
		}
	}
}

func (d *DefaultLogger) startLogFlusher() {
	d.flushTimer = time.NewTimer(d.flushInterval)

	go func() {
		logBuffer := make([]string, 0, d.bufferSize)

		for {
			select {
			case logString := <-d.logChan:
				logBuffer = append(logBuffer, logString)
				if len(logBuffer) >= d.bufferSize {
					d.flushLogs(logBuffer)
					logBuffer = make([]string, 0, d.bufferSize)
				}
			case <-d.flushTimer.C:
				if len(logBuffer) > 0 {
					d.flushLogs(logBuffer)
					logBuffer = make([]string, 0, d.bufferSize)
				}
				d.flushTimer.Reset(d.flushInterval)
			case <-d.stopChan:
				if len(logBuffer) > 0 {
					d.flushLogs(logBuffer)
				}
				return
			}
		}
	}()
}

func (d *DefaultLogger) flushLogs(logs []string) {
	if len(logs) == 0 {
		return
	}

	dir, err := os.Getwd()
	if err != nil {
		gaia.ErrorF("获取当前目录失败: %v", err)
		return
	}

	path := dir + gaia.Sep + DefaultLoggerPath + time.Now().Format("2006-01-02")
	fileName := time.Now().Format("15") + ".log"
	filePath := path + gaia.Sep + fileName

	if err = gaia.MkDirAll(path, os.ModePerm); err != nil {
		gaia.ErrorF("创建日志目录失败: %v", err)
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
	if err = gaia.FileAppendContent(filePath, content); err != nil {
		gaia.ErrorF("写入日志文件失败: %v", err)
		return
	}
}

// cleanupOldLogs 清理过期的日志文件
func (d *DefaultLogger) cleanupOldLogs(dir string) {
	files, err := os.ReadDir(dir)
	if err != nil {
		gaia.ErrorF("读取日志目录失败: %v", err)
		return
	}

	// 按修改时间排序
	logFiles := make([]os.DirEntry, 0)
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".log") {
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
				gaia.ErrorF("删除过期日志文件失败: %v", err)
			}
		}
	}
}

func (d *DefaultLogger) Stop() {
	// 先停止定时器
	if d.flushTimer != nil {
		d.flushTimer.Stop()
	}

	// 发送停止信号
	close(d.stopChan)

	// 等待日志刷新协程完成
	// 这里需要等待一小段时间让协程处理完剩余的日志
	time.Sleep(100 * time.Millisecond)

	// 确保所有日志都被处理完毕
	// 继续处理channel中剩余的日志
	d.ensureAllLogsFlushed()
}

// ensureAllLogsFlushed 确保所有日志都被刷新到文件
func (d *DefaultLogger) ensureAllLogsFlushed() {
	// 创建一个临时的日志缓冲区
	logBuffer := make([]string, 0, 100)

	// 设置超时，避免无限等待
	timeout := time.After(5 * time.Second)

	for {
		select {
		case logString, ok := <-d.logChan:
			if !ok {
				// channel 已关闭
				if len(logBuffer) > 0 {
					d.flushLogs(logBuffer)
				}
				return
			}

			logBuffer = append(logBuffer, logString)
			if len(logBuffer) >= cap(logBuffer) {
				d.flushLogs(logBuffer)
				logBuffer = make([]string, 0, 100)
			}
		case <-timeout:
			// 超时后强制刷新剩余日志
			if len(logBuffer) > 0 {
				d.flushLogs(logBuffer)
			}
			return
		default:
			// 如果没有更多日志，检查并刷新缓冲区
			if len(logBuffer) > 0 {
				d.flushLogs(logBuffer)
			}
			return
		}
	}
}

func (d *DefaultLogger) Log(logLevel gaia.LogLevel, content string) {
	if logLevel < d.ShowLoggerLevel {
		return
	}
	traceStack := ""
	if logLevel >= gaia.LogWarnLevel {
		traceStack = gaia.GetStackFramesString(2, 30)
	}

	logString := d.GetLogString(d.timeFormat, gaia.LogSysType.String(), logLevel.String(), content)
	d.log(logString)

	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:    gaia.LogSysType,
		docBody:    nil,
		logLevel:   logLevel,
		content:    content,
		traceStack: traceStack,
	})
}

func (d *DefaultLogger) ApiLog(logLevel gaia.LogLevel, content string) {
	logString := d.GetLogString(d.timeFormat, gaia.LogApiType.String(), logLevel.String(), content)
	d.log(logString)
}

func (d *DefaultLogger) ApiLogBody(logLevel gaia.LogLevel, content string, body HttpLogModel) {
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:  gaia.LogApiType,
		docBody:  body,
		logLevel: logLevel,
		content:  content,
	})
}

func (d *DefaultLogger) DbLog(logLevel gaia.LogLevel, content string) {
	logString := d.GetLogString(d.timeFormat, gaia.LogDbType.String(), logLevel.String(), content)
	d.log(logString)
}

func (d *DefaultLogger) DbLogBody(logLevel gaia.LogLevel, content string, body DbLogBaseModel) {
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:  gaia.LogDbType,
		docBody:  body,
		logLevel: logLevel,
		content:  content,
	})
}

func (d *DefaultLogger) OutLog(logLevel gaia.LogLevel, content string) {
	logString := d.GetLogString(d.timeFormat, gaia.LogOutType.String(), logLevel.String(), content)
	d.log(logString)
}

func (d *DefaultLogger) OutLogBody(logLevel gaia.LogLevel, content string, body HttpLogModel) {
	d.CreateRemoteLogDoc(createRemoteLogDocArg{
		logType:  gaia.LogOutType,
		docBody:  body,
		logLevel: logLevel,
		content:  content,
	})
}

func (d *DefaultLogger) GetLogBody(logTimeFormat, logType, logLevel, content, traceStack string) LogModel {
	traceData := gaia.GetContextTrace()

	logModel := LogModel{
		PId:          strconv.Itoa(os.Getpid()),
		GoId:         gaia.GetGoRoutineId(),
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
	d.Debug(fmt.Sprintf(format, args...))
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

func (d *DefaultLogger) CreateRemoteLogDoc(arg createRemoteLogDocArg) {
	if arg.logLevel < gaia.LogInfoLevel {
		return
	}
	g.Go(func() {
		//考虑到如果
		var logBodyDetail any
		var logModel LogModel
		var logIndex = SysLogIndex
		switch arg.logType {
		case gaia.LogSysType:
			logBodyDetail = d.GetLogBody(d.timeFormat, gaia.LogSysType.String(), arg.logLevel.String(), arg.content, arg.traceStack)
		case gaia.LogApiType:
			logModel = d.GetLogBody(d.timeFormat, gaia.LogApiType.String(), arg.logLevel.String(), arg.content, arg.traceStack)
			logBodyDetail = ApiLogModel{
				LogModel:     logModel,
				HttpLogModel: arg.docBody.(HttpLogModel),
			}
			logIndex = ApiLogIndex
		case gaia.LogDbType:
			logModel = d.GetLogBody(d.timeFormat, gaia.LogDbType.String(), arg.logLevel.String(), arg.content, arg.traceStack)
			logBodyDetail = DbLoggerModel{
				LogModel:       logModel,
				DbLogBaseModel: arg.docBody.(DbLogBaseModel),
			}
			logIndex = DbLogIndex
		case gaia.LogOutType:
			logModel = d.GetLogBody(d.timeFormat, gaia.LogOutType.String(), arg.logLevel.String(), arg.content, arg.traceStack)
			logBodyDetail = ApiLogModel{
				LogModel:     logModel,
				HttpLogModel: arg.docBody.(HttpLogModel),
			}
			logIndex = OutRequestLogIndex
		}
		if err := d.PushLog(arg.logType.String(), logIndex, logBodyDetail); err != nil {
			gaia.Println(gaia.LogErrorLevel, err.Error())
		}
	})
}

func (d *DefaultLogger) PushLog(logType string, logIndex string, doc any) error {
	// 检查是否禁用远程日志
	if gaia.GetSafeConfBool("Logger.DisableRemote") {
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

		// 创建ES客户端
		workEs, err := es.NewFrameWorkEs()
		if err != nil {
			gaia.ErrorF("创建ES客户端失败: %v", err)
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
