// Package server 包注释
// @author wanlizhan
// @created 2025-04-09
package server

import (
	"context"
	"fmt"
	"io"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/xxzhwl/gaia"
)

type ServerLogger struct {
}

func (s *ServerLogger) Trace(v ...interface{}) {
	l := ""
	l += fmt.Sprint(v...)
	gaia.Log(gaia.LogTraceLevel, l)
}

func (s *ServerLogger) Debug(v ...interface{}) {
	l := ""
	l += fmt.Sprint(v...)
	gaia.Log(gaia.LogDebugLevel, l)
}

func (s *ServerLogger) Info(v ...interface{}) {
	l := ""
	l += fmt.Sprint(v...)
	gaia.Log(gaia.LogInfoLevel, l)
}

func (s *ServerLogger) Notice(v ...interface{}) {
	l := ""
	l += fmt.Sprint(v...)
	gaia.Log(gaia.LogWarnLevel, l)
}

func (s *ServerLogger) Warn(v ...interface{}) {
	l := ""
	l += fmt.Sprint(v...)
	gaia.Log(gaia.LogWarnLevel, l)
}

func (s *ServerLogger) Error(v ...interface{}) {
	l := ""
	l += fmt.Sprint(v...)
	gaia.Log(gaia.LogErrorLevel, l)
}

func (s *ServerLogger) Fatal(v ...interface{}) {
	l := ""
	l += fmt.Sprint(v...)
	gaia.Log(gaia.LogErrorLevel, l)
}

func (s *ServerLogger) Tracef(format string, v ...interface{}) {
	gaia.Log(gaia.LogTraceLevel, fmt.Sprintf(format, v...))
}

func (s *ServerLogger) Debugf(format string, v ...interface{}) {
	gaia.DebugF(format, v...)
}

func (s *ServerLogger) Infof(format string, v ...interface{}) {
	gaia.InfoF(format, v...)
}

func (s *ServerLogger) Noticef(format string, v ...interface{}) {
	gaia.WarnF(format, v...)
}

func (s *ServerLogger) Warnf(format string, v ...interface{}) {
	gaia.WarnF(format, v...)
}

func (s *ServerLogger) Errorf(format string, v ...interface{}) {
	gaia.ErrorF(format, v...)
}

func (s *ServerLogger) Fatalf(format string, v ...interface{}) {
	gaia.ErrorF(format, v...)
}

func (s *ServerLogger) CtxTracef(ctx context.Context, format string, v ...interface{}) {
	s.Tracef(format, v...)
}

func (s *ServerLogger) CtxDebugf(ctx context.Context, format string, v ...interface{}) {
	s.Debugf(format, v...)
}

func (s *ServerLogger) CtxInfof(ctx context.Context, format string, v ...interface{}) {
	s.Infof(format, v...)
}

func (s *ServerLogger) CtxNoticef(ctx context.Context, format string, v ...interface{}) {
	s.Noticef(format, v...)
}

func (s *ServerLogger) CtxWarnf(ctx context.Context, format string, v ...interface{}) {
	s.Warnf(format, v...)
}

func (s *ServerLogger) CtxErrorf(ctx context.Context, format string, v ...interface{}) {
	s.Errorf(format, v...)
}

func (s *ServerLogger) CtxFatalf(ctx context.Context, format string, v ...interface{}) {
	s.Fatalf(format, v...)
}

func (s *ServerLogger) SetLevel(level hlog.Level) {
}

func (s *ServerLogger) SetOutput(writer io.Writer) {
}
