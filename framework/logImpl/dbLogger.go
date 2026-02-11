// Package logImpl 注释
// @author wanlizhan
// @created 2024/5/9
package logImpl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"

	"github.com/xxzhwl/gaia"
)

var (
	traceWarnStr = "%s %s\n[%s] [%s] [%.3fms] [rows:%v] %s"
	traceErrStr  = "%s %s\n[%s] [%s] [%.3fms] [rows:%v] %s"
)

type DbLoggerModel struct {
	LogModel
	DbLogBaseModel
}

type DbLogBaseModel struct {
	Dsn            string
	SqlType        string
	Sql            string
	SqlPosition    string
	MainTable      string
	StartTime      string
	EndTime        string
	Duration       float64
	StartTimeStamp int64
	EndTimeStamp   int64
}

type LocalDbLogger struct {
	DefaultLogger
	Config logger.Config
}

func NewDbLogger(conf logger.Config) *LocalDbLogger {
	return &LocalDbLogger{DefaultLogger: *NewDefaultLogger(), Config: conf}
}

func (l *LocalDbLogger) LogMode(level logger.LogLevel) logger.Interface {
	// 创建一个新的 logger 实例，避免修改原始实例
	newLogger := &LocalDbLogger{
		DefaultLogger: l.DefaultLogger,
		Config:        l.Config,
	}
	newLogger.Config.LogLevel = level
	return newLogger
}

func (l *LocalDbLogger) Info(ctx context.Context, s string, i ...interface{}) {
	l.InfoF(s, i...)
}

func (l *LocalDbLogger) Warn(ctx context.Context, s string, i ...interface{}) {
	l.WarnF(s, i...)
}

func (l *LocalDbLogger) Error(ctx context.Context, s string, i ...interface{}) {
	l.ErrorF(s, i...)
}

func (l *LocalDbLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	// 根据 GORM 日志级别判断是否记录日志
	// Silent: 不记录任何日志
	// Error: 只记录错误日志
	// Warn: 记录错误和慢查询日志
	// Info: 记录所有日志
	if l.Config.LogLevel == logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	split := strings.Split(sql, " ")
	sqlType := ""

	if len(split) > 0 {
		sqlType = split[0]
	}
	mainTable := strings.ReplaceAll(fetchMainTable(sql, sqlType), " ", "")
	l.DefaultLogger.SetTitle(mainTable)
	logContent := ""
	logLevel := gaia.LogInfoLevel
	needLog := false

	if err != nil && (!errors.Is(err, gorm.ErrRecordNotFound)) {
		// 错误日志
		if l.Config.LogLevel >= logger.Error {
			needLog = true
			logLevel = gaia.LogErrorLevel
			if rows == -1 {
				logContent = fmt.Sprintf(traceErrStr, utils.FileWithLineNum(), err, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, "-", sql)
			} else {
				logContent = fmt.Sprintf(traceErrStr, utils.FileWithLineNum(), err, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, rows, sql)
			}
		}
	} else if elapsed > l.Config.SlowThreshold && l.Config.SlowThreshold != 0 {
		// 慢查询日志
		if l.Config.LogLevel >= logger.Warn {
			needLog = true
			logLevel = gaia.LogWarnLevel
			slowLog := fmt.Sprintf("SLOW SQL >= %v", l.Config.SlowThreshold)
			if rows == -1 {
				logContent = fmt.Sprintf(traceWarnStr, utils.FileWithLineNum(), slowLog, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, "-", sql)
			} else {
				logContent = fmt.Sprintf(traceWarnStr, utils.FileWithLineNum(), slowLog, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, rows, sql)
			}
		}
	} else {
		// 正常SQL日志
		if l.Config.LogLevel >= logger.Info {
			needLog = true
			logLevel = gaia.LogInfoLevel
			logContent = fmt.Sprintf(traceWarnStr, utils.FileWithLineNum(), "", sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, rows, sql)
		}
	}

	if needLog {
		l.DefaultLogger.DbLog(logLevel, logContent)
		l.DefaultLogger.DbLogBody(logLevel, logContent, DbLogBaseModel{
			SqlType:        sqlType,
			Sql:            sql,
			Dsn:            "",
			SqlPosition:    utils.FileWithLineNum(),
			MainTable:      mainTable,
			StartTime:      begin.Format(l.DefaultLogger.timeFormat),
			EndTime:        time.Now().Format(l.DefaultLogger.timeFormat),
			Duration:       float64(elapsed.Nanoseconds()) / 1e6,
			StartTimeStamp: begin.UnixMilli(),
			EndTimeStamp:   time.Now().UnixMilli(),
		})
	}
}

func fetchMainTable(sql, sqlType string) string {
	mainTable := ""
	switch strings.ToLower(sqlType) {
	case "select", "delete":
		index := strings.Index(sql, "FROM")
		if index == -1 {
			return ""
		}
		afterFromSql := sql[index+4:]
		i := strings.Index(afterFromSql, "`")
		if i == -1 {
			return ""
		}
		afterFirstSql := afterFromSql[i+1:]
		j := strings.Index(afterFirstSql, "`")
		if j == -1 {
			return ""
		}
		mainTable = afterFirstSql[:j]
	case "update":
		fromIndex := strings.Index(sql, "UPDATE")
		setIndex := strings.Index(sql, "SET")
		if fromIndex == -1 || setIndex == -1 {
			return ""
		}
		mainTable = strings.ReplaceAll(sql[fromIndex+6:setIndex], "`", "")
	case "insert":
		index := strings.Index(sql, "INTO")
		if index == -1 {
			return ""
		}
		afterFromSql := sql[index+4:]
		i := strings.Index(afterFromSql, "`")
		if i == -1 {
			return ""
		}
		afterFirstSql := afterFromSql[i+1:]
		j := strings.Index(afterFirstSql, "`")
		if j == -1 {
			return ""
		}
		mainTable = afterFirstSql[:j]
	default:
	}
	return mainTable
}
