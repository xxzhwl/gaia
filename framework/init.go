// Package framework 注释
// @author wanlizhan
// @created 2024/5/6
package framework

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"gorm.io/gorm/logger"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/messageImpl"
	"github.com/xxzhwl/gaia/framework/remoteConfig"
	"github.com/xxzhwl/gaia/framework/system"
	"github.com/xxzhwl/gaia/framework/tracer"
)

func Init() {
	//框架系统日志注入
	localLogger := logImpl.NewDefaultLogger().SetTitle("GaiaFramework")
	if gaia.IsEnvDev() {
		localLogger.SetEnableColor(true)
	}

	gaia.LocalLogger = localLogger
	gaia.NewLogger = logImpl.NewLogger
	gaia.InfoF("注入LocalLogger:[showLoggerLevel:%s]", localLogger.ShowLoggerLevel)
	gaia.Info("注入NewLogger方法")
	//框架系统名称注入
	gaia.SystemNameImplObj = system.GaiaSystem{}
	gaia.InfoF("当前环境为[%s]", gaia.GetEnvFlag())

	//框架远程配置中心注入
	remoteConfig.RegisterConsulRemoteConf()

	ctx := context.Background()
	_, err := tracer.SetupTracer(ctx, gaia.GetSystemEnName())
	if err != nil {
		panic(err)
	}

	tracer.LocalTrace = otel.Tracer("gaiaTracer")

	//HTTP请求前置处理器注入
	httpclient.SetRequestBeforeHandler(httpclient.DefaultHandler)

	//框架消息提醒注入
	gaia.Message = messageImpl.NewFeiShuRobot()
	//框架DB层日志注入
	// 根据配置设置日志级别
	logLevel := logger.Silent
	logLevelStr := gaia.GetSafeConfString("Gorm.LogLevel")
	gaia.InfoF("Gorm日志级别:%s", logLevelStr)

	switch logLevelStr {
	case "INFO":
		logLevel = logger.Info
	case "WARN":
		logLevel = logger.Warn
	case "ERROR":
		logLevel = logger.Error
	default:
		logLevel = logger.Silent
	}
	newDbLogger := logImpl.NewDbLogger(logger.Config{
		SlowThreshold:             time.Duration(gaia.GetSafeConfInt64WithDefault("Gorm.SlowThreshold", 200)) * time.Millisecond,
		LogLevel:                  logLevel,
		IgnoreRecordNotFoundError: false,
		Colorful:                  true,
	})
	gaia.SetDbLogger(newDbLogger)
}
