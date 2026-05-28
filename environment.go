package gaia

/*
封装系统环境的一些相关配置信息的获取，比如系统名称等
@author wanlizhan
@created 2023-03-03
*/

import (
	"log"
	"os"
	"sync"
)

const (
	EnvDev        = "dev"
	EnvTest       = "test"
	EnvPrerelease = "prerelease"
	EnvProduct    = "product"
)

// ISystemName 自定义系统名称接口实现
// 默认为本地配置文件中的 SystemEnName, SystemCnName
// 如果存在特殊的动态命名逻辑，可以考虑接口重载
type ISystemName interface {
	//GetSystemEnName 英文名称标识
	GetSystemEnName() string

	//GetSystemCnName 中文名称
	GetSystemCnName() string
}

// SystemNameImplObj 接口逻辑注入
var SystemNameImplObj ISystemName

// isEnvFlagErrReport 环境标记缺失提示
var isEnvFlagErrReport bool = true

var _realRunEnv string
var _realRunEnvRw sync.RWMutex

func init() {
	_realRunEnv = os.Getenv("REAL_RUN_ENV")
}

// DisableEnvFlagErrReport 是否禁用环境标识缺失日志
func DisableEnvFlagErrReport() {
	isEnvFlagErrReport = false
}

// GetSystemEnName 获取系统名称标识(英文标识)
func GetSystemEnName() (systemEnName string) {
	systemEnName = os.Getenv("SystemEnName")
	if SystemNameImplObj != nil && len(SystemNameImplObj.GetSystemEnName()) != 0 {
		systemEnName = SystemNameImplObj.GetSystemEnName()
	} else {
		Log(LogWarnLevel, "SystemNameImplObj-GetSystemEnName:Not Found")
		if len(systemEnName) == 0 {
			Log(LogWarnLevel, "GetEnvKey[SystemEnName]:Not Found")
			systemEnName = "UnknownSystem"
		}
	}

	return
}

var goDeployEnvironmentFlag string
var _goDeployEnvironmentFlagRw sync.RWMutex

func _getGoDeployEnvironmentFlag() string {
	_goDeployEnvironmentFlagRw.RLock()
	defer _goDeployEnvironmentFlagRw.RUnlock()
	return goDeployEnvironmentFlag
}

func _setGoDeployEnvironmentFlag(goEnvFlag string) {
	_goDeployEnvironmentFlagRw.Lock()
	defer _goDeployEnvironmentFlagRw.Unlock()
	goDeployEnvironmentFlag = goEnvFlag
}

// GetEnvFlag 获取环境标识
// 比如： 开发环境-dev  测试环境-test  正式环境-product
func GetEnvFlag() string {
	goEnvFlag := _getGoDeployEnvironmentFlag()
	if len(goEnvFlag) > 0 {
		return goEnvFlag
	}

	//尝试从环境变量中获取 GODEPLOYFLAG 的值
	return getEnvFlag()
}

// getEnvFlag 获取环境标识
// 比如： 开发环境-dev  测试环境-test  正式环境-product
func getEnvFlag() string {
	//尝试从环境变量中获取 GODEPLOYFLAG 的值
	env := os.Getenv("DeployEnvironment")
	if len(env) == 0 {
		env = EnvDev
		if isEnvFlagErrReport {
			log.Printf("Variable `DeployEnvironment` not found in evnironment variables,  auto setting to `%s`.\n", env)
		}
		_setGoDeployEnvironmentFlag(env)
	}
	return env
}

// IsEnvDev 判断当前是否为开发环境
func IsEnvDev() bool {
	env := GetEnvFlag()
	return env == EnvDev
}

// IsEnvTest 当前环境是否为测试环境
func IsEnvTest() bool {
	return GetEnvFlag() == EnvTest
}

// IsEnvProduct 当前环境是否为产品环境
func IsEnvProduct() bool {
	return GetEnvFlag() == EnvProduct
}

// IsEnvPrerelease 是否为预发布环境，当存在环境变量标识为 RUN_ENV=prerelease 时，认为是预发布环境
// 预发布环境除了RUN_ENV值不同外，其它所有的配置(包括GODEPLOYFLAG)往往相同
func IsEnvPrerelease() bool {
	//通过环境变量定位是否为预发布环境
	if GetRealRunEnv() == EnvPrerelease {
		return true
	} else {
		return false
	}
}

// GetRealRunEnv 获取当前系统所在的实际运行环境
// 实际运行环境的标记(REAL_RUN_ENV) 与 GODEPLOYFLAG 有所不同
// REAL_RUN_ENV 指当前系统运行的具体环境位置，更体现不同的区域性，比如product-set1, product-set2, test, prerelease
// 而 GODEPLOYFLAG 指的是运行中的环境分类，比如 product, test, dev，往往与配置中心的配置中对应
func GetRealRunEnv() string {
	_realRunEnvRw.RLock()
	defer _realRunEnvRw.RUnlock()
	return _realRunEnv
}
