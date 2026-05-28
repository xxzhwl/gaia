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
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/xxzhwl/gaia/cvt"
	"github.com/xxzhwl/gaia/dic"
	"github.com/xxzhwl/gaia/errwrap"
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

	// requestedRemoteKeys 记录所有"曾经走到远端配置中心"的 key
	// 远端中心在收到推送时可以基于该集合精准失效一二级缓存
	requestedRemoteKeys   = map[string]struct{}{}
	requestedRemoteKeysMu sync.RWMutex

	// RemoteSnapshotOwned 表示远端配置中心已自行接管 remoteConfig.json 文件的写入。
	// 为 true 时 GetConf 不再以 flat-map 形式追加写入快照文件，
	// 这样文件可以作为远端 YAML/JSON 的"完整镜像"使用，便于审阅和冷启动兜底。
	// 通常由 NacosConfigCenter 等支持 ConfigDumper 的 Provider 在装配时置为 true。
	RemoteSnapshotOwned bool

	// confInflight: 同一个 key 同时只允许一个 goroutine 去加载，其它 goroutine 等待结果
	// 解决 CacheLoad 并发未去重问题，避免冷启动期对远端/本地的重复 IO
	confInflight sync.Map // map[string]*inflightCall

	// localFileCache: 本地配置文件解析后的整体快照缓存，避免每次 cache miss 都
	// 完整 ReadFile + Unmarshal。失效条件 = TTL 到期 或 文件 mtime 变化
	localFileCache sync.Map // map[string]*localFileSnapshot

	// cachedRemoteConfTimeout: 远端拉取超时配置的解析结果缓存
	// 避免每次 GetConf 远端 path 都重新读 config.json + Unmarshal
	cachedRemoteConfTimeout atomic.Int64 // ns
	cachedRemoteConfExpire  atomic.Int64 // unix ns
)

// confBox 包裹真实配置值，规避 cache.Empty() 把 0/""/0.0/false 等合法零值视为空
// 一律使用 *confBox 入缓存 → 永远非空 → 一级缓存对所有合法值生效
type confBox struct {
	v        any
	notFound bool // true 表示"确认不存在"的负缓存条目
}

// inflightCall 用于同 key 并发去重
type inflightCall struct {
	done chan struct{}
	v    any
	err  error
}

// localFileSnapshot 本地配置文件整文件快照
type localFileSnapshot struct {
	raw      map[string]any
	mtime    time.Time
	expireAt time.Time
}

const (
	localConfCacheTime           = time.Second * 30
	defaultConfCacheTime         = time.Second * 5
	negativeConfCacheTime        = time.Second * 30
	localFileSnapshotTTL         = time.Second * 30
	remoteTimeoutCacheTTL        = time.Second * 30
	maxRequestedRemoteKeys       = 10000
	defaultRemoteConfTimeoutTime = time.Millisecond * 2000
	envRemoteConfTimeoutMs       = "GAIA_REMOTE_CONF_TIMEOUT_MS"
	localRemoteConfTimeoutKey    = "RemoteConfig.RemoteTimeoutMs"
)

// getRemoteConfTimeout 读取远程配置超时时间。
// 优先级：环境变量 GAIA_REMOTE_CONF_TIMEOUT_MS > 本地配置文件 RemoteConfig.RemoteTimeoutMs > 默认 2000ms
// 直接从本地文件读取而非通过 GetConf，避免递归。
// 结果用 atomic 缓存 30s，避免每次 GetConf 远端路径都重新读盘+Unmarshal。
func getRemoteConfTimeout() time.Duration {
	now := time.Now().UnixNano()
	if exp := cachedRemoteConfExpire.Load(); exp > 0 && now < exp {
		if v := cachedRemoteConfTimeout.Load(); v > 0 {
			return time.Duration(v)
		}
	}
	d := resolveRemoteConfTimeout()
	cachedRemoteConfTimeout.Store(int64(d))
	cachedRemoteConfExpire.Store(now + int64(remoteTimeoutCacheTTL))
	return d
}

// resolveRemoteConfTimeout 不走缓存的真实解析逻辑
func resolveRemoteConfTimeout() time.Duration {
	if v := os.Getenv(envRemoteConfTimeoutMs); v != "" {
		var ms int64
		if _, err := fmt.Sscanf(v, "%d", &ms); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	if val, found, _ := getConfFromLocalFile(ResolveLocalConfigFile(), localRemoteConfTimeoutKey); found {
		switch n := val.(type) {
		case float64:
			if n > 0 {
				return time.Duration(n) * time.Millisecond
			}
		case int:
			if n > 0 {
				return time.Duration(n) * time.Millisecond
			}
		case int64:
			if n > 0 {
				return time.Duration(n) * time.Millisecond
			}
		case string:
			var ms int64
			if _, err := fmt.Sscanf(n, "%d", &ms); err == nil && ms > 0 {
				return time.Duration(ms) * time.Millisecond
			}
		}
	}
	return defaultRemoteConfTimeoutTime
}

// trackRequestedRemoteKey 记录已从远端读取过的 key
// 加上限保护，避免错别字 key 持续累积导致内存泄漏
func trackRequestedRemoteKey(key string) {
	requestedRemoteKeysMu.RLock()
	_, exists := requestedRemoteKeys[key]
	size := len(requestedRemoteKeys)
	requestedRemoteKeysMu.RUnlock()
	if exists {
		return
	}
	if size >= maxRequestedRemoteKeys {
		// 超出上限不再追加，但保证现有的能被精准失效
		return
	}
	requestedRemoteKeysMu.Lock()
	if len(requestedRemoteKeys) < maxRequestedRemoteKeys {
		requestedRemoteKeys[key] = struct{}{}
	}
	requestedRemoteKeysMu.Unlock()
}

// ListRequestedRemoteKeys 返回所有曾经命中过远端配置中心的 key 快照
// 远端中心在收到 DataId 整体变更推送后，可以遍历该列表批量失效一级缓存
func ListRequestedRemoteKeys() []string {
	requestedRemoteKeysMu.RLock()
	defer requestedRemoteKeysMu.RUnlock()
	out := make([]string, 0, len(requestedRemoteKeys))
	for k := range requestedRemoteKeys {
		out = append(out, k)
	}
	return out
}

// GetConfFromRemote 需要依赖外部实现
var GetConfFromRemote func(key string) (any, bool, error)

const DefaultConfigDir = "configs"
const DefaultLocalConfigDir = DefaultConfigDir + Sep + "local"
const DefaultRemoteConfigFile = DefaultConfigDir + Sep + "remote" + Sep + "remoteConfig.json"

// DefaultLocalConfigFile 本地配置文件的"首选 / 写入"路径（JSON）。
//
// 兼容性：本常量用于测试 / 外部写入路径；运行时读取请使用 ResolveLocalConfigFile()，
// 它会按 json → yaml → yml 顺序选择第一个存在的文件，从而原生支持 YAML 配置。
const DefaultLocalConfigFile = DefaultLocalConfigDir + Sep + "config.json"

// DefaultLocalConfigYamlFile / DefaultLocalConfigYmlFile YAML 候选路径
const (
	DefaultLocalConfigYamlFile = DefaultLocalConfigDir + Sep + "config.yaml"
	DefaultLocalConfigYmlFile  = DefaultLocalConfigDir + Sep + "config.yml"
)

// localConfigCandidates 本地配置文件候选列表（优先级从高到低）
//
// 同时存在多份时按列表顺序"先到先得"。建议项目中只放一份，避免维护两份不一致的本地配置。
func localConfigCandidates() []string {
	return []string{
		DefaultLocalConfigFile,
		DefaultLocalConfigYamlFile,
		DefaultLocalConfigYmlFile,
	}
}

// ResolveLocalConfigFile 返回当前实际生效的本地配置文件路径
//
//   - 按 json → yaml → yml 顺序探测，返回首个存在的文件
//   - 都不存在时返回首选（config.json）路径——交由读取方再处理 NotExist
//
// 注意：本函数每次调用都会做最多 3 次 os.Stat；GetConf 主链路上层有 30s 文件级别快照缓存，
// 不会让重复调用造成实质开销。
func ResolveLocalConfigFile() string {
	for _, p := range localConfigCandidates() {
		if FileExists(p) {
			return p
		}
	}
	return DefaultLocalConfigFile
}

// GetConfFromRemoteConfCenter 获取远程配置中心的配置key，ctx控制超时
func GetConfFromRemoteConfCenter(ctx context.Context, key string) (res any, existed bool, err error) {
	getRemote := GetConfFromRemote
	if getRemote == nil {
		return nil, false, nil
	}

	type remoteResult struct {
		res     any
		existed bool
		err     error
	}
	// 使用 goroutine 和 channel 实现超时控制，因为 GetConfFromRemote 函数不支持 context 参数。
	done := make(chan remoteResult, 1)
	go func() {
		remote, b, errTemp := getRemote(key)
		done <- remoteResult{res: remote, existed: b, err: errTemp}
	}()

	select {
	case <-ctx.Done():
		Println(LogErrorLevel, fmt.Sprintf("从远程配置中心获取配置%s超时", key))
		return nil, false, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return nil, false, result.err
		}
		if result.existed {
			return result.res, true, nil
		}
		return nil, false, nil
	}
}

// GetConfFromLocalFile 从本地文件获取配置值
// 注意：与 GetConf 共享文件级别快照缓存（30s TTL + mtime 失效），
// 这层 confBox 一级缓存只保证"key 维度的查询结果"也被复用。
func GetConfFromLocalFile(key string) (any, bool, error) {
	if len(key) == 0 {
		return nil, false, nil
	}
	cKey := "localConf-" + key
	cache := NewCache()
	if cached := cache.Get(cKey); cached != nil {
		if box, ok := cached.(*confBox); ok {
			if box.notFound {
				return nil, false, fmt.Errorf("未查询到配置:%s", key)
			}
			return box.v, true, nil
		}
	}
	v, found, err := getConfFromLocalFile(ResolveLocalConfigFile(), key)
	if err != nil {
		return nil, false, err
	}
	if !found {
		cache.Set(cKey, &confBox{notFound: true}, negativeConfCacheTime)
		return nil, false, fmt.Errorf("未查询到配置:%s", key)
	}
	cache.Set(cKey, &confBox{v: v}, localConfCacheTime)
	return v, true, nil
}

func getConfFromLocalFile(fileName, key string) (any, bool, error) {
	parsed, err := loadLocalFileSnapshot(fileName)
	if err != nil {
		return nil, false, err
	}
	if parsed == nil {
		return nil, false, nil
	}
	if value, ok := parsed[key]; ok {
		return value, true, nil
	}
	value, gerr := dic.GetValueByMapPath(parsed, key)
	if gerr == nil {
		return value, true, nil
	}
	return nil, false, nil
}

// loadLocalFileSnapshot 加载本地配置文件并解析为 map，结果按文件名缓存。
// 失效条件：TTL 到期 或 文件 mtime 变化，任一满足都会重新解析。
// 这样多 key 串行查询同一个 config.json 时只需 1 次磁盘读+解析。
func loadLocalFileSnapshot(fileName string) (map[string]any, error) {
	if cached, ok := localFileCache.Load(fileName); ok {
		snap := cached.(*localFileSnapshot)
		if time.Now().Before(snap.expireAt) {
			// 在 TTL 内，再做一次轻量 mtime check
			if info, err := os.Stat(fileName); err == nil && info.ModTime().Equal(snap.mtime) {
				return snap.raw, nil
			}
		}
	}

	raw, mtime, err := parseLocalFile(fileName)
	if err != nil {
		// 文件不存在不算错（让上层判 found=false 走下一级），其他错误透传
		if os.IsNotExist(err) {
			localFileCache.Store(fileName, &localFileSnapshot{
				raw:      nil,
				mtime:    time.Time{},
				expireAt: time.Now().Add(localFileSnapshotTTL),
			})
			return nil, nil
		}
		return nil, err
	}
	localFileCache.Store(fileName, &localFileSnapshot{
		raw:      raw,
		mtime:    mtime,
		expireAt: time.Now().Add(localFileSnapshotTTL),
	})
	return raw, nil
}

// parseLocalFile 真正的磁盘读 + Unmarshal
func parseLocalFile(fileName string) (map[string]any, time.Time, error) {
	info, err := os.Stat(fileName)
	if err != nil {
		return nil, time.Time{}, err
	}
	all, err := ReadFileAll(fileName)
	if err != nil {
		return nil, time.Time{}, err
	}
	res := map[string]any{}
	switch {
	case strings.HasSuffix(fileName, ".json"):
		if err = json.Unmarshal(all, &res); err != nil {
			return nil, time.Time{}, err
		}
	case strings.HasSuffix(fileName, ".yaml"), strings.HasSuffix(fileName, ".yml"):
		if err = yaml.Unmarshal(all, &res); err != nil {
			return nil, time.Time{}, err
		}
	default:
		return nil, time.Time{}, errors.New("不支持的文件类型")
	}
	return res, info.ModTime(), nil
}

// invalidateLocalFileSnapshot 清掉文件快照缓存（外部修改文件后可主动调用）
func invalidateLocalFileSnapshot(fileName string) {
	localFileCache.Delete(fileName)
}

// GetConf 获取配置值，支持环境变量、本地文件、远程配置中心多级来源
//
// 性能与正确性保证：
//  1. 一级缓存：用 *confBox 包裹，不受 cache.Empty 误判 0/""/0.0/false 的影响
//  2. 同 key 并发去重：同时只允许 1 个 goroutine 走 fresh path
//  3. 本地文件级别快照缓存：100 个 key 启动期只读 1 次盘
//  4. 短 TTL 负缓存：未命中的 key 30s 内不再打远端
//  5. 容灾：本地文件 IO 错误不影响远端读取；远端失败回退本地快照文件
func GetConf(key string) (any, error) {
	cKey := "conf-" + key
	cache := NewCache()

	// 一级缓存命中
	if cached := cache.Get(cKey); cached != nil {
		if box, ok := cached.(*confBox); ok {
			if box.notFound {
				return nil, errwrap.NewNotFoundError("未查找到该配置:%s", key)
			}
			return box.v, nil
		}
	}

	// inflight 去重：同 key 只允许 1 个 goroutine 进入 fresh path
	call := &inflightCall{done: make(chan struct{})}
	if existing, loaded := confInflight.LoadOrStore(cKey, call); loaded {
		ec := existing.(*inflightCall)
		<-ec.done
		return ec.v, ec.err
	}
	defer func() {
		confInflight.Delete(cKey)
		close(call.done)
	}()

	v, ttl, found, err := loadConfFresh(key)
	if err != nil {
		// 不缓存"系统错误"（如远端真实出错），避免短时故障期错误结果被锁定
		call.err = err
		return nil, err
	}
	if !found {
		// 短 TTL 负缓存，防止 missing key 反复打远端
		cache.Set(cKey, &confBox{notFound: true}, negativeConfCacheTime)
		nfErr := errwrap.NewNotFoundError("未查找到该配置:%s", key)
		call.err = nfErr
		return nil, nfErr
	}

	cache.Set(cKey, &confBox{v: v}, ttl)
	call.v = v
	DebugF("获取配置[%s:%v]", key, v)
	return v, nil
}

// loadConfFresh 完整一遍 env → local → remote 的取值链路，返回 (值, TTL, 是否命中, 错误)
//
// TTL 决策：
//   - env / remote 命中 → defaultConfCacheTime (5s)，便于响应快速变更
//   - local 命中 → localConfCacheTime (30s)，本地文件改动较少
func loadConfFresh(key string) (val any, ttl time.Duration, found bool, err error) {
	// 1. 环境变量（最高优先级，进程启动期固定）
	TraceF("1.获取环境变量配置")
	if envConf, ok := os.LookupEnv(key); ok {
		return envConf, defaultConfCacheTime, true, nil
	}

	// 2. 本地配置文件
	TraceF("2.获取本地配置")
	fileConf, fileExisted, fileErr := getConfFromLocalFile(ResolveLocalConfigFile(), key)
	if fileExisted {
		return fileConf, localConfCacheTime, true, nil
	}
	// 本地文件 IO 错误：记日志，但不阻断后续远端读取
	if fileErr != nil {
		Println(LogErrorLevel, fmt.Sprintf("读本地配置文件失败 key=%s: %s", key, fileErr.Error()))
	}

	// 3. 远端配置中心
	TraceF("3.获取远程配置")
	if GetConfFromRemote != nil {
		ctx, cancel := context.WithTimeout(context.Background(), getRemoteConfTimeout())
		defer cancel()
		trackRequestedRemoteKey(key)
		remoteConf, existed, rErr := GetConfFromRemoteConfCenter(ctx, key)
		if existed {
			persistRemoteConfToFile(key, remoteConf)
			return remoteConf, defaultConfCacheTime, true, nil
		}
		// 远端"明确不存在" → found=false（走负缓存）
		if rErr == nil {
			return nil, 0, false, nil
		}
		// 远端 IO 失败 → 4. 走本地兜底快照
		TraceF("3.1远程配置获取失败(%v)，尝试本地快照兜底", rErr)
		if remoteFileConf, remoteFileExisted, _ := getConfFromLocalFile(DefaultRemoteConfigFile, key); remoteFileExisted {
			return remoteFileConf, defaultConfCacheTime, true, nil
		}
		// 兜底也没有 → 透出原始错误
		return nil, 0, false, fmt.Errorf("获取配置:%s失败:%w", key, rErr)
	}

	// 没装配远端 Provider → 试本地远端快照（冷启动场景）
	if remoteFileConf, remoteFileExisted, _ := getConfFromLocalFile(DefaultRemoteConfigFile, key); remoteFileExisted {
		return remoteFileConf, defaultConfCacheTime, true, nil
	}
	return nil, 0, false, nil
}

// persistRemoteConfToFile 把单 key 远端结果以 flat-map 累积写入快照文件
// 仅在远端 Provider 未自管快照时启用
func persistRemoteConfToFile(key string, val any) {
	if RemoteSnapshotOwned {
		return
	}
	remoteConfMapLocker.Lock()
	if len(remoteConfMap) == 0 {
		loadRemoteConfSnapshotLocked()
	}
	remoteConfMap[key] = val
	marshal, err := json.Marshal(remoteConfMap)
	if err != nil {
		remoteConfMapLocker.Unlock()
		Println(LogErrorLevel, fmt.Sprintf("序列化远端配置快照失败:%s", err.Error()))
		return
	}
	if errTemp := MkDirAll(filepath.Dir(DefaultRemoteConfigFile), 0o755); errTemp != nil {
		remoteConfMapLocker.Unlock()
		Println(LogErrorLevel, fmt.Sprintf("创建远端配置快照目录失败:%s", errTemp.Error()))
		return
	}
	if errTemp := FilePutContent(DefaultRemoteConfigFile, string(marshal)); errTemp != nil {
		remoteConfMapLocker.Unlock()
		Println(LogErrorLevel, fmt.Sprintf("写远端配置快照失败:%s", errTemp.Error()))
		return
	}
	invalidateLocalFileSnapshot(DefaultRemoteConfigFile)
	remoteConfMapLocker.Unlock()
}

// loadRemoteConfSnapshotLocked 在 remoteConfMapLocker 写锁内加载已有快照。
func loadRemoteConfSnapshotLocked() {
	parsed, _, err := parseLocalFile(DefaultRemoteConfigFile)
	if err != nil || parsed == nil {
		return
	}
	for k, v := range parsed {
		remoteConfMap[k] = v
	}
}

// GetConfInt64 获取int64类型的配置值
func GetConfInt64(key string) (int64, error) {
	conf, err := GetConf(key)
	if err != nil {
		return 0, err
	}
	return cvt.GetInt64(conf, fmt.Sprintf("conf:%s转int64失败", key), 0)
}

// GetSafeConfInt64 安全获取int64配置值，出错时返回零值
func GetSafeConfInt64(key string) int64 {
	v, _ := GetConfInt64(key)
	return v
}

// GetSafeConfInt64WithDefault 安全获取int64配置值，支持自定义默认值
func GetSafeConfInt64WithDefault(key string, defaultValue int64) int64 {
	v, err := GetConfInt64(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// GetConfString 获取string类型的配置值
func GetConfString(key string) (string, error) {
	conf, err := GetConf(key)
	if err != nil {
		return "", err
	}
	return cvt.GetString(conf, fmt.Sprintf("conf:%s转String失败", key), "")
}

// GetSafeConfString 安全获取string配置值，出错时返回空字符串
func GetSafeConfString(key string) string {
	v, _ := GetConfString(key)
	return v
}

// GetSafeConfStringWithDefault 安全获取string配置值，支持自定义默认值
func GetSafeConfStringWithDefault(key string, defaultValue string) string {
	v, err := GetConfString(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// GetConfStringSliceFromString 获取字符串切片配置值，将配置字符串按分隔符分割
func GetConfStringSliceFromString(key string) ([]string, error) {
	v, err := GetConfString(key)
	if err != nil {
		return nil, err
	}
	return SplitStr(v), nil
}

// GetSafeConfStringSliceFromString 安全获取字符串切片配置值，出错时返回空切片
func GetSafeConfStringSliceFromString(key string) []string {
	v, _ := GetConfString(key)
	return SplitStr(v)
}

// GetSafeConfStringSliceFromStringWithDefault 安全获取字符串切片配置值，支持自定义默认值
func GetSafeConfStringSliceFromStringWithDefault(key string, defaultValue []string) []string {
	v, err := GetConfString(key)
	if err != nil {
		return defaultValue
	}
	return SplitStr(v)
}

// GetConfFloat64 获取float64类型的配置值
func GetConfFloat64(key string) (float64, error) {
	conf, err := GetConf(key)
	if err != nil {
		return 0, err
	}
	return cvt.GetFloat64(conf, fmt.Sprintf("conf:%s转Float失败", key), 0)
}

// GetSafeConfFloat64 安全获取float64配置值，出错时返回零值
func GetSafeConfFloat64(key string) float64 {
	v, _ := GetConfFloat64(key)
	return v
}

// GetSafeConfFloat64WithDefault 安全获取float64配置值，支持自定义默认值
func GetSafeConfFloat64WithDefault(key string, defaultValue float64) float64 {
	v, err := GetConfFloat64(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// GetConfBool 获取bool类型的配置值
func GetConfBool(key string) (bool, error) {
	conf, err := GetConf(key)
	if err != nil {
		return false, err
	}
	return cvt.GetBool(conf, fmt.Sprintf("conf:%s转bool失败", key), false)
}

// GetSafeConfBool 安全获取bool配置值，出错时返回false
func GetSafeConfBool(key string) bool {
	v, _ := GetConfBool(key)
	return v
}

// GetSafeConfBoolWithDefault 安全获取bool配置值，支持自定义默认值
func GetSafeConfBoolWithDefault(key string, defaultValue bool) bool {
	v, err := GetConfBool(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// LoadConfToObjWithErr 加载配置到对象，返回错误信息
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

// LoadConfToObj 加载配置到对象，忽略错误
func LoadConfToObj(key string, obj any) {
	LoadConfToObjWithErr(key, obj)
}

// BindConfigWithErr 通过结构体标签绑定配置
// 标签格式：config:"配置键名"
// 示例：
//
//	type Config struct {
//	    Port string `config:"Server.Port"`
//	}
func BindConfigWithErr(confArg any) error {
	val := reflect.ValueOf(confArg)
	if val.Kind() != reflect.Ptr || val.Elem().Kind() != reflect.Struct {
		return errors.New("confArg must be a pointer to struct")
	}

	return bindConfigRecursive(val.Elem())
}

// timeType 反射比较用的 time.Time 类型缓存
var timeType = reflect.TypeOf(time.Time{})

// bindConfigRecursive 递归绑定配置到结构体字段
func bindConfigRecursive(val reflect.Value) error {
	typ := val.Type()
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		// time.Time 字段不递归，直接走 setFieldValue
		if field.Kind() == reflect.Struct && field.Type() == timeType {
			tag := fieldType.Tag.Get("config")
			if tag == "" {
				continue
			}
			confValue, err := GetConf(tag)
			if err != nil {
				continue
			}
			if err := setFieldValue(field, confValue); err != nil {
				return fmt.Errorf("failed to set field %s: %w", fieldType.Name, err)
			}
			continue
		}

		// 如果是嵌套结构体（非匿名），递归处理
		if field.Kind() == reflect.Struct && fieldType.Anonymous {
			// 匿名结构体，嵌入字段
			if err := bindConfigRecursive(field); err != nil {
				return err
			}
			continue
		} else if field.Kind() == reflect.Struct {
			// 非匿名结构体字段，递归处理
			if err := bindConfigRecursive(field); err != nil {
				return err
			}
			continue
		}

		// 获取config标签
		tag := fieldType.Tag.Get("config")
		if tag == "" {
			continue // 没有配置标签，跳过
		}

		// 获取配置值
		confValue, err := GetConf(tag)
		if err != nil {
			// 配置不存在，可以跳过或返回错误
			// 这里选择跳过，保持字段的零值
			continue
		}

		// 根据字段类型进行转换和赋值
		if err := setFieldValue(field, confValue); err != nil {
			return fmt.Errorf("failed to set field %s: %w", fieldType.Name, err)
		}
	}
	return nil
}

// setFieldValue 设置结构体字段的值
func setFieldValue(field reflect.Value, confValue any) error {
	if !field.CanSet() {
		return errors.New("field cannot be set")
	}

	// time.Time 特殊处理
	if field.Kind() == reflect.Struct && field.Type() == timeType {
		t, err := cvt.GetTime(confValue, "", time.Time{})
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(t))
		return nil
	}

	// 指针字段：先实例化再递归
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return setFieldValue(field.Elem(), confValue)
	}

	switch field.Kind() {
	case reflect.String:
		str, err := cvt.GetString(confValue, "", "")
		if err != nil {
			return err
		}
		field.SetString(str)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// time.Duration 底层类型也是 int64，单独识别
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := cvt.GetDuration(confValue, "", 0)
			if err != nil {
				return err
			}
			field.SetInt(int64(d))
			return nil
		}
		i, err := cvt.GetInt64(confValue, "", 0)
		if err != nil {
			return err
		}
		field.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		i, err := cvt.GetInt64(confValue, "", 0)
		if err != nil {
			return err
		}
		field.SetUint(uint64(i))
	case reflect.Float32, reflect.Float64:
		f, err := cvt.GetFloat64(confValue, "", 0)
		if err != nil {
			return err
		}
		field.SetFloat(f)
	case reflect.Bool:
		b, err := cvt.GetBool(confValue, "", false)
		if err != nil {
			return err
		}
		field.SetBool(b)
	case reflect.Slice:
		// 处理切片类型
		sliceType := field.Type()
		jsonBytes, err := json.Marshal(confValue)
		if err != nil {
			return err
		}
		slicePtr := reflect.New(sliceType)
		if err := json.Unmarshal(jsonBytes, slicePtr.Interface()); err != nil {
			return err
		}
		field.Set(slicePtr.Elem())
	case reflect.Map:
		// 处理 map 类型，使用 JSON 进行兼容反序列化
		mapType := field.Type()
		jsonBytes, err := json.Marshal(confValue)
		if err != nil {
			return err
		}
		mapPtr := reflect.New(mapType)
		if err := json.Unmarshal(jsonBytes, mapPtr.Interface()); err != nil {
			return err
		}
		field.Set(mapPtr.Elem())
	default:
		// 其他类型暂不支持
		return fmt.Errorf("unsupported field type: %s", field.Kind())
	}

	return nil
}

// BindConfig 通过结构体标签绑定配置，忽略错误
func BindConfig(confArg any) {
	BindConfigWithErr(confArg)
}

// GetConfSlice 获取切片类型的配置值
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

// GetSafeConfSlice 安全获取切片类型配置值，出错时返回空切片
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

// GetSafeConfSliceWithDefault 安全获取切片类型配置值，出错或不存在时返回默认值
func GetSafeConfSliceWithDefault[T any](key string, defaultValue []T) []T {
	conf, err := GetConf(key)
	if err != nil {
		return defaultValue
	}
	marshal, err := json.Marshal(conf)
	if err != nil {
		return defaultValue
	}
	result := make([]T, 0)
	if err = json.Unmarshal(marshal, &result); err != nil {
		return defaultValue
	}
	return result
}

// =============================================================================
// int / uint64 系列
// =============================================================================

// GetConfInt 获取 int 类型的配置值
func GetConfInt(key string) (int, error) {
	v, err := GetConfInt64(key)
	if err != nil {
		return 0, err
	}
	return int(v), nil
}

// GetSafeConfInt 安全获取 int 配置值，出错时返回零值
func GetSafeConfInt(key string) int {
	v, _ := GetConfInt(key)
	return v
}

// GetSafeConfIntWithDefault 安全获取 int 配置值，出错时返回默认值
func GetSafeConfIntWithDefault(key string, defaultValue int) int {
	v, err := GetConfInt(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// GetConfUint64 获取 uint64 类型的配置值
func GetConfUint64(key string) (uint64, error) {
	conf, err := GetConf(key)
	if err != nil {
		return 0, err
	}
	i, err := cvt.GetInt64(conf, fmt.Sprintf("conf:%s转uint64失败", key), 0)
	if err != nil {
		return 0, err
	}
	if i < 0 {
		return 0, fmt.Errorf("conf:%s 为负数, 无法转换为 uint64", key)
	}
	return uint64(i), nil
}

// GetSafeConfUint64 安全获取 uint64 配置值，出错时返回零值
func GetSafeConfUint64(key string) uint64 {
	v, _ := GetConfUint64(key)
	return v
}

// GetSafeConfUint64WithDefault 安全获取 uint64 配置值，出错时返回默认值
func GetSafeConfUint64WithDefault(key string, defaultValue uint64) uint64 {
	v, err := GetConfUint64(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// =============================================================================
// 时间相关
// =============================================================================

// GetConfTime 获取 time.Time 类型的配置值
// 支持的格式见 cvt.GetTime
func GetConfTime(key string) (time.Time, error) {
	conf, err := GetConf(key)
	if err != nil {
		return time.Time{}, err
	}
	return cvt.GetTime(conf, fmt.Sprintf("conf:%s转time.Time失败", key), time.Time{})
}

// GetSafeConfTime 安全获取 time.Time 配置值，出错时返回零值
func GetSafeConfTime(key string) time.Time {
	v, _ := GetConfTime(key)
	return v
}

// GetSafeConfTimeWithDefault 安全获取 time.Time 配置值，出错时返回默认值
func GetSafeConfTimeWithDefault(key string, defaultValue time.Time) time.Time {
	v, err := GetConfTime(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// GetConfTimeWithLayout 使用指定布局解析时间字符串配置
func GetConfTimeWithLayout(key, layout string) (time.Time, error) {
	conf, err := GetConf(key)
	if err != nil {
		return time.Time{}, err
	}
	return cvt.GetTimeWithLayout(conf, layout, time.Time{})
}

// GetConfDuration 获取 time.Duration 类型的配置值
// 支持 "200ms"、"1h30m" 字符串及数字（按毫秒解释）
func GetConfDuration(key string) (time.Duration, error) {
	conf, err := GetConf(key)
	if err != nil {
		return 0, err
	}
	return cvt.GetDuration(conf, fmt.Sprintf("conf:%s转time.Duration失败", key), 0)
}

// GetSafeConfDuration 安全获取 time.Duration 配置值，出错时返回零值
func GetSafeConfDuration(key string) time.Duration {
	v, _ := GetConfDuration(key)
	return v
}

// GetSafeConfDurationWithDefault 安全获取 time.Duration 配置值，出错时返回默认值
func GetSafeConfDurationWithDefault(key string, defaultValue time.Duration) time.Duration {
	v, err := GetConfDuration(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// =============================================================================
// Map 相关
// =============================================================================

// GetConfMap 获取 map[string]any 类型的配置值
func GetConfMap(key string) (map[string]any, error) {
	conf, err := GetConf(key)
	if err != nil {
		return nil, err
	}
	if m, ok := conf.(map[string]any); ok {
		return m, nil
	}
	marshal, err := json.Marshal(conf)
	if err != nil {
		return nil, err
	}
	res := map[string]any{}
	if err = json.Unmarshal(marshal, &res); err != nil {
		return nil, fmt.Errorf("conf:%s转map[string]any失败:%w", key, err)
	}
	return res, nil
}

// GetSafeConfMap 安全获取 map[string]any 配置值，出错时返回 nil
func GetSafeConfMap(key string) map[string]any {
	v, _ := GetConfMap(key)
	return v
}

// GetSafeConfMapWithDefault 安全获取 map[string]any 配置值，出错时返回默认值
func GetSafeConfMapWithDefault(key string, defaultValue map[string]any) map[string]any {
	v, err := GetConfMap(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// GetConfMapT 获取 map[string]V 类型的配置值（泛型）
func GetConfMapT[V any](key string) (map[string]V, error) {
	conf, err := GetConf(key)
	if err != nil {
		return nil, err
	}
	marshal, err := json.Marshal(conf)
	if err != nil {
		return nil, err
	}
	res := map[string]V{}
	if err = json.Unmarshal(marshal, &res); err != nil {
		return nil, fmt.Errorf("conf:%s转map失败:%w", key, err)
	}
	return res, nil
}

// GetSafeConfMapT 安全获取 map[string]V 类型配置值
func GetSafeConfMapT[V any](key string) map[string]V {
	v, _ := GetConfMapT[V](key)
	return v
}

// =============================================================================
// 字节大小
// =============================================================================

// GetConfByteSize 解析 "10MB"、"1.5GiB" 等字符串，返回字节数
func GetConfByteSize(key string) (int64, error) {
	conf, err := GetConf(key)
	if err != nil {
		return 0, err
	}
	return cvt.GetByteSize(conf, fmt.Sprintf("conf:%s转ByteSize失败", key), 0)
}

// GetSafeConfByteSize 安全获取字节数配置值，出错时返回零值
func GetSafeConfByteSize(key string) int64 {
	v, _ := GetConfByteSize(key)
	return v
}

// GetSafeConfByteSizeWithDefault 安全获取字节数配置值，出错时返回默认值
func GetSafeConfByteSizeWithDefault(key string, defaultValue int64) int64 {
	v, err := GetConfByteSize(key)
	if err != nil {
		return defaultValue
	}
	return v
}

// =============================================================================
// Must 系列：缺失或转换失败直接 panic，适合启动期 fail-fast
// =============================================================================

// MustGetConfString 配置缺失或转换失败时 panic
func MustGetConfString(key string) string {
	v, err := GetConfString(key)
	if err != nil {
		panic(fmt.Sprintf("MustGetConfString(%s) failed: %s", key, err.Error()))
	}
	return v
}

// MustGetConfInt 配置缺失或转换失败时 panic
func MustGetConfInt(key string) int {
	v, err := GetConfInt(key)
	if err != nil {
		panic(fmt.Sprintf("MustGetConfInt(%s) failed: %s", key, err.Error()))
	}
	return v
}

// MustGetConfInt64 配置缺失或转换失败时 panic
func MustGetConfInt64(key string) int64 {
	v, err := GetConfInt64(key)
	if err != nil {
		panic(fmt.Sprintf("MustGetConfInt64(%s) failed: %s", key, err.Error()))
	}
	return v
}

// MustGetConfFloat64 配置缺失或转换失败时 panic
func MustGetConfFloat64(key string) float64 {
	v, err := GetConfFloat64(key)
	if err != nil {
		panic(fmt.Sprintf("MustGetConfFloat64(%s) failed: %s", key, err.Error()))
	}
	return v
}

// MustGetConfBool 配置缺失或转换失败时 panic
func MustGetConfBool(key string) bool {
	v, err := GetConfBool(key)
	if err != nil {
		panic(fmt.Sprintf("MustGetConfBool(%s) failed: %s", key, err.Error()))
	}
	return v
}

// MustGetConfDuration 配置缺失或转换失败时 panic
func MustGetConfDuration(key string) time.Duration {
	v, err := GetConfDuration(key)
	if err != nil {
		panic(fmt.Sprintf("MustGetConfDuration(%s) failed: %s", key, err.Error()))
	}
	return v
}

// MustGetConfTime 配置缺失或转换失败时 panic
func MustGetConfTime(key string) time.Time {
	v, err := GetConfTime(key)
	if err != nil {
		panic(fmt.Sprintf("MustGetConfTime(%s) failed: %s", key, err.Error()))
	}
	return v
}

// =============================================================================
// 存在性检查 / 缓存失效
// =============================================================================

// HasConf 仅判断配置是否存在，不抛错日志
func HasConf(key string) bool {
	_, err := GetConf(key)
	return err == nil
}

// InvalidateConfCache 失效指定配置 key 的本地缓存
// 同时清理 conf-key 与 localConf-key 两层缓存
func InvalidateConfCache(key string) {
	if len(key) == 0 {
		return
	}
	c := NewCache()
	c.Delete("conf-" + key)
	c.Delete("localConf-" + key)
}

// InvalidateConfCacheBatch 批量失效配置缓存
// 通常由远端配置中心在收到推送/检测到差异后批量调用
func InvalidateConfCacheBatch(keys []string) {
	if len(keys) == 0 {
		return
	}
	c := NewCache()
	for _, key := range keys {
		if key == "" {
			continue
		}
		c.Delete("conf-" + key)
		c.Delete("localConf-" + key)
	}
}

// InvalidateAllRequestedRemoteConfCache 失效所有曾被请求过的远端配置 key 缓存
// 当远端 DataId 整体推送变更时调用，确保下次 GetConf 走 fresh 路径
func InvalidateAllRequestedRemoteConfCache() {
	keys := ListRequestedRemoteKeys()
	InvalidateConfCacheBatch(keys)
}

// InvalidateLocalConfFileCache 主动失效本地配置文件的整文件快照缓存
// 适用于运维通过外部工具直接改了 config.json 想立刻让进程感知的场景
// （正常情况下 mtime check 会自动触发刷新，这只是显式入口）
// InvalidateLocalConfFileCache 失效本地配置文件 / 远端快照文件的快照缓存
//
// 适用于运维通过外部工具直接改了 config.json / config.yaml / config.yml 想立刻让进程感知的场景
// （正常情况下 mtime check 会自动触发刷新，这只是显式入口）
func InvalidateLocalConfFileCache() {
	for _, p := range localConfigCandidates() {
		invalidateLocalFileSnapshot(p)
	}
	invalidateLocalFileSnapshot(DefaultRemoteConfigFile)
}
