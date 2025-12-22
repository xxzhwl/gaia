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

var EnableLogColor = false

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
}

func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{timeFormat: logTimeFormat, title: "Default", ShowLoggerLevel: gaia.LogInfoLevel}
}

func NewLogger(title string) gaia.IBaseLog {
	return &DefaultLogger{title: title, timeFormat: logTimeFormat}
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

func EnableColor() {
	EnableLogColor = true
}

func DisableColor() {
	EnableLogColor = false
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
		g.Go(func() {
			d.logLocalLogFile(logString)
		})
	}
}

func (d *DefaultLogger) logLocalLogFile(logString string) {
	//输出到本地日志文件
	dir, err := os.Getwd()
	if err != nil {
		gaia.Error(err.Error())
		return
	}
	path := dir + gaia.Sep + DefaultLoggerPath + time.Now().Format("2006-01-02")
	fileName := time.Now().Format("15") + ".log"

	if err = gaia.MkDirAll(path, os.ModePerm); err != nil {
		gaia.Error(err.Error())
		return
	}
	if err = gaia.FileAppendContent(path+gaia.Sep+fileName, logString); err != nil {
		gaia.Error(err.Error())
		return
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
	if EnableLogColor {
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
	dataBuffer, err := buffer.GetDataBuffer(logType, func(data [][]byte) error {
		if len(data) == 0 {
			return nil
		}
		workEs, err := es.NewFrameWorkEs()
		if err != nil {
			gaia.Println(gaia.LogErrorLevel, err.Error())
			return nil
		}
		for _, datum := range data {
			m := map[string]any{}
			if err := json.Unmarshal(datum, &m); err != nil {
				gaia.Println(gaia.LogErrorLevel, err.Error())
				continue
			}
			_, err = workEs.CreateDoc(logIndex, m)
			if err != nil {
				gaia.Println(gaia.LogErrorLevel, err.Error())
			}
		}
		return nil
	})
	if err != nil {
		gaia.Println(gaia.LogWarnLevel, err.Error())
		return nil
	}
	if dataBuffer == nil {
		return fmt.Errorf("init data buffer [%s] err", logType)
	}
	marshal, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	dataBuffer.Push(marshal)
	return nil
}
