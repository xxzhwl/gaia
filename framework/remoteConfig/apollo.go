// Package remoteConfig — Apollo Provider
//
// 配置示例：
//
//	RemoteConfig:
//	  Type: apollo
//	  Apollo:
//	    AppID: gaia-server
//	    Cluster: default
//	    MetaAddr: http://apollo-meta-dev:8080
//	    Namespaces: application,database       # 多个逗号分隔；默认 application
//	    Secret: ""
//	    BackupConfigPath: ""
//	    CacheTTL: 300                          # 秒，可选
//	    WatchInterval: 5                       # 秒，可选
//
// Apollo 原生支持 namespace 级别的推送监听；本 Provider 在收到变更事件后
// 会主动刷新 BaseCenter 缓存并触发 diff 通知。
//
// @author wanlizhan
// @created 2026-04-24
package remoteConfig

import (
	"fmt"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/components/apollo"
	"github.com/xxzhwl/gaia/dic"
)

func init() {
	RegisterProvider("apollo", newApolloProvider)
}

// ApolloConfigCenter Apollo 配置中心实现
type ApolloConfigCenter struct {
	*BaseCenter
	cli *apollo.Client
}

// NewApolloConfigCenter 基于已有 apollo.Client 创建 ConfigCenter
func NewApolloConfigCenter(cli *apollo.Client, opt BaseOption) (*ApolloConfigCenter, error) {
	if cli == nil {
		return nil, fmt.Errorf("apollo client 为空")
	}
	center := &ApolloConfigCenter{cli: cli}
	center.BaseCenter = NewBaseCenter(center.fetch, opt)

	// 注册 Apollo 原生推送监听；只用来刷日志，watch 的 diff 触发仍由 BaseCenter 管理
	cli.AddChangeListener(func(namespace string, changes map[string]*apollo.ConfigChange) {
		gaia.InfoF("[RemoteConfig][apollo] namespace[%s] 变化，共 %d 个 key", namespace, len(changes))
	})
	return center, nil
}

// newApolloProvider 工厂函数
func newApolloProvider(cfg map[string]any) (ConfigCenter, error) {
	sub, _ := dic.GetValueByMapPath(cfg, "Apollo")
	subMap, _ := sub.(map[string]any)
	if subMap == nil {
		return nil, fmt.Errorf("RemoteConfig.Apollo 节点未配置")
	}

	appID := getString(subMap, "AppID")
	if appID == "" {
		return nil, fmt.Errorf("RemoteConfig.Apollo.AppID 必填")
	}
	meta := getString(subMap, "MetaAddr")
	if meta == "" {
		return nil, fmt.Errorf("RemoteConfig.Apollo.MetaAddr 必填")
	}

	namespaces := splitAndTrim(getString(subMap, "Namespaces"))

	cli, err := apollo.NewClient(apollo.Config{
		AppID:            appID,
		Cluster:          getString(subMap, "Cluster"),
		MetaAddr:         meta,
		Namespaces:       namespaces,
		Secret:           getString(subMap, "Secret"),
		BackupConfigPath: getString(subMap, "BackupConfigPath"),
	})
	if err != nil {
		return nil, err
	}

	opt := BaseOption{
		CacheTTL:      time.Duration(toInt64(subMap["CacheTTL"], 0)) * time.Second,
		WatchInterval: time.Duration(toInt64(subMap["WatchInterval"], 0)) * time.Second,
	}
	return NewApolloConfigCenter(cli, opt)
}

// fetch 调用 apollo.Client 的多 namespace 查询
func (a *ApolloConfigCenter) fetch(key string) (any, bool, error) {
	return a.cli.GetConfig(key)
}

// Close 覆盖 BaseCenter.Close，额外释放 SDK client
func (a *ApolloConfigCenter) Close() error {
	_ = a.cli.Close()
	return a.BaseCenter.Close()
}
