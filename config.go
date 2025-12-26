// Package gaia 包注释
// @author wanlizhan
// @created 2024-12-03
package gaia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xxzhwl/gaia/cvt"
	"github.com/xxzhwl/gaia/dic"
	"github.com/xxzhwl/gaia/errwrap"
	"gopkg.in/yaml.v3"
)

// 配置文件分为几个部分：1.环境变量；2.本地配置文件；3.远程配置中心
// 最好可以在本地内存中做一个缓存，这些配置中，环境变量可以覆盖本地配置文件可以覆盖远程配置中心
// 同时缓存时间

// 这里要注意一个可能产生循环的地方，比如：
// 若写日志需要一些配置，但是没有给出配置，同时获取配置的过程中有写日志，可能会陷入无限循环
// 如果有组件依赖配置，而拿配置又依赖组件，也就会产生无限循环问题，这里要注意
var (
	remoteConfMap       = map[string]any{}
	remoteConfMapLocker sync.RWMutex
)

const (
	localConfCacheTime           = time.Second * 30
	defaultConfCacheTime         = time.Second * 5
	defaultRemoteConfTimeoutTime = time.Millisecond * 200
)

// GetConfFromRemote 需要依赖外部实现
var GetConfFromRemote func(key string) (any, bool, error)

const DefaultConfigDir = "configs"
const DefaultLocalConfigDir = DefaultConfigDir + Sep + "local"
const DefaultRemoteConfigFile = DefaultConfigDir + Sep + "remote" + Sep + "remoteConfig.json"
const DefaultLocalConfigFile = DefaultLocalConfigDir + Sep + "config.json"

// GetConfFromRemoteConfCenter 获取远程配置中心的配置key，ctx控制超时
func GetConfFromRemoteConfCenter(ctx context.Context, key string) (res any, existed bool, err error) {
	if GetConfFromRemote == nil {
		return nil, false, nil
	}

	// 使用goroutine和channel实现超时控制
	// 因为GetConfFromRemote函数不支持context参数
	done := make(chan struct{})
	go func() {
		defer close(done)
		remote, b, errTemp := GetConfFromRemote(key)
		res = remote
		existed = b
		err = errTemp
	}()

	select {
	case <-ctx.Done():
		Println(LogErrorLevel, fmt.Sprintf("从远程配置中心获取配置%s超时", key))
		return nil, false, ctx.Err()
	case <-done:
		if err != nil {
			return nil, false, err
		}
		if existed {
			return res, true, nil
		}
		return nil, false, nil
	}
}

func GetConfFromLocalFile(key string) (any, bool, error) {
	if len(key) == 0 {
		return nil, false, nil
	}
	result, err := CacheLoad("localConf-"+key, time.Minute*5, func() (result any, err error) {
		v, b, err := getConfFromLocalFile(DefaultLocalConfigFile, key)
		if err != nil {
			return nil, err
		}
		if b {
			return v, nil
		}
		return nil, nil
	})
	if err != nil {
		return nil, false, err
	}
	if result == nil {
		return nil, false, fmt.Errorf("未查询到配置:%s", key)
	}
	return result, true, nil
}

func getConfFromLocalFile(fileName, key string) (any, bool, error) {
	if strings.HasSuffix(fileName, ".json") {
		return getConfFromLocalJsonFile(fileName, key)
	}
	if strings.HasSuffix(fileName, ".yaml") || strings.HasSuffix(fileName, ".yml") {
		return getConfFromLocalYmlFile(fileName, key)
	}
	return nil, false, errors.New("不支持的文件类型")
}

func getConfFromLocalJsonFile(fileName, key string) (any, bool, error) {
	all, err := ReadFileAll(fileName)
	if err != nil {
		return nil, false, err
	}
	res := map[string]any{}
	if err = json.Unmarshal(all, &res); err != nil {
		return nil, false, err
	}
	value, err := dic.GetValueByMapPath(res, key)
	if err == nil {
		return value, true, nil
	}
	return nil, false, nil
}

func getConfFromLocalYmlFile(fileName, key string) (any, bool, error) {
	all, err := ReadFileAll(fileName)
	if err != nil {
		return nil, false, err
	}
	res := map[string]any{}
	if err = yaml.Unmarshal(all, &res); err != nil {
		return nil, false, err
	}
	value, err := dic.GetValueByMapPath(res, key)
	if err == nil {
		return value, true, nil
	}
	return nil, false, nil
}

func GetConf(key string) (any, error) {
	confCacheTime := defaultConfCacheTime

	res, err := CacheLoad("conf-"+key, confCacheTime, func() (result any, err error) {
		TraceF("1.获取环境变量配置")
		//1.找env拿，若没有拿到就无
		envConf, ok := os.LookupEnv(key)
		if ok {
			return envConf, nil
		}

		TraceF("2.获取本地配置")
		//2.找本地文件拿，若没有拿到就3
		fileConf, fileExisted, err := getConfFromLocalFile(DefaultLocalConfigFile, key)
		if fileExisted {
			confCacheTime = localConfCacheTime //本地文件我们认为改动较少，这里缓存时间长一些
			return fileConf, nil
		}

		TraceF("3.获取远程配置")
		ctx, cancel := context.WithTimeout(context.Background(), defaultRemoteConfTimeoutTime)
		defer cancel()
		remoteConf, existed, err := GetConfFromRemoteConfCenter(ctx, key)
		if existed {
			//写入本地文件
			remoteConfMapLocker.Lock()
			remoteConfMap[key] = remoteConf
			remoteConfMapLocker.Unlock()
			marshal, err := json.Marshal(remoteConfMap)
			if err != nil {
				Println(LogErrorLevel, fmt.Sprintf("缓存远程配置到本地文件失败%s", err.Error()))
			} else {
				TraceF("3.1远程配置获取成功，写入文件")
				if errTemp := FilePutContent(DefaultRemoteConfigFile, string(marshal)); errTemp != nil {
					Println(LogErrorLevel, fmt.Sprintf("缓存远程配置到本地文件失败%s", errTemp.Error()))
				}
			}
			return remoteConf, nil
		}
		TraceF("4.获取远程配置本地缓存")
		//如果是超时导致的,可以看下本地缓存的
		if errors.Is(err, context.DeadlineExceeded) {
			TraceF("4.1远程配置获取超时")
			remoteFileConf, remoteFileExisted, err := getConfFromLocalFile(DefaultRemoteConfigFile, key)
			if remoteFileExisted {
				return remoteFileConf, nil
			}
			if err == nil {
				TraceF("4.2远程配置获取超时，获取远程配置本地缓存文件内容")
			} else {
				Println(LogErrorLevel, err.Error())
			}
		}

		//1.进入方法说明缓存不存在，我们直接考虑怎么拿一个配置
		//2.去找远程配置中心拿，若没有或者超时(这里请求远程配置中心，要考虑不能太影响速度，那么就要设置超时时间，我们给它200ms的时间)拿不到就3

		if err != nil {
			return nil, fmt.Errorf("获取配置:%s失败:%s", key, err)
		}
		return nil, errwrap.NewNotFoundError("未查找到该配置:%s", key)
	})
	if err != nil {
		Debug(err.Error())
		return nil, err
	}
	DebugF("获取配置[%s:%v]", key, res)
	return res, nil
}

func GetConfInt64(key string) (int64, error) {
	conf, err := GetConf(key)
	if err != nil {
		return 0, err
	}
	return cvt.GetInt64(conf, fmt.Sprintf("conf:%s转int64失败", key), 0)
}

func GetSafeConfInt64(key string) int64 {
	v, _ := GetConfInt64(key)
	return v
}

func GetSafeConfInt64WithDefault(key string, defaultValue int64) int64 {
	v, err := GetConfInt64(key)
	if err != nil {
		return defaultValue
	}
	return v
}

func GetConfString(key string) (string, error) {
	conf, err := GetConf(key)
	if err != nil {
		return "", err
	}
	return cvt.GetString(conf, fmt.Sprintf("conf:%s转String失败", key), "")
}

func GetSafeConfString(key string) string {
	v, _ := GetConfString(key)
	return v
}

func GetSafeConfStringWithDefault(key string, defaultValue string) string {
	v, err := GetConfString(key)
	if err != nil {
		return defaultValue
	}
	return v
}

func GetConfStringSliceFromString(key string) ([]string, error) {
	v, err := GetConfString(key)
	if err != nil {
		return nil, err
	}
	return SplitStr(v), nil
}

func GetSafeConfStringSliceFromString(key string) []string {
	v, _ := GetConfString(key)
	return SplitStr(v)
}

func GetSafeConfStringSliceFromStringWithDefault(key string, defaultValue []string) []string {
	v, err := GetConfString(key)
	if err != nil {
		return defaultValue
	}
	return SplitStr(v)
}

func GetConfFloat64(key string) (float64, error) {
	conf, err := GetConf(key)
	if err != nil {
		return 0, err
	}
	return cvt.GetFloat64(conf, fmt.Sprintf("conf:%s转Float失败", key), 0)
}

func GetSafeConfFloat64(key string) float64 {
	v, _ := GetConfFloat64(key)
	return v
}

func GetSafeConfFloat64WithDefault(key string, defaultValue float64) float64 {
	v, err := GetConfFloat64(key)
	if err != nil {
		return defaultValue
	}
	return v
}

func GetConfBool(key string) (bool, error) {
	conf, err := GetConf(key)
	if err != nil {
		return false, err
	}
	return cvt.GetBool(conf, fmt.Sprintf("conf:%s转bool失败", key), false)
}

func GetSafeConfBool(key string) bool {
	v, _ := GetConfBool(key)
	return v
}

func GetSafeConfBoolWithDefault(key string, defaultValue bool) bool {
	v, err := GetConfBool(key)
	if err != nil {
		return defaultValue
	}
	return v
}

func LoadConfToObjWithErr(key string, obj any) error {
	conf, err := GetConf(key)
	if err != nil {
		return err
	}
	marshal, err := json.Marshal(conf)
	if err != nil {
		return err
	}
	return json.Unmarshal(marshal, &obj)
}

func LoadConfToObj(key string, obj any) {
	LoadConfToObjWithErr(key, obj)
}

func GetConfSlice[T any](key string) ([]T, error) {
	conf, err := GetConf(key)
	if err != nil {
		return nil, err
	}
	marshal, err := json.Marshal(conf)
	if err != nil {
		return nil, err
	}
	result := make([]T, 0)
	if err = json.Unmarshal(marshal, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func GetSafeConfSlice[T any](key string) []T {
	result := make([]T, 0)
	conf, err := GetConf(key)
	if err != nil {
		return result
	}
	marshal, err := json.Marshal(conf)
	if err != nil {
		return result
	}

	if err = json.Unmarshal(marshal, &result); err != nil {
		return result
	}
	return result
}
