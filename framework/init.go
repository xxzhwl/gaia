// Package framework 注释
// @author wanlizhan
// @created 2024/5/6
package framework

import (
	"context"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/httpclient"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/framework/messageImpl"
	"github.com/xxzhwl/gaia/framework/metrics"
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
	if _, err := remoteConfig.InitFromConfig(); err != nil {
		gaia.WarnF("远程配置中心初始化失败，将无法使用远程配置功能: %s", err.Error())
	}

	ctx := context.Background()
	_, err := tracer.SetupTracer(ctx, gaia.GetSystemEnName())
	if err != nil {
		gaia.WarnF("初始化追踪系统失败: %s，将使用 NoopTracer", err.Error())
	}

	// 指标系统初始化（默认关闭：Framework.Metrics.Enabled=true 时才生效）
	// 失败降级 noop，不阻塞启动。
	if _, err := metrics.Setup(ctx, gaia.GetSystemEnName()); err != nil {
		gaia.WarnF("初始化指标系统失败: %s，将使用 NoopMeterProvider", err.Error())
	}

	//HTTP请求前置处理器注入
	httpclient.SetRequestBeforeHandler(httpclient.DefaultHandler)

	//框架消息提醒注入
	gaia.Message = messageImpl.NewFeiShuRobot()
	//框架DB层日志注入
	// 配置全部读自 Gorm.* 命名空间：Gorm.LocalLevel / Gorm.RemoteLevel / Gorm.SlowThreshold
	// GORM 自身的 logger.Config.LogLevel 由 NewFrameworkDbLogger 内部从两个 level 推导。
	newDbLogger := logImpl.NewFrameworkDbLogger()
	gaia.SetDbLogger(newDbLogger)
	gaia.InfoF("注入DB Logger: localLevel=%s remoteLevel=%s slow=%dms",
		newDbLogger.LevelText(newDbLogger.LocalLevel()),
		newDbLogger.LevelText(newDbLogger.RemoteLevel()),
		newDbLogger.Config.SlowThreshold/time.Millisecond)

	// ===== 组件依赖检测（Init 最后执行）=====
	// 统一检测所有组件配置状态，可选组件缺失仅 warn 一次，必选组件缺失则 panic
	if hasRequiredMissing := checkComponents(); hasRequiredMissing {
		panic("存在必选组件未配置，无法启动。请检查上方的组件检测报告。")
	}

	// ===== 远程日志状态初始化 =====
	// 组件检查完成后：根据 ES 配置状态同步远程日志开关；
	// 并启动后台 watcher，支持 ES 配置热恢复（从无到有）/ 热关闭（从有到无）
	syncRemoteLogByESConfig()
	startRemoteLogWatcher()

	// ===== 标记框架已装配完成 =====
	// 供 EnsureInitialized 自检使用，让"忘了调 Init"从静默降级变成显式告警。
	markInitialized()
}
