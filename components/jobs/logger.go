// Package jobs 包注释
// @author wanlizhan
// @created 2024/7/2
package jobs

import (
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
)

type CronLogger struct {
	logger *logImpl.DefaultLogger
}

func (c CronLogger) Info(msg string, keysAndValues ...interface{}) {
	arg := gaia.CvtArg(keysAndValues...)
	c.logger.InfoF(msg, arg...)
}

func (c CronLogger) Error(err error, msg string, keysAndValues ...interface{}) {
	arg := gaia.CvtArg(keysAndValues...)
	if err != nil {
		c.logger.ErrorF(err.Error()+msg, arg...)
	} else {
		c.logger.ErrorF(msg, arg...)
	}
}
