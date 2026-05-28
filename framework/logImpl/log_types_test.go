// Package logImpl 注释
// @author wanlizhan
// @created 2026/6/1
//
// 覆盖新增的 4 类日志（MqLog / CacheLog / AsyncTaskLog / JobLog）：
//  1. LogType.String() 名称稳定 —— 防止误改影响 ES index 选择
//  2. 实例方法本地写入路径不 panic —— 远程推送已由开关默认关闭，测试只关注本地链路
//  3. *LogBaseModel JSON schema 稳定 —— 字段名是 ES mapping 的契约，重命名等同 break change
package logImpl

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xxzhwl/gaia"
)

func TestLogType_StringNewTypes(t *testing.T) {
	cases := []struct {
		typ  gaia.LogType
		want string
	}{
		{gaia.LogMqType, "MqLog"},
		{gaia.LogCacheType, "CacheLog"},
		{gaia.LogAsyncTaskType, "AsyncTaskLog"},
		{gaia.LogJobType, "JobLog"},
	}
	for _, c := range cases {
		if got := c.typ.String(); got != c.want {
			t.Errorf("LogType(%d).String() = %q, want %q", c.typ, got, c.want)
		}
	}
}

// TestNewLogTypes_LocalWriteNoPanic 验证 4 类新方法的本地链路：
// 调用方应能直接拿到 *DefaultLogger 并完成本地写入，不依赖任何远程服务。
func TestNewLogTypes_LocalWriteNoPanic(t *testing.T) {
	d := NewDefaultLogger().SetTitle("test-newtypes")
	t.Cleanup(d.Stop)

	// 本地写入接口（仅打文件 + stdout）
	d.MqLog(gaia.LogInfoLevel, "mq local")
	d.CacheLog(gaia.LogInfoLevel, "cache local")
	d.AsyncTaskLog(gaia.LogInfoLevel, "async local")
	d.JobLog(gaia.LogInfoLevel, "job local")

	// 远程 Body 接口（远程开关默认关闭，等价 no-op；只验证不 panic + 类型签名正确）
	d.MqLogBody(gaia.LogInfoLevel, "mq body", MqLogBaseModel{
		Backend: "kafka", Direction: "consume", Topic: "test-topic",
	})
	d.CacheLogBody(gaia.LogInfoLevel, "cache body", CacheLogBaseModel{
		Backend: "redis", Op: "get", Key: "u:1",
	})
	d.AsyncTaskLogBody(gaia.LogInfoLevel, "async body", AsyncTaskLogBaseModel{
		TaskName: "send_email", TaskId: "t-1", Phase: "run",
	})
	d.JobLogBody(gaia.LogInfoLevel, "job body", JobLogBaseModel{
		JobName: "daily_clean", CronSpec: "@daily", Phase: "fire",
	})
}

// TestNewLogModels_JSONSchemaStable 锁定 4 类新 BaseModel 的 JSON 字段名，
// 这层是 ES mapping 的契约：任何字段名重命名都应在这里被立刻发现。
func TestNewLogModels_JSONSchemaStable(t *testing.T) {
	type tc struct {
		name        string
		marshal     func() ([]byte, error)
		mustContain []string
	}
	tt := true
	cases := []tc{
		{
			name: "mq",
			marshal: func() ([]byte, error) {
				return json.Marshal(MqLogBaseModel{
					Backend: "kafka", Direction: "produce",
					Topic: "orders", Partition: 3, Offset: 1024,
					Key: "u-1", ConsumerGroup: "cg",
					BodySize: 256, Duration: 12.5,
				})
			},
			mustContain: []string{
				`"backend":"kafka"`, `"direction":"produce"`, `"topic":"orders"`,
				`"partition":3`, `"offset":1024`, `"consumer_group":"cg"`,
				`"body_size":256`, `"duration":12.5`,
			},
		},
		{
			name: "cache",
			marshal: func() ([]byte, error) {
				return json.Marshal(CacheLogBaseModel{
					Backend: "redis", Op: "get", Key: "u:1",
					Hit: &tt, TTL: 600, BodySize: 128, Duration: 1.2,
				})
			},
			mustContain: []string{
				`"backend":"redis"`, `"op":"get"`, `"key":"u:1"`,
				`"hit":true`, `"ttl":600`, `"body_size":128`, `"duration":1.2`,
			},
		},
		{
			name: "async_task",
			marshal: func() ([]byte, error) {
				return json.Marshal(AsyncTaskLogBaseModel{
					TaskName: "send_email", TaskId: "t-1", Phase: "fail",
					RetryCount: 2, MaxRetry: 3, Queue: "default",
					Payload: `{"to":"x"}`, Err: "smtp err",
				})
			},
			mustContain: []string{
				`"task_name":"send_email"`, `"task_id":"t-1"`, `"phase":"fail"`,
				`"retry_count":2`, `"max_retry":3`, `"queue":"default"`,
				`"payload":"{\"to\":\"x\"}"`, `"err":"smtp err"`,
			},
		},
		{
			name: "job",
			marshal: func() ([]byte, error) {
				return json.Marshal(JobLogBaseModel{
					JobName: "daily_clean", CronSpec: "0 0 * * *",
					Phase: "skipped", NextFireTime: "2026-06-02 00:00:00",
				})
			},
			mustContain: []string{
				`"job_name":"daily_clean"`, `"cron_spec":"0 0 * * *"`,
				`"phase":"skipped"`, `"next_fire_time":"2026-06-02 00:00:00"`,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := c.marshal()
			if err != nil {
				t.Fatalf("marshal err: %v", err)
			}
			s := string(b)
			for _, frag := range c.mustContain {
				if !strings.Contains(s, frag) {
					t.Errorf("json missing %q\nfull: %s", frag, s)
				}
			}
		})
	}
}

// TestNewLogIndices 锁定 4 个新 ES index 名（与 mapping 一一对应）。
func TestNewLogIndices(t *testing.T) {
	cases := map[string]string{
		InLogIndex:        "in_log",
		OutLogIndex:       "out_log",
		MqLogIndex:        "mq_log",
		CacheLogIndex:     "cache_log",
		AsyncTaskLogIndex: "async_task_log",
		JobLogIndex:       "job_log",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("index name = %q, want %q", got, want)
		}
	}
}

func TestAccessLogModel_JSONSchemaStable(t *testing.T) {
	body := AccessLogBaseModel{
		Direction:      AccessDirectionInbound,
		Protocol:       AccessProtocolGRPC,
		Operation:      "/demo.UserService/Get",
		Peer:           "127.0.0.1:9000",
		Status:         "OK",
		StatusCode:     0,
		Success:        true,
		ReqHeader:      map[string][]string{"authorization": {"[REDACTED]"}},
		ReqBody:        `{"id":"1"}`,
		RespBody:       `{"name":"alice"}`,
		StartTime:      "2026-06-27 10:00:00.000",
		EndTime:        "2026-06-27 10:00:00.012",
		StartTimeStamp: 1782535200000,
		EndTimeStamp:   1782535200012,
		Duration:       12,
		GRPC: &GrpcAccessLogFields{
			FullMethod: "/demo.UserService/Get",
			Service:    "demo.UserService",
			Method:     "Get",
			Kind:       "unary",
			Code:       "OK",
		},
	}

	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal err: %v", err)
	}
	s := string(b)
	for _, frag := range []string{
		`"direction":"inbound"`,
		`"protocol":"grpc"`,
		`"operation":"/demo.UserService/Get"`,
		`"status":"OK"`,
		`"success":true`,
		`"req_header":{"authorization":["[REDACTED]"]}`,
		`"grpc":{"full_method":"/demo.UserService/Get","service":"demo.UserService","method":"Get","kind":"unary","code":"OK"}`,
	} {
		if !strings.Contains(s, frag) {
			t.Errorf("json missing %q\nfull: %s", frag, s)
		}
	}
}

func TestAccessLogRemoteRouting_UsesDirectionIndexes(t *testing.T) {
	d := NewDefaultLogger().SetTitle("test-access-routing")
	t.Cleanup(d.Stop)

	inDoc := d.buildRemoteLogDoc(createRemoteLogDocArg{
		logType:  gaia.LogInType,
		docBody:  NewHTTPAccessLog(AccessDirectionInbound, HttpLogModel{Url: "/users", HttpMethod: "GET", HttpStatusCode: 200}),
		logLevel: gaia.LogInfoLevel,
		content:  "inbound http",
	})
	if inDoc.logIndex != InLogIndex {
		t.Fatalf("inbound index = %q, want %q", inDoc.logIndex, InLogIndex)
	}
	inBody, ok := inDoc.docBody.(AccessLogModel)
	if !ok {
		t.Fatalf("inbound doc type = %T, want AccessLogModel", inDoc.docBody)
	}
	if inBody.Direction != AccessDirectionInbound || inBody.Protocol != AccessProtocolHTTP || inBody.Operation != "/users" {
		t.Fatalf("bad inbound access fields: %+v", inBody.AccessLogBaseModel)
	}

	outDoc := d.buildRemoteLogDoc(createRemoteLogDocArg{
		logType: gaia.LogOutType,
		docBody: NewGRPCAccessLog(AccessDirectionOutbound, GrpcLogBaseModel{
			FullMethod: "/demo.UserService/Get",
			Kind:       "unary",
			Code:       "Unavailable",
			Err:        "unavailable",
		}),
		logLevel: gaia.LogErrorLevel,
		content:  "outbound grpc",
	})
	if outDoc.logIndex != OutLogIndex {
		t.Fatalf("outbound index = %q, want %q", outDoc.logIndex, OutLogIndex)
	}
	outBody, ok := outDoc.docBody.(AccessLogModel)
	if !ok {
		t.Fatalf("outbound doc type = %T, want AccessLogModel", outDoc.docBody)
	}
	if outBody.Direction != AccessDirectionOutbound || outBody.Protocol != AccessProtocolGRPC || outBody.Operation != "/demo.UserService/Get" {
		t.Fatalf("bad outbound access fields: %+v", outBody.AccessLogBaseModel)
	}
}
