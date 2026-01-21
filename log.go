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

func Println(logLevel LogLevel, content string) {
	fmt.Println(GetLogString(logTimeFormat, LogSysType.String(), logLevel.String(), content))
	if logLevel == LogErrorLevel {
		fmt.Println(GetStackFramesString(2, 30))
	}
}

func Debug(content string) {
	Log(LogDebugLevel, content)
}

func DebugF(format string, i ...any) {
	Debug(fmt.Sprintf(format, i...))
}

func Trace(content string) {
	Log(LogTraceLevel, content)
}

func TraceF(format string, i ...any) {
	Trace(fmt.Sprintf(format, i...))
}

func Info(content string) {
	Log(LogInfoLevel, content)
}

func InfoF(format string, i ...any) {
	Info(fmt.Sprintf(format, i...))
}

func Warn(content string) {
	Log(LogWarnLevel, content)
}

func WarnF(format string, i ...any) {
	Warn(fmt.Sprintf(format, i...))
}

func Error(content string) {
	Log(LogErrorLevel, content)
}

func ErrorF(format string, i ...any) {
	Error(fmt.Sprintf(format, i...))
}

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

type TempLog struct {
}

func (t TempLog) SetShowLoggerLevel(level LogLevel) {
}

func (t TempLog) Log(logLevel LogLevel, content string) {
	Log(logLevel, content)
}

func (t TempLog) Trace(content string) {
	Trace(content)
}

func (t TempLog) TraceF(format string, args ...any) {
	TraceF(format, args...)
}

func (t TempLog) Debug(content string) {
	Debug(content)
}

func (t TempLog) DebugF(format string, args ...any) {
	DebugF(format, args...)
}

func (t TempLog) Info(content string) {
	Info(content)
}

func (t TempLog) InfoF(format string, args ...any) {
	InfoF(format, args...)
}

func (t TempLog) Warn(content string) {
	Warn(content)
}

func (t TempLog) WarnF(format string, args ...any) {
	WarnF(format, args...)
}

func (t TempLog) Error(content string) {
	Error(content)
}

func (t TempLog) ErrorF(format string, args ...any) {
	ErrorF(format, args...)
}

func (t TempLog) Stop() {
	// TempLog 不需要停止操作
}
