// Package logImpl 注释
// @author wanlizhan
// @created 2024/5/9
// @refactored 2026-06-05 — 废弃 Gorm.LogLevel / Logger.DbLocalLevel / Logger.DbRemoteLevel；
//
//	统一到 Gorm.LocalLevel / Gorm.RemoteLevel / Gorm.SlowThreshold
package logImpl

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"

	"github.com/xxzhwl/gaia"
)

// silentLevel 内部哨兵：高于所有有效级别，等效"完全静默"。
// 不直接复用 logger.Silent，因为这里使用的是 gaia.LogLevel 维度。
const silentLevel gaia.LogLevel = 255

// parseLogLevel 将配置字符串解析为 gaia.LogLevel。
// 支持: debug, trace, info, warn, error, silent(禁用)。不识别时返回 defaultLevel。
func parseLogLevel(s string, defaultLevel gaia.LogLevel) gaia.LogLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return gaia.LogDebugLevel
	case "trace":
		return gaia.LogTraceLevel
	case "info":
		return gaia.LogInfoLevel
	case "warn":
		return gaia.LogWarnLevel
	case "error":
		return gaia.LogErrorLevel
	case "silent", "disable", "off", "false":
		return silentLevel
	default:
		return defaultLevel
	}
}

// gaiaToGormLevel 把 gaia.LogLevel 映射到 gorm logger.LogLevel
//
// 映射规则：
//
//	silentLevel(255)    → logger.Silent
//	LogErrorLevel       → logger.Error
//	LogWarnLevel        → logger.Warn
//	其它（info / trace / debug） → logger.Info
func gaiaToGormLevel(g gaia.LogLevel) logger.LogLevel {
	switch {
	case g >= silentLevel:
		return logger.Silent
	case g >= gaia.LogErrorLevel:
		return logger.Error
	case g >= gaia.LogWarnLevel:
		return logger.Warn
	default:
		return logger.Info
	}
}

// looserLevel 取两个级别中"更宽松"的那个（更低的阈值放过更多日志）
//
// 用于推导 GORM 那一层应当放行的最低级别：本地与远程任一想看就放过，
// 两边都不要时（都为 silent）才让 GORM 直接 short-circuit。
func looserLevel(a, b gaia.LogLevel) gaia.LogLevel {
	if a < b {
		return a
	}
	return b
}

const traceLogFmt = "%s %s\n[%s] [%s] [%.3fms] [rows:%v] [traceId:%s] [dsn:%s] %s"

type DbLoggerModel struct {
	LogModel
	DbLogBaseModel
}

type DbLogBaseModel struct {
	Dsn            string  `json:"dsn,omitempty"`
	SqlType        string  `json:"sql_type"`
	Sql            string  `json:"sql"`
	SqlPosition    string  `json:"sql_position"`
	MainTable      string  `json:"main_table"`
	StartTime      string  `json:"start_time"`
	EndTime        string  `json:"end_time"`
	Duration       float64 `json:"duration"`
	StartTimeStamp int64   `json:"start_time_stamp"`
	EndTimeStamp   int64   `json:"end_time_stamp"`
}

type LocalDbLogger struct {
	*DefaultLogger
	Config logger.Config
	Dsn    string // 可选：设置后会随 Trace 一同写入 ES，用于区分多库

	localLevel  gaia.LogLevel // 本地控制台/文件输出的最低日志级别
	remoteLevel gaia.LogLevel // 远程 ES 推送的最低日志级别
}

// DbLoggerOptions 低层构造选项
type DbLoggerOptions struct {
	// LocalLevel 本地输出最低级别；零值时默认 LogWarnLevel
	LocalLevel gaia.LogLevel
	// RemoteLevel 远程推送最低级别；零值时默认 LogWarnLevel
	RemoteLevel gaia.LogLevel
}

// NewDbLogger 低层构造：不读 gaia 配置；适合手动注入 / 测试场景
//
// 注意：本函数不会根据 LocalLevel / RemoteLevel 推导 conf.LogLevel；
// 调用方需自行保证 conf.LogLevel 至少能放过 LocalLevel / RemoteLevel 想要的最低级别，
// 否则会被 GORM 这一层提前过滤。
// 框架启动路径请使用 NewFrameworkDbLogger。
func NewDbLogger(conf logger.Config, opts ...DbLoggerOptions) *LocalDbLogger {
	opt := DbLoggerOptions{LocalLevel: gaia.LogWarnLevel, RemoteLevel: gaia.LogWarnLevel}
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.LocalLevel == gaia.LogDefaultLevel {
		opt.LocalLevel = gaia.LogWarnLevel
	}
	if opt.RemoteLevel == gaia.LogDefaultLevel {
		opt.RemoteLevel = gaia.LogWarnLevel
	}
	return &LocalDbLogger{
		DefaultLogger: NewDefaultLogger(),
		Config:        conf,
		localLevel:    opt.LocalLevel,
		remoteLevel:   opt.RemoteLevel,
	}
}

// NewFrameworkDbLogger 框架启动路径使用：从 gaia 配置中读取 GORM 日志相关全部字段
//
// 读取的配置项（统一在 Gorm.* 命名空间下）：
//   - Gorm.LocalLevel    string  本地输出最低级别（silent / error / warn / info / trace / debug）；默认 warn
//   - Gorm.RemoteLevel   string  远程 ES 推送最低级别；默认 warn
//   - Gorm.SlowThreshold int64   慢 SQL 阈值（毫秒）；默认 200
//
// 设计要点：
//   - GORM 自身的 logger.Config.LogLevel 自动推导为 looserOf(LocalLevel, RemoteLevel)
//     —— 取两者中"更宽松"（数值更低）的级别。
//   - 这样既能保证本地 / 远程拿到各自想要的条目；
//     又能在两侧都为 silent 时让 GORM 直接 short-circuit，节省 trace 计算。
func NewFrameworkDbLogger() *LocalDbLogger {
	localLevel := parseLogLevel(gaia.GetSafeConfString("Gorm.LocalLevel"), gaia.LogWarnLevel)
	remoteLevel := parseLogLevel(gaia.GetSafeConfString("Gorm.RemoteLevel"), gaia.LogWarnLevel)
	slowMs := gaia.GetSafeConfInt64WithDefault("Gorm.SlowThreshold", 200)

	gormLevel := gaiaToGormLevel(looserLevel(localLevel, remoteLevel))
	return &LocalDbLogger{
		DefaultLogger: NewDefaultLogger(),
		Config: logger.Config{
			SlowThreshold:             time.Duration(slowMs) * time.Millisecond,
			LogLevel:                  gormLevel,
			IgnoreRecordNotFoundError: false,
			Colorful:                  true,
		},
		localLevel:  localLevel,
		remoteLevel: remoteLevel,
	}
}

// LocalLevel 返回当前生效的本地最低级别
func (l *LocalDbLogger) LocalLevel() gaia.LogLevel { return l.localLevel }

// RemoteLevel 返回当前生效的远程最低级别
func (l *LocalDbLogger) RemoteLevel() gaia.LogLevel { return l.remoteLevel }

// LevelText 把内部 level 用对外友好的字符串表示出来；silentLevel 显示为 SILENT
func (l *LocalDbLogger) LevelText(g gaia.LogLevel) string {
	if g >= silentLevel {
		return "SILENT"
	}
	return g.String()
}

// WithDsn 设置当前 Logger 所属的 DSN（会自动脱敏密码字段）。
//
// 在多 DB 连接场景下，为每个 DB 独立克隆一个 logger 后设置 DSN。框架在 genConn /
// genPgConn 内部已经会自动调用 CloneWithDsn 完成这件事，业务代码一般不需要手动调用。
func (l *LocalDbLogger) WithDsn(dsn string) *LocalDbLogger {
	l.Dsn = sanitizeDsn(dsn)
	return l
}

// CloneWithDsn 克隆一份 Logger 并独占设置 DSN（脱敏后）
//
// 用于框架在创建每个 DB 连接时为该连接绑定独立的日志标签：
//   - DefaultLogger / Config / level 字段都共享父 logger（轻量）
//   - 仅 Dsn 字段独占，避免多 DB 之间互相覆盖
//
// 返回 logger.Interface 而非 *LocalDbLogger，便于在 gaia 包通过接口断言调用，
// 不必引入 framework/logImpl 形成循环依赖。
func (l *LocalDbLogger) CloneWithDsn(dsn string) logger.Interface {
	return &LocalDbLogger{
		DefaultLogger: l.DefaultLogger,
		Config:        l.Config,
		Dsn:           sanitizeDsn(dsn),
		localLevel:    l.localLevel,
		remoteLevel:   l.remoteLevel,
	}
}

// LogMode 返回一个新的 logger 实例，只拷贝 Config 避免并发修改 SlowThreshold/LogLevel 影响调用方。
// DefaultLogger 是帮着看的底层输出通道，多个实例间可以安全共享。
func (l *LocalDbLogger) LogMode(level logger.LogLevel) logger.Interface {
	newLogger := &LocalDbLogger{
		DefaultLogger: l.DefaultLogger,
		Config:        l.Config,
		Dsn:           l.Dsn,
		localLevel:    l.localLevel,
		remoteLevel:   l.remoteLevel,
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
	// GORM 这一层若被 silent 整体关闭则直接 return；
	// LogLevel 由 NewFrameworkDbLogger 推导自 looserOf(LocalLevel, RemoteLevel)，
	// 因此走到这里时一定有至少一侧（本地或远程）想要看日志。
	if l.Config.LogLevel == logger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()
	sqlType := ""
	trimmed := strings.TrimSpace(sql)
	if idx := strings.IndexAny(trimmed, " \t\n"); idx > 0 {
		sqlType = trimmed[:idx]
	} else {
		sqlType = trimmed
	}
	mainTable := strings.ReplaceAll(fetchMainTable(sql, sqlType), " ", "")

	// 打拿 traceId，方便跨服务串联查询
	traceId := ""
	if tr := gaia.GetContextTrace(); tr != nil {
		traceId = tr.TraceId
	}

	logContent := ""
	logLevel := gaia.LogInfoLevel
	needLog := false

	if err != nil && (!errors.Is(err, gorm.ErrRecordNotFound)) {
		// 错误日志
		if l.Config.LogLevel >= logger.Error {
			needLog = true
			logLevel = gaia.LogErrorLevel
			if rows == -1 {
				logContent = fmt.Sprintf(traceLogFmt, utils.FileWithLineNum(), err, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, "-", traceId, l.Dsn, sql)
			} else {
				logContent = fmt.Sprintf(traceLogFmt, utils.FileWithLineNum(), err, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, rows, traceId, l.Dsn, sql)
			}
		}
	} else if elapsed > l.Config.SlowThreshold && l.Config.SlowThreshold != 0 {
		// 慢查询日志
		if l.Config.LogLevel >= logger.Warn {
			needLog = true
			logLevel = gaia.LogWarnLevel
			slowLog := fmt.Sprintf("SLOW SQL >= %v", l.Config.SlowThreshold)
			if rows == -1 {
				logContent = fmt.Sprintf(traceLogFmt, utils.FileWithLineNum(), slowLog, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, "-", traceId, l.Dsn, sql)
			} else {
				logContent = fmt.Sprintf(traceLogFmt, utils.FileWithLineNum(), slowLog, sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, rows, traceId, l.Dsn, sql)
			}
		}
	} else {
		// 正常SQL日志
		if l.Config.LogLevel >= logger.Info {
			needLog = true
			logLevel = gaia.LogInfoLevel
			logContent = fmt.Sprintf(traceLogFmt, utils.FileWithLineNum(), "", sqlType, mainTable, float64(elapsed.Nanoseconds())/1e6, rows, traceId, l.Dsn, sql)
		}
	}

	if needLog {
		if logLevel >= l.localLevel {
			l.DefaultLogger.DbLog(logLevel, logContent)
		}
		if logLevel >= l.remoteLevel {
			l.DefaultLogger.DbLogBody(logLevel, logContent, DbLogBaseModel{
				SqlType:        sqlType,
				Sql:            sql,
				Dsn:            l.Dsn,
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
}

func fetchMainTable(sql, sqlType string) string {
	mainTable := ""
	// 指令不区分大小写定位，但 mainTable 仍从原始 sql 提取以保留大小写
	upperSql := strings.ToUpper(sql)
	switch strings.ToLower(sqlType) {
	case "select", "delete":
		index := strings.Index(upperSql, "FROM")
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
		fromIndex := strings.Index(upperSql, "UPDATE")
		setIndex := strings.Index(upperSql, "SET")
		if fromIndex == -1 || setIndex == -1 {
			return ""
		}
		mainTable = strings.ReplaceAll(sql[fromIndex+6:setIndex], "`", "")
	case "insert":
		index := strings.Index(upperSql, "INTO")
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

// ================================ DSN 脱敏 ================================

var (
	// urlFormPwdRe 匹配 URL 形式 DSN 的密码段：scheme://user:PASSWORD@host...
	urlFormPwdRe = regexp.MustCompile(`(://[^:/@\s]+:)([^@\s]*)(@)`)
	// plainFormPwdRe 匹配 user:PASSWORD@host... 形式（无 scheme 前缀，典型 mysql DSN）
	plainFormPwdRe = regexp.MustCompile(`(^[^:/@\s]+:)([^@\s]*)(@)`)
	// pgKVPwdRe 匹配 postgres key=value 形式中的 password=VALUE
	pgKVPwdRe = regexp.MustCompile(`(?i)(password=)([^\s]+)`)
)

// sanitizeDsn 把 DSN 中的密码字段替换为 *** ，保留其它部分用作日志标签
//
// 支持的常见格式：
//   - MySQL DSN：       user:pwd@tcp(host:port)/db   →  user:***@tcp(host:port)/db
//   - URL 形式 DSN：    postgres://u:p@h:5432/db     →  postgres://u:***@h:5432/db
//   - postgres 键值对： host=h user=u password=pwd   →  host=h user=u password=***
func sanitizeDsn(dsn string) string {
	if dsn == "" {
		return ""
	}
	s := dsn
	if urlFormPwdRe.MatchString(s) {
		s = urlFormPwdRe.ReplaceAllString(s, "${1}***${3}")
	} else {
		s = plainFormPwdRe.ReplaceAllString(s, "${1}***${3}")
	}
	s = pgKVPwdRe.ReplaceAllString(s, "${1}***")
	return s
}
