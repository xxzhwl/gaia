// Package gaia 注释
// @author wanlizhan
// @created 2024/4/27
package gaia

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xxzhwl/gaia/color"
)

type LogLevel uint8

const (
	LogDefaultLevel LogLevel = iota
	LogDebugLevel
	LogTraceLevel
	LogInfoLevel
	LogWarnLevel
	LogErrorLevel
)

var ShowLogLevel = LogInfoLevel

func (l LogLevel) String() string {
	switch l {
	case LogDebugLevel:
		return "DEBUG"
	case LogTraceLevel:
		return "TRACE"
	case LogInfoLevel:
		return "INFO"
	case LogWarnLevel:
		return "WARN"
	case LogErrorLevel:
		return "ERROR"
	case LogDefaultLevel:
		return "DEFAULT"
	default:
		return "DEFAULT"
	}
}

type LogType uint8

const (
	LogSysType LogType = iota
	LogApiType
	LogOutType
	LogDbType
)

func (l LogType) String() string {
	switch l {
	case LogSysType:
		return "SysLog"
	case LogApiType:
		return "ApiLog"
	case LogOutType:
		return "OutLog"
	case LogDbType:
		return "DbLog"
	default:
		return "SysLog"
	}
}

type IBaseLog interface {
	Log(logLevel LogLevel, content string)

	Trace(content string)
	TraceF(format string, args ...any)

	Debug(content string)
	DebugF(format string, args ...any)

	Info(content string)
	InfoF(format string, args ...any)

	Warn(content string)
	WarnF(format string, args ...any)

	Error(content string)
	ErrorF(format string, args ...any)

	SetShowLoggerLevel(level LogLevel)

	// Stop 停止日志服务，确保所有日志都被刷新
	Stop()
}

var LocalLogger IBaseLog

var logTimeFormat = "2006-01-02 15:04:05.000"

// SetLogTimeFormat 设置Log时间格式
func SetLogTimeFormat(format string) {
	logTimeFormat = format
}

// GetLogger 获取当前日志器实例
func GetLogger() IBaseLog {
	return LocalLogger
}

var NewLogger func(title string) IBaseLog

func init() {
	if NewLogger == nil {
		NewLogger = func(title string) IBaseLog {
			return &TempLog{}
		}
	}
}

// Log 输出日志，根据日志级别控制输出
func Log(logLevel LogLevel, content string) {
	if LocalLogger != nil {
		LocalLogger.Log(logLevel, content)
		return
	}
	//根据logLevel输出
	if logLevel < ShowLogLevel {
		return
	}
	Println(logLevel, content)
}

// Println 打印日志行，包含完整的日志格式
func Println(logLevel LogLevel, content string) {
	fmt.Println(GetLogString(logTimeFormat, LogSysType.String(), logLevel.String(), content))
	if logLevel == LogErrorLevel {
		fmt.Println(GetStackFramesString(2, 30))
	}
}

// Debug 输出调试级别日志
func Debug(content string) {
	Log(LogDebugLevel, content)
}

// DebugF 格式化输出调试级别日志
func DebugF(format string, i ...any) {
	Debug(fmt.Sprintf(format, i...))
}

// Trace 输出跟踪级别日志
func Trace(content string) {
	Log(LogTraceLevel, content)
}

// TraceF 格式化输出跟踪级别日志
func TraceF(format string, i ...any) {
	Trace(fmt.Sprintf(format, i...))
}

// Info 输出信息级别日志
func Info(content string) {
	Log(LogInfoLevel, content)
}

// InfoF 格式化输出信息级别日志
func InfoF(format string, i ...any) {
	Info(fmt.Sprintf(format, i...))
}

// Warn 输出警告级别日志
func Warn(content string) {
	Log(LogWarnLevel, content)
}

// WarnF 格式化输出警告级别日志
func WarnF(format string, i ...any) {
	Warn(fmt.Sprintf(format, i...))
}

// Error 输出错误级别日志
func Error(content string) {
	Log(LogErrorLevel, content)
}

// ErrorF 格式化输出错误级别日志
func ErrorF(format string, i ...any) {
	Error(fmt.Sprintf(format, i...))
}

// GetLogString 生成格式化的日志字符串，包含时间、级别、类型等信息
func GetLogString(logTimeFormat, logType, logLevel, content string) string {
	trace := NewContextTrace()
	logId := trace.GetId()
	builder := strings.Builder{}

	levelS := logLevel
	switch logLevel {
	case LogInfoLevel.String():
		levelS = color.Blue + levelS + color.Reset
	case LogWarnLevel.String():
		levelS = color.Yellow + levelS + color.Reset
	case LogErrorLevel.String():
		levelS = color.Red + levelS + color.Reset
	case LogTraceLevel.String():
		levelS = color.Green + levelS + color.Reset
	}

	builder.WriteString(fmt.Sprintf("%s %s %s %s %s %s %s %s",
		"["+levelS+"]",
		time.Now().Format(logTimeFormat),
		"[Gaia]",
		"[PId:"+strconv.Itoa(os.Getpid())+"]",
		"[GoId:"+GetGoRoutineId()+"]",
		"[LogId:"+logId+"]",
		"["+logType+"]",
		content))

	return builder.String()
}

// CvtArg 转换参数，将time.Time类型转换为格式化字符串
func CvtArg(args ...any) []any {
	argsTemp := []any{}
	for _, arg := range args {
		if t, ok := arg.(time.Time); ok {
			arg = t.Format(logTimeFormat)
		}
		argsTemp = append(argsTemp, arg)
	}
	return argsTemp
}

// TempLog 临时日志实现，用于日志接口的默认实现
type TempLog struct {
}

// SetShowLoggerLevel 设置日志显示级别（临时日志实现，无实际效果）
func (t TempLog) SetShowLoggerLevel(level LogLevel) {
}

// Log 输出日志，调用全局Log函数
func (t TempLog) Log(logLevel LogLevel, content string) {
	Log(logLevel, content)
}

// Trace 输出跟踪级别日志，调用全局Trace函数
func (t TempLog) Trace(content string) {
	Trace(content)
}

// TraceF 格式化输出跟踪级别日志，调用全局TraceF函数
func (t TempLog) TraceF(format string, args ...any) {
	TraceF(format, args...)
}

// Debug 输出调试级别日志，调用全局Debug函数
func (t TempLog) Debug(content string) {
	Debug(content)
}

// DebugF 格式化输出调试级别日志，调用全局DebugF函数
func (t TempLog) DebugF(format string, args ...any) {
	DebugF(format, args...)
}

// Info 输出信息级别日志，调用全局Info函数
func (t TempLog) Info(content string) {
	Info(content)
}

// InfoF 格式化输出信息级别日志，调用全局InfoF函数
func (t TempLog) InfoF(format string, args ...any) {
	InfoF(format, args...)
}

// Warn 输出警告级别日志，调用全局Warn函数
func (t TempLog) Warn(content string) {
	Warn(content)
}

// WarnF 格式化输出警告级别日志，调用全局WarnF函数
func (t TempLog) WarnF(format string, args ...any) {
	WarnF(format, args...)
}

// Error 输出错误级别日志，调用全局Error函数
func (t TempLog) Error(content string) {
	Error(content)
}

func (t TempLog) ErrorF(format string, args ...any) {
	ErrorF(format, args...)
}

func (t TempLog) Stop() {
	// TempLog 不需要停止操作
}
