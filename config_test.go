// Package gaia config_test 配置相关方法的单元测试
// @author wanlizhan
// @created 2026-05-28
package gaia

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// setupTempLocalConfig 写一份临时 JSON 本地配置文件，并把全局变量
// （指向该文件的常量是 const，所以这里直接用一个独立的 helper 写到默认路径并在收尾时还原）
// 注意：因为 DefaultLocalConfigFile 是 const，无法重赋值；为避免污染真实工程目录下的
// configs/local/config.json，本函数会把已有文件备份，最后通过 cleanup 还原。
func setupTempLocalConfig(t *testing.T, conf map[string]any) {
	t.Helper()

	// 准备目录
	if err := os.MkdirAll(DefaultLocalConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	target := DefaultLocalConfigFile

	// 备份原文件（若存在）
	var backup []byte
	hadOrigin := false
	if b, err := os.ReadFile(target); err == nil {
		backup = b
		hadOrigin = true
	}

	// 写入新内容
	data, err := json.Marshal(conf)
	if err != nil {
		t.Fatalf("marshal conf: %v", err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}

	t.Cleanup(func() {
		if hadOrigin {
			_ = os.WriteFile(target, backup, 0o644)
		} else {
			_ = os.Remove(target)
		}
		// 清理本测试期间所有可能写入的缓存项
		// 通过将常用的测试 key 做一次 invalidate；缓存基于 key 隔离，
		// 测试间彼此使用不同的 key 前缀以避免相互干扰。
	})
}

// invalidateAll 在每个子用例间清理传入 keys 的缓存
func invalidateAll(keys ...string) {
	for _, k := range keys {
		InvalidateConfCache(k)
	}
}

// ---------- 基础类型 ----------

func TestGetConfInt(t *testing.T) {
	keys := []string{"test_int_a", "test_int_b", "test_int_c"}
	setupTempLocalConfig(t, map[string]any{
		"test_int_a": 42,
		"test_int_b": "100",
		// test_int_c 不存在
	})
	invalidateAll(keys...)

	if got := GetSafeConfInt("test_int_a"); got != 42 {
		t.Errorf("GetSafeConfInt(a) = %d, want 42", got)
	}
	if got := GetSafeConfInt("test_int_b"); got != 100 {
		t.Errorf("GetSafeConfInt(b) = %d, want 100", got)
	}
	if got := GetSafeConfIntWithDefault("test_int_c", 7); got != 7 {
		t.Errorf("GetSafeConfIntWithDefault(c, 7) = %d, want 7", got)
	}

	if _, err := GetConfInt("test_int_c"); err == nil {
		t.Errorf("GetConfInt(c) want error, got nil")
	}
}

func TestGetConfUint64(t *testing.T) {
	keys := []string{"u_pos", "u_neg", "u_str"}
	setupTempLocalConfig(t, map[string]any{
		"u_pos": 12345,
		"u_neg": -1,
		"u_str": "98765",
	})
	invalidateAll(keys...)

	if got := GetSafeConfUint64("u_pos"); got != 12345 {
		t.Errorf("u_pos = %d, want 12345", got)
	}
	if got := GetSafeConfUint64("u_str"); got != 98765 {
		t.Errorf("u_str = %d, want 98765", got)
	}
	// 负数应当报错
	if _, err := GetConfUint64("u_neg"); err == nil {
		t.Errorf("u_neg want error, got nil")
	}
	if got := GetSafeConfUint64WithDefault("u_neg", 9); got != 9 {
		t.Errorf("u_neg with default = %d, want 9", got)
	}
}

// ---------- 时间相关 ----------

func TestGetConfTime(t *testing.T) {
	keys := []string{"t_str", "t_rfc", "t_ts_sec", "t_ts_ms", "t_layout"}
	setupTempLocalConfig(t, map[string]any{
		"t_str":    "2024-03-15 10:30:00",
		"t_rfc":    "2024-03-15T10:30:00Z",
		"t_ts_sec": 1710501000, // 2024-03-15 ~UTC
		"t_ts_ms":  int64(1710501000123),
		"t_layout": "15/03/2024",
	})
	invalidateAll(keys...)

	tt, err := GetConfTime("t_str")
	if err != nil {
		t.Fatalf("t_str err: %v", err)
	}
	if tt.Year() != 2024 || tt.Month() != 3 || tt.Day() != 15 || tt.Hour() != 10 {
		t.Errorf("t_str parsed = %v", tt)
	}

	tt, err = GetConfTime("t_rfc")
	if err != nil {
		t.Fatalf("t_rfc err: %v", err)
	}
	if tt.UTC().Year() != 2024 {
		t.Errorf("t_rfc parsed = %v", tt)
	}

	tt, err = GetConfTime("t_ts_sec")
	if err != nil {
		t.Fatalf("t_ts_sec err: %v", err)
	}
	if tt.Unix() != 1710501000 {
		t.Errorf("t_ts_sec unix = %d, want 1710501000", tt.Unix())
	}

	tt, err = GetConfTime("t_ts_ms")
	if err != nil {
		t.Fatalf("t_ts_ms err: %v", err)
	}
	if tt.UnixMilli() != 1710501000123 {
		t.Errorf("t_ts_ms unixmilli = %d, want 1710501000123", tt.UnixMilli())
	}

	// 自定义布局
	tt, err = GetConfTimeWithLayout("t_layout", "02/01/2006")
	if err != nil {
		t.Fatalf("t_layout err: %v", err)
	}
	if tt.Year() != 2024 || tt.Month() != 3 || tt.Day() != 15 {
		t.Errorf("t_layout parsed = %v", tt)
	}

	// 默认值兜底
	def := time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local)
	if got := GetSafeConfTimeWithDefault("__not_exist__", def); !got.Equal(def) {
		t.Errorf("default time mismatch: %v", got)
	}
}

func TestGetConfDuration(t *testing.T) {
	keys := []string{"d_str", "d_complex", "d_num", "d_float"}
	setupTempLocalConfig(t, map[string]any{
		"d_str":     "200ms",
		"d_complex": "1h30m",
		"d_num":     500,    // 数字按毫秒
		"d_float":   1500.5, // 1500ms
	})
	invalidateAll(keys...)

	if got, err := GetConfDuration("d_str"); err != nil || got != 200*time.Millisecond {
		t.Errorf("d_str = %v, err = %v", got, err)
	}
	if got, err := GetConfDuration("d_complex"); err != nil || got != time.Hour+30*time.Minute {
		t.Errorf("d_complex = %v, err = %v", got, err)
	}
	if got, err := GetConfDuration("d_num"); err != nil || got != 500*time.Millisecond {
		t.Errorf("d_num = %v, err = %v", got, err)
	}
	if got, err := GetConfDuration("d_float"); err != nil || got != 1500*time.Millisecond {
		t.Errorf("d_float = %v, err = %v", got, err)
	}

	// 默认值
	if got := GetSafeConfDurationWithDefault("__no_dur__", 5*time.Second); got != 5*time.Second {
		t.Errorf("default duration = %v", got)
	}
}

// ---------- Map ----------

func TestGetConfMap(t *testing.T) {
	keys := []string{"m_obj", "m_int_obj"}
	setupTempLocalConfig(t, map[string]any{
		"m_obj": map[string]any{
			"name": "alice",
			"age":  18,
		},
		"m_int_obj": map[string]any{
			"a": 1,
			"b": 2,
			"c": 3,
		},
	})
	invalidateAll(keys...)

	got, err := GetConfMap("m_obj")
	if err != nil {
		t.Fatalf("GetConfMap err: %v", err)
	}
	if got["name"] != "alice" {
		t.Errorf("m_obj.name = %v, want alice", got["name"])
	}

	// 泛型 map[string]int
	mInt, err := GetConfMapT[int]("m_int_obj")
	if err != nil {
		t.Fatalf("GetConfMapT err: %v", err)
	}
	if mInt["a"] != 1 || mInt["b"] != 2 || mInt["c"] != 3 {
		t.Errorf("GetConfMapT[int] = %v", mInt)
	}

	// 默认值
	def := map[string]any{"d": "default"}
	if got := GetSafeConfMapWithDefault("__no_map__", def); !reflect.DeepEqual(got, def) {
		t.Errorf("default map = %v", got)
	}
}

// ---------- 字节大小 ----------

func TestGetConfByteSize(t *testing.T) {
	keys := []string{"b_str_kb", "b_str_mb", "b_str_gib", "b_num", "b_float"}
	setupTempLocalConfig(t, map[string]any{
		"b_str_kb":  "10KB",
		"b_str_mb":  "5MB",
		"b_str_gib": "1.5GiB",
		"b_num":     2048,
		"b_float":   1024.0,
	})
	invalidateAll(keys...)

	cases := []struct {
		key  string
		want int64
	}{
		{"b_str_kb", 10 * 1024},
		{"b_str_mb", 5 * 1024 * 1024},
		{"b_str_gib", int64(1.5 * float64(1<<30))},
		{"b_num", 2048},
		{"b_float", 1024},
	}
	for _, c := range cases {
		got, err := GetConfByteSize(c.key)
		if err != nil {
			t.Errorf("GetConfByteSize(%s) err: %v", c.key, err)
			continue
		}
		if got != c.want {
			t.Errorf("GetConfByteSize(%s) = %d, want %d", c.key, got, c.want)
		}
	}

	if got := GetSafeConfByteSizeWithDefault("__no_bs__", 999); got != 999 {
		t.Errorf("default byte size = %d", got)
	}
}

// ---------- Slice WithDefault ----------

func TestGetSafeConfSliceWithDefault(t *testing.T) {
	keys := []string{"s_exist", "s_missing"}
	setupTempLocalConfig(t, map[string]any{
		"s_exist": []any{1, 2, 3},
	})
	invalidateAll(keys...)

	got := GetSafeConfSliceWithDefault[int]("s_exist", []int{9})
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Errorf("s_exist = %v", got)
	}
	got = GetSafeConfSliceWithDefault[int]("s_missing", []int{9})
	if !reflect.DeepEqual(got, []int{9}) {
		t.Errorf("s_missing fallback = %v", got)
	}
}

// ---------- Must 系列 ----------

func TestMustSeriesPanicWhenMissing(t *testing.T) {
	setupTempLocalConfig(t, map[string]any{
		"must_str": "hello",
		"must_int": 7,
	})
	invalidateAll("must_str", "must_int", "must_missing")

	if got := MustGetConfString("must_str"); got != "hello" {
		t.Errorf("MustGetConfString = %s", got)
	}
	if got := MustGetConfInt("must_int"); got != 7 {
		t.Errorf("MustGetConfInt = %d", got)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustGetConfString missing should panic")
		}
	}()
	MustGetConfString("must_missing")
}

func TestMustGetConfDurationOK(t *testing.T) {
	setupTempLocalConfig(t, map[string]any{
		"must_dur": "3s",
	})
	invalidateAll("must_dur")

	if got := MustGetConfDuration("must_dur"); got != 3*time.Second {
		t.Errorf("MustGetConfDuration = %v", got)
	}
}

// ---------- HasConf / InvalidateConfCache ----------

func TestHasConfAndInvalidate(t *testing.T) {
	setupTempLocalConfig(t, map[string]any{
		"hc_a": "v1",
	})
	invalidateAll("hc_a", "hc_b")

	if !HasConf("hc_a") {
		t.Errorf("HasConf(hc_a) = false, want true")
	}
	if HasConf("hc_b") {
		t.Errorf("HasConf(hc_b) = true, want false")
	}

	// 修改文件内容并清缓存后，应能读到新值
	newConf := map[string]any{"hc_a": "v2"}
	data, _ := json.Marshal(newConf)
	if err := os.WriteFile(DefaultLocalConfigFile, data, 0o644); err != nil {
		t.Fatalf("rewrite conf: %v", err)
	}
	InvalidateConfCache("hc_a")
	if got := GetSafeConfString("hc_a"); got != "v2" {
		t.Errorf("after invalidate = %s, want v2", got)
	}
}

// ---------- BindConfig 增强：time.Time / time.Duration / Map / 指针 ----------

type bindCfg struct {
	Name    string            `config:"bc_name"`
	Timeout time.Duration     `config:"bc_timeout"`
	StartAt time.Time         `config:"bc_start"`
	Tags    map[string]string `config:"bc_tags"`
	Port    *int              `config:"bc_port"`
	Nested  struct {
		Level int `config:"bc_level"`
	}
}

func TestBindConfigEnhanced(t *testing.T) {
	setupTempLocalConfig(t, map[string]any{
		"bc_name":    "svc-a",
		"bc_timeout": "500ms",
		"bc_start":   "2024-03-15 10:00:00",
		"bc_tags": map[string]any{
			"env":  "prod",
			"role": "api",
		},
		"bc_port":  8080,
		"bc_level": 3,
	})
	invalidateAll("bc_name", "bc_timeout", "bc_start", "bc_tags", "bc_port", "bc_level")

	var c bindCfg
	if err := BindConfigWithErr(&c); err != nil {
		t.Fatalf("BindConfigWithErr: %v", err)
	}
	if c.Name != "svc-a" {
		t.Errorf("Name = %s", c.Name)
	}
	if c.Timeout != 500*time.Millisecond {
		t.Errorf("Timeout = %v", c.Timeout)
	}
	if c.StartAt.IsZero() {
		t.Errorf("StartAt should not be zero")
	}
	if c.Tags["env"] != "prod" || c.Tags["role"] != "api" {
		t.Errorf("Tags = %v", c.Tags)
	}
	if c.Port == nil || *c.Port != 8080 {
		t.Errorf("Port = %v", c.Port)
	}
	if c.Nested.Level != 3 {
		t.Errorf("Nested.Level = %d", c.Nested.Level)
	}
}

// ---------- 路径相关：确保 invalidate 在 yaml/json 路径都生效 ----------

func TestInvalidateClearsLocalCache(t *testing.T) {
	setupTempLocalConfig(t, map[string]any{
		"cl_a": "first",
	})
	invalidateAll("cl_a")

	// 第一次读取，进入 GetConf 缓存
	if got := GetSafeConfString("cl_a"); got != "first" {
		t.Fatalf("first read = %s", got)
	}

	// 再走一次 GetConfFromLocalFile，进入 localConf-cl_a 缓存
	if v, ok, err := GetConfFromLocalFile("cl_a"); err != nil || !ok || v.(string) != "first" {
		t.Fatalf("GetConfFromLocalFile first = %v %v %v", v, ok, err)
	}

	// 修改文件内容
	newConf := map[string]any{"cl_a": "second"}
	data, _ := json.Marshal(newConf)
	if err := os.WriteFile(filepath.Clean(DefaultLocalConfigFile), data, 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	// 不清缓存，仍然返回旧值
	if got := GetSafeConfString("cl_a"); got != "first" {
		t.Errorf("before invalidate = %s, want first (cache hit)", got)
	}

	// 清掉两层缓存后，应返回新值
	InvalidateConfCache("cl_a")
	if got := GetSafeConfString("cl_a"); got != "second" {
		t.Errorf("after invalidate = %s, want second", got)
	}
}

func setupTempRemoteSnapshot(t *testing.T, conf map[string]any) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(DefaultRemoteConfigFile), 0o755); err != nil {
		t.Fatalf("mkdir remote config dir: %v", err)
	}
	target := DefaultRemoteConfigFile

	var backup []byte
	hadOrigin := false
	if b, err := os.ReadFile(target); err == nil {
		backup = b
		hadOrigin = true
	}

	data, err := json.Marshal(conf)
	if err != nil {
		t.Fatalf("marshal remote conf: %v", err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("write remote conf: %v", err)
	}
	invalidateLocalFileSnapshot(DefaultRemoteConfigFile)

	t.Cleanup(func() {
		if hadOrigin {
			_ = os.WriteFile(target, backup, 0o644)
		} else {
			_ = os.Remove(target)
		}
		invalidateLocalFileSnapshot(DefaultRemoteConfigFile)
		remoteConfMapLocker.Lock()
		remoteConfMap = map[string]any{}
		remoteConfMapLocker.Unlock()
		RemoteSnapshotOwned = false
	})
}

func TestRemoteSnapshotExactKeyFallback(t *testing.T) {
	setupTempRemoteSnapshot(t, map[string]any{
		"flat.key": "flat-value",
		"nested": map[string]any{
			"key": "nested-value",
		},
	})

	if got, ok, err := getConfFromLocalFile(DefaultRemoteConfigFile, "flat.key"); err != nil || !ok || got != "flat-value" {
		t.Fatalf("flat exact key = %v, %v, %v", got, ok, err)
	}
	if got, ok, err := getConfFromLocalFile(DefaultRemoteConfigFile, "nested.key"); err != nil || !ok || got != "nested-value" {
		t.Fatalf("nested dot path = %v, %v, %v", got, ok, err)
	}
}

func TestPersistRemoteConfToFilePreservesExistingSnapshot(t *testing.T) {
	setupTempRemoteSnapshot(t, map[string]any{
		"old.key": "old-value",
	})

	persistRemoteConfToFile("new.key", "new-value")

	if got, ok, err := getConfFromLocalFile(DefaultRemoteConfigFile, "old.key"); err != nil || !ok || got != "old-value" {
		t.Fatalf("old snapshot key = %v, %v, %v", got, ok, err)
	}
	if got, ok, err := getConfFromLocalFile(DefaultRemoteConfigFile, "new.key"); err != nil || !ok || got != "new-value" {
		t.Fatalf("new snapshot key = %v, %v, %v", got, ok, err)
	}
}

func TestGetConfFromRemoteConfCenterTimeout(t *testing.T) {
	oldRemote := GetConfFromRemote
	GetConfFromRemote = func(key string) (any, bool, error) {
		time.Sleep(50 * time.Millisecond)
		return "late-value", true, nil
	}
	t.Cleanup(func() {
		GetConfFromRemote = oldRemote
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	got, existed, err := GetConfFromRemoteConfCenter(ctx, "slow.remote.key")
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if existed || got != nil {
		t.Fatalf("timeout result = %v, %v, want nil false", got, existed)
	}

	time.Sleep(60 * time.Millisecond)
}
