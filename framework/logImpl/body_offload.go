package logImpl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	storageiface "github.com/xxzhwl/gaia/components/storage"
	storagecos "github.com/xxzhwl/gaia/components/storage/cos"
	storageoss "github.com/xxzhwl/gaia/components/storage/oss"
	storages3 "github.com/xxzhwl/gaia/components/storage/s3"
)

const (
	defaultBodyOffloadProvider      = "auto"
	defaultBodyOffloadThreshold     = int64(4096)
	defaultBodyOffloadPrefix        = "gaia/logs/offload"
	defaultBodyOffloadSignURLExpire = 24 * time.Hour
	defaultBodyOffloadTimeout       = 5 * time.Second
	bodyOffloadRetryInterval        = 30 * time.Second
)

type bodyOffloadConfig struct {
	Enabled       bool
	Provider      string
	Threshold     int64
	Prefix        string
	SignURLExpire time.Duration
	Timeout       time.Duration
}

type bodyOffloadStoreCache struct {
	mu         sync.Mutex
	provider   string
	store      storageiface.ObjectStore
	lastErr    error
	lastErrAt  time.Time
	lastLoaded time.Time
}

type offloadedFieldMeta struct {
	Key    string
	URL    string
	Size   int64
	SHA256 string
}

var remoteBodyOffloadStore bodyOffloadStoreCache
var newBodyOffloadStore = buildBodyOffloadStore

// RemoteLogBodyFromBytes prepares a request/response body for remote log documents.
func RemoteLogBodyFromBytes(logBody bool, maxBytes int64, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	return RemoteLogBodyFromString(logBody, maxBytes, string(body))
}

// RemoteLogBodyFromString prepares a serialized payload for remote log documents.
func RemoteLogBodyFromString(logBody bool, maxBytes int64, body string) string {
	if body == "" {
		return ""
	}
	if !logBody {
		return fmt.Sprintf("[REDACTED len=%d]", len(body))
	}
	if loadBodyOffloadConfig().Enabled {
		return body
	}
	return truncateLogField(body, normalizeBodyLogLimit(maxBytes))
}

func offloadLargeRemoteLogFields(logType string, doc any) any {
	cfg := loadBodyOffloadConfig()
	if !cfg.Enabled {
		return doc
	}

	switch v := doc.(type) {
	case LogModel:
		offloadLogModelFields(cfg, logType, &v)
		return v
	case AccessLogModel:
		offloadLogModelFields(cfg, logType, &v.LogModel)
		offloadAccessLogFields(cfg, logType, v.LogModel.LogId, &v.AccessLogBaseModel)
		return v
	case DbLoggerModel:
		offloadLogModelFields(cfg, logType, &v.LogModel)
		return v
	case MqLogModel:
		offloadLogModelFields(cfg, logType, &v.LogModel)
		return v
	case CacheLogModel:
		offloadLogModelFields(cfg, logType, &v.LogModel)
		return v
	case AsyncTaskLogModel:
		offloadLogModelFields(cfg, logType, &v.LogModel)
		return v
	case JobLogModel:
		offloadLogModelFields(cfg, logType, &v.LogModel)
		return v
	default:
		return doc
	}
}

func offloadLogModelFields(cfg bodyOffloadConfig, logType string, body *LogModel) {
	if int64(len(body.Content)) <= cfg.Threshold {
		return
	}
	meta, ok := offloadRemoteLogField(cfg, logType, body.LogId, "content", body.Content)
	if !ok {
		body.Content = truncateLogField(body.Content, cfg.Threshold)
		return
	}
	body.Content = offloadPlaceholder("content", meta)
	body.ContentObjectKey = meta.Key
	body.ContentObjectURL = meta.URL
	body.ContentObjectSize = meta.Size
	body.ContentObjectSHA256 = meta.SHA256
	body.ContentOffloaded = true
}

func offloadAccessLogFields(cfg bodyOffloadConfig, logType, logID string, body *AccessLogBaseModel) {
	if int64(len(body.ReqBody)) > cfg.Threshold {
		meta, ok := offloadRemoteLogField(cfg, logType, logID, "req_body", body.ReqBody)
		if !ok {
			body.ReqBody = truncateLogField(body.ReqBody, cfg.Threshold)
		} else {
			body.ReqBody = offloadPlaceholder("req_body", meta)
			body.ReqBodyObjectKey = meta.Key
			body.ReqBodyObjectURL = meta.URL
			body.ReqBodyObjectSize = meta.Size
			body.ReqBodyObjectSHA256 = meta.SHA256
			body.ReqBodyOffloaded = true
		}
	}
	if int64(len(body.RespBody)) > cfg.Threshold {
		meta, ok := offloadRemoteLogField(cfg, logType, logID, "resp_body", body.RespBody)
		if !ok {
			body.RespBody = truncateLogField(body.RespBody, cfg.Threshold)
		} else {
			body.RespBody = offloadPlaceholder("resp_body", meta)
			body.RespBodyObjectKey = meta.Key
			body.RespBodyObjectURL = meta.URL
			body.RespBodyObjectSize = meta.Size
			body.RespBodyObjectSHA256 = meta.SHA256
			body.RespBodyOffloaded = true
		}
	}
}

func offloadRemoteLogField(cfg bodyOffloadConfig, logType, logID, field, value string) (offloadedFieldMeta, bool) {
	if int64(len(value)) <= cfg.Threshold {
		return offloadedFieldMeta{}, false
	}

	sum := sha256.Sum256([]byte(value))
	sha := hex.EncodeToString(sum[:])
	key := buildBodyOffloadKey(cfg, logType, logID, field, sha)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	store, err := getBodyOffloadStore(cfg.Provider)
	if err != nil {
		gaia.Println(gaia.LogWarnLevel, fmt.Sprintf("[Logger] body offload 获取对象存储失败 provider=%s field=%s: %v", cfg.Provider, field, err))
		return offloadedFieldMeta{}, false
	}
	if err := store.Put(ctx, key, []byte(value)); err != nil {
		gaia.Println(gaia.LogWarnLevel, fmt.Sprintf("[Logger] body offload 上传对象失败 provider=%s field=%s key=%s: %v", cfg.Provider, field, key, err))
		return offloadedFieldMeta{}, false
	}

	url, err := store.SignURL(ctx, key, cfg.SignURLExpire)
	if err != nil {
		gaia.Println(gaia.LogWarnLevel, fmt.Sprintf("[Logger] body offload 生成预签名URL失败 provider=%s field=%s key=%s: %v", cfg.Provider, field, key, err))
	}
	return offloadedFieldMeta{
		Key:    key,
		URL:    url,
		Size:   int64(len(value)),
		SHA256: sha,
	}, true
}

func offloadPlaceholder(field string, meta offloadedFieldMeta) string {
	if meta.URL == "" {
		return fmt.Sprintf("[offloaded field=%s len=%d sha256=%s key=%s]", field, meta.Size, meta.SHA256, meta.Key)
	}
	return fmt.Sprintf("[offloaded field=%s len=%d sha256=%s url=%s]", field, meta.Size, meta.SHA256, meta.URL)
}

func buildBodyOffloadKey(cfg bodyOffloadConfig, logType, logID, field, sha string) string {
	prefix := strings.Trim(cfg.Prefix, "/")
	if prefix == "" {
		prefix = defaultBodyOffloadPrefix
	}
	if logID == "" {
		logID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	shortSHA := sha
	if len(shortSHA) > 16 {
		shortSHA = shortSHA[:16]
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s-%s.txt",
		prefix,
		time.Now().Format("2006/01/02"),
		sanitizeObjectKeyPart(logType),
		sanitizeObjectKeyPart(logID),
		sanitizeObjectKeyPart(field),
		shortSHA,
	)
}

func sanitizeObjectKeyPart(value string) string {
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func truncateLogField(value string, maxBytes int64) string {
	if int64(len(value)) <= maxBytes {
		return value
	}
	return fmt.Sprintf("%s...[truncated %d bytes]", value[:int(maxBytes)], int64(len(value))-maxBytes)
}

func normalizeBodyLogLimit(maxBytes int64) int64 {
	if maxBytes <= 0 {
		return defaultBodyOffloadThreshold
	}
	return maxBytes
}

func loadBodyOffloadConfig() bodyOffloadConfig {
	provider := strings.ToLower(strings.TrimSpace(gaia.GetSafeConfStringWithDefault("Logger.BodyOffload.Provider", defaultBodyOffloadProvider)))
	if provider == "" {
		provider = defaultBodyOffloadProvider
	}

	threshold := gaia.GetSafeConfInt64WithDefault("Logger.BodyOffload.ThresholdBytes", defaultBodyOffloadThreshold)
	if threshold <= 0 {
		threshold = defaultBodyOffloadThreshold
	}

	expireSec := gaia.GetSafeConfInt64WithDefault("Logger.BodyOffload.SignURLExpireSec", int64(defaultBodyOffloadSignURLExpire/time.Second))
	if expireSec <= 0 {
		expireSec = int64(defaultBodyOffloadSignURLExpire / time.Second)
	}

	timeoutSec := gaia.GetSafeConfInt64WithDefault("Logger.BodyOffload.UploadTimeoutSec", int64(defaultBodyOffloadTimeout/time.Second))
	if timeoutSec <= 0 {
		timeoutSec = int64(defaultBodyOffloadTimeout / time.Second)
	}

	prefix := strings.TrimSpace(gaia.GetSafeConfStringWithDefault("Logger.BodyOffload.Prefix", defaultBodyOffloadPrefix))
	if prefix == "" {
		prefix = defaultBodyOffloadPrefix
	}

	return bodyOffloadConfig{
		Enabled:       getBodyOffloadEnabled(),
		Provider:      provider,
		Threshold:     threshold,
		Prefix:        prefix,
		SignURLExpire: time.Duration(expireSec) * time.Second,
		Timeout:       time.Duration(timeoutSec) * time.Second,
	}
}

func getBodyOffloadEnabled() bool {
	v, err := gaia.GetConf("Logger.BodyOffload.Enabled")
	if err != nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off", "":
			return false
		default:
			return true
		}
	default:
		return gaia.GetSafeConfBool("Logger.BodyOffload.Enabled")
	}
}

func getBodyOffloadStore(provider string) (storageiface.ObjectStore, error) {
	remoteBodyOffloadStore.mu.Lock()
	defer remoteBodyOffloadStore.mu.Unlock()

	if remoteBodyOffloadStore.store != nil && remoteBodyOffloadStore.provider == provider {
		return remoteBodyOffloadStore.store, nil
	}
	if remoteBodyOffloadStore.lastErr != nil &&
		remoteBodyOffloadStore.provider == provider &&
		time.Since(remoteBodyOffloadStore.lastErrAt) < bodyOffloadRetryInterval {
		return nil, remoteBodyOffloadStore.lastErr
	}

	store, err := newBodyOffloadStore(provider)
	if err != nil {
		remoteBodyOffloadStore.provider = provider
		remoteBodyOffloadStore.lastErr = err
		remoteBodyOffloadStore.lastErrAt = time.Now()
		return nil, err
	}

	remoteBodyOffloadStore.provider = provider
	remoteBodyOffloadStore.store = store
	remoteBodyOffloadStore.lastErr = nil
	remoteBodyOffloadStore.lastLoaded = time.Now()
	return store, nil
}

func buildBodyOffloadStore(provider string) (storageiface.ObjectStore, error) {
	switch provider {
	case "", "auto":
		return buildAutoBodyOffloadStore()
	case "cos":
		return buildCOSBodyOffloadStore()
	case "oss":
		return buildOSSBodyOffloadStore()
	case "s3":
		return buildS3BodyOffloadStore()
	default:
		return nil, fmt.Errorf("unknown Logger.BodyOffload.Provider: %s", provider)
	}
}

func buildAutoBodyOffloadStore() (storageiface.ObjectStore, error) {
	if frameworkCOSConfigured() {
		if store, err := buildCOSBodyOffloadStore(); err == nil {
			return store, nil
		}
	}
	if frameworkOSSConfigured() {
		if store, err := buildOSSBodyOffloadStore(); err == nil {
			return store, nil
		}
	}
	if frameworkS3Configured() {
		if store, err := buildS3BodyOffloadStore(); err == nil {
			return store, nil
		}
	}
	return nil, fmt.Errorf("no configured Framework.Cos, Framework.OSS, or Framework.S3 object store")
}

func buildCOSBodyOffloadStore() (storageiface.ObjectStore, error) {
	cli, err := storagecos.NewFrameworkClient()
	if err != nil {
		return nil, err
	}
	return storagecos.NewAdapter(cli), nil
}

func buildOSSBodyOffloadStore() (storageiface.ObjectStore, error) {
	cli, err := storageoss.NewFrameworkClient()
	if err != nil {
		return nil, err
	}
	return storageoss.NewAdapter(cli), nil
}

func buildS3BodyOffloadStore() (storageiface.ObjectStore, error) {
	cli, err := storages3.NewFrameworkClient()
	if err != nil {
		return nil, err
	}
	return storages3.NewAdapter(cli), nil
}

func frameworkCOSConfigured() bool {
	return gaia.GetSafeConfString("Framework.Cos.Bucket") != "" &&
		gaia.GetSafeConfString("Framework.Cos.SecretID") != "" &&
		gaia.GetSafeConfString("Framework.Cos.SecretKey") != ""
}

func frameworkOSSConfigured() bool {
	return gaia.GetSafeConfString("Framework.OSS.Bucket") != "" &&
		gaia.GetSafeConfString("Framework.OSS.Endpoint") != "" &&
		gaia.GetSafeConfString("Framework.OSS.AccessKeyID") != "" &&
		gaia.GetSafeConfString("Framework.OSS.AccessKeySecret") != ""
}

func frameworkS3Configured() bool {
	return gaia.GetSafeConfString("Framework.S3.Bucket") != "" &&
		gaia.GetSafeConfString("Framework.S3.Region") != ""
}
