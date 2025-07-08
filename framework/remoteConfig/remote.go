// Package remoteConfig 包注释
// @author wanlizhan
// @created 2024-12-05
package remoteConfig

import (
	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/consul"
	"github.com/xxzhwl/gaia/dic"
)

// RegisterConsulRemoteConf 注入远程配置中心为Consul
func RegisterConsulRemoteConf() {
	endPoint := gaia.GetSafeConfString("RemoteConfig.EndPoint")
	path := gaia.GetSafeConfString("RemoteConfig.Path")
	if len(endPoint) == 0 || len(path) == 0 {
		gaia.Info("远程配置中心配置无效")
		return
	}
	gaia.InfoF("注入远程配置中心[Consul:%s-%s]", endPoint, path)
	gaia.GetConfFromRemote = func(key string) (any, bool, error) {
		client, err := consul.NewClient(endPoint)
		if err != nil {
			return nil, false, err
		}

		configs, err := client.GetConfigs(path)
		if err != nil {
			return nil, false, err
		}

		mapPath, err := dic.GetValueByMapPath(configs, key)
		if err != nil {
			return nil, false, err
		}
		if mapPath == nil {
			return nil, false, nil
		}
		return mapPath, true, nil
	}
}
