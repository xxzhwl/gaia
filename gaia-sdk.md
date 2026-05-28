---
name: gaia-sdk
description: Use this skill whenever developing Go backend services with the Gaia SDK (github.com/xxzhwl/gaia) — an enterprise-grade, modular Go framework. Triggers include: any mention of "gaia", "Gaia SDK", creating HTTP services, data validation, async tasks, cron jobs, database operations, Redis caching, file storage, message notifications, OAuth account systems, proxy pattern, or any Go backend development involving the gaia package. Always use this skill when the user asks to build, extend, or fix anything related to gaia-based projects.
---

# Gaia SDK Development Skill

This skill provides comprehensive guidance for developing Go backend services using the Gaia SDK (`github.com/xxzhwl/gaia`), an enterprise-grade modular framework authored by wanlizhan. It covers the full API surface, coding conventions, architectural patterns, and component integration.

## Core Philosophy

Gaia SDK is a convention-over-configuration framework. Key principles:

1. **Package-level functions** — most utilities are exposed as package-level functions in the `gaia` root package, not methods on structs. Import `"github.com/xxzhwl/gaia"` and call directly.
2. **Chinese comments** — all code comments and user-facing error messages are in Chinese. Respect this convention when writing new code.
3. **Generics everywhere** — list/slice utilities use Go generics extensively for type safety.
4. **Proxy pattern** — services are registered via `RegisterProxy(class, service, impl)` and retrieved via `GetProxy(class, service)` — an in-memory service locator.
5. **Init-then-use** — the framework must be initialized via `framework.Init()` (or the init/base.go package) before any gaia features work.
6. **Schema-based config** — configuration is organized by "schemas" (e.g., "Server", "AsyncTask", "Framework") that map to JSON config sections.
7. **Struct-tag-driven validation** — request validation is declared via Go struct tags (`require`, `validator`, `range`, `gt`, `lt`, `length`).

---

## Project Structure Convention

```
project/
├── main.go
├── configs/local/config.json    # Local configuration
├── internal/
│   ├── handler/                  # HTTP request handlers
│   ├── service/                  # Business logic services
│   ├── model/                    # Data models / DAO
│   └── task/                     # Async/cron task definitions
├── go.mod
└── go.sum
```

Typical `main.go`:
```go
package main

import (
    "github.com/xxzhwl/gaia/framework"
    "github.com/xxzhwl/gaia/framework/server"
)

func init() {
    framework.Init()
}

func main() {
    s := server.DefaultApp()
    s.RegisterCommonHandler()
    // register custom routes...
    s.Run()
}
```

---

## Module Reference

### Configuration (`gaia` package, config.go)

Configuration is JSON-based with three priority levels: env vars > local config > remote config.

```go
// String config
port := gaia.GetSafeConfString("Server.Port")

// Integer config
timeout := gaia.GetSafeConfInt64("Server.Timeout")

// Boolean config
debug := gaia.GetSafeConfBool("App.Debug")

// Load config section into a struct
type ServerConfig struct {
    Port string `json:"port"`
}
cfg := &ServerConfig{}
gaia.LoadConfToObj("Server", cfg)
```

Environment variables override config: `SERVER_PORT` overrides `Server.Port`.

### HTTP Server (`gaia` package, httpserver.go)

A lightweight HTTP/HTTPS server wrapper with graceful shutdown.

```go
// Create with handler
s := gaia.NewHttpServer(":8080", handler)

// Fluently configure
s.WithReadTimeout(30 * time.Second).
  WithWriteTimeout(30 * time.Second).
  WithIdleTimeout(60 * time.Second).
  WithShutdownTimeout(15 * time.Second)

// Start with signal handling (blocks until SIGINT/SIGTERM)
s.Start()

// Start with custom context cancellation
s.StartWithContext(ctx)

// HTTPS
s.WithCertFile("cert.pem").WithKeyFile("key.pem")
```

**Important**: `Start()` calls `GetLogger().Stop()` during shutdown to flush logs. Do not skip this.

### HTTP Server (framework/server package)

For production services, use the framework-level server built on CloudWeGo Hertz:

```go
import "github.com/xxzhwl/gaia/framework/server"

// Create default app (uses "Server" schema config)
app := server.DefaultApp()

// Or with custom schema
app := server.NewApp("MyService")

// Register health check + generic CRUD handlers
app.RegisterCommonHandler()

// Custom GET route
app.GET("/api/demo", server.MakeHandler(func(arg server.Request) (any, error) {
    id := arg.GetUrlParam("id")
    page := arg.GetUrlQuery("page", "1")
    
    var req MyRequest
    if err := arg.BindJsonWithChecker(&req); err != nil {
        return nil, err
    }
    
    return map[string]any{"id": id, "data": req}, nil
}))

// Middleware registration
server.MakePlugin(func(arg server.Request) error {
    // auth logic, return error to reject
    return nil
})

// Start
app.Run()
```

The `server.Request` interface provides:
- `GetUrlParam(key string) string` — path parameters
- `GetUrlQuery(key, default string) string` — query string parameters
- `BindJsonWithChecker(obj any) error` — bind JSON body with automatic validation
- `TraceContext` — the request context for tracing
- `C()` — the underlying Hertz context

### Data Validation (`gaia` package, datachecker.go)

Struct-tag-based validation, invoked automatically by `BindJsonWithChecker` or manually.

```go
type CreateUserRequest struct {
    Username string `json:"username" require:"1"`
    Password string `json:"password" require:"1" length:"8,"`     // min 8 chars
    Email    string `json:"email" validate:"email"`
    Age      int    `json:"age" validate:"range:18-100"`
    Phone    string `json:"phone" validate:"phone"`
    Role     string `json:"role" range:"admin;user;guest"`       // enum
    Score    int    `json:"score" gt:"0" lt:"1000"`              // range
    Birthday string `json:"birthday" validate:"date"`
    Gender   string `json:"gender" length:"1,1"`                 // exactly 1 char
}
```

Supported validation tags:
| Tag | Purpose | Example |
|-----|---------|---------|
| `require:"1"` | Not empty (string/number/slice/map/struct) | `require:"1"` |
| `validate:"email"` | Email format | |
| `validate:"phone"` | Phone format | |
| `validate:"date"` | Date (yyyy-mm-dd or RFC3339) | |
| `validate:"datetime"` | DateTime (yyyy-mm-dd HH:MM:SS) | |
| `validate:"time"` | Date or DateTime | |
| `validate:"month"` | Year-Month (yyyy-mm) | |
| `validate:"timehour"` | Time only (HH:MM:SS) | |
| `range:"a;b;c"` | Enum values | `range:"1;2;3"` |
| `gt:"N"` / `lt:"N"` | Greater than / Less than (numeric) | |
| `gte:"N"` / `lte:"N"` / `ge:"N"` / `le:"N"` | >= or <= (numeric) | |
| `length:"min,max"` | String length range | `length:"6,20"` |
| `length:"N"` | Exact string length | `length:"11"` |

**Nested validation**: Structs within structs, slices of structs, and maps of structs are recursively validated. Anonymous/embedded structs are treated as flattened fields.

**Manual usage**:
```go
checker := gaia.NewDataChecker()
err := checker.CheckStructDataValid(&request)

// Single field checks
checker.Require(value, "field name")
checker.Mail(email, "email")
checker.Date(dateStr, "date")
checker.Datetime(datetimeStr, "datetime")
```

### Proxy / Service Locator (`gaia` package, proxy.go)

Thread-safe in-memory service registry. Services are organized by class and name.

```go
// Register (typically in init() or setup code)
gaia.RegisterProxy("repository", "user", &UserRepository{})
gaia.RegisterProxy("service", "auth", &AuthService{})

// Retrieve
repo := gaia.GetProxy("repository", "user").(*UserRepository)

// Get all services of a class
services := gaia.GetServiceProxies("repository")
```

This is the standard way to wire dependencies in gaia projects — no DI framework needed.

### Retry (`gaia` package, retry.go)

```go
// Simple retry
err := gaia.Retry(func() error {
    return doSomething()
}, 3, 2*time.Second) // 3 retries, 2s interval

// Run at fixed intervals (with context control)
gaia.RunInterval(ctx, func() error {
    return pollStatus()
}, 30*time.Second, true) // run immediately, then every 30s
```

### List / Slice Operations (`gaia` package, list.go)

Functional-style operations with full generics support. These are the primary data manipulation tools.

```go
// Map
ids := gaia.MapListByFunc(users, func(u User) int64 { return u.ID })

// Filter
active := gaia.FilterListByFunc(users, func(u User, idx int) bool { return u.Active })

// Reduce
sum := gaia.ReduceListByFunc(nums, func(agg, itm int) int { return agg + itm })

// Unique
unique := gaia.UniqueList(duplicates) // comparable types
uniqueUsers := gaia.UniqueListByFunc(users, func(u User) int64 { return u.ID })

// Group
groups := gaia.GroupListByFunc(users, func(u User) string { return u.Department })

// Intersection / Union / Difference
inter := gaia.IntersectList(list1, list2)
union := gaia.UnionList(list1, list2)
left, right := gaia.DifferenceList(list1, list2)

// Membership
if gaia.InList(needle, haystack) { ... }

// Find index
idx := gaia.FindListIndex(elem, list) // returns -1 if not found

// Delete by value
cleaned := gaia.DelListValue(list, removeMe)

// Join to string
str := gaia.Join([]int64{1, 2, 3}, ",") // "1,2,3"

// SQL IN clause helper
sqlIn := gaia.ListToStringIn([]string{"a", "b", "c"}) // "'a','b','c'"

// Chunk
chunks := gaia.GroupList(data, 100) // groups of 100

// Reverse / Random sample / Copy
reversed := gaia.ListReverse(list)
sample := gaia.RandList(list, 5)
cloned := gaia.CopyList(list)

// Map list to keyed map
mapped := gaia.GetMapListById(listOfMaps, "id") // map[string]map[string]string
mappedI := gaia.GetMapInterfaceListById(listOfMaps, "id") // map[string]map[string]any
```

### String Utilities (`gaia` package, string.go)

```go
// Empty check
gaia.Empty(value) // true for nil, "", 0, empty slice/map

// Substring
gaia.SubStr(str, start, offset)         // byte-based
gaia.SubStrUnicode(str, start, offset)  // rune-based (supports CJK)

// Naming conventions
gaia.CamelCaseToFilename("MyStruct")      // "my_struct"
gaia.UnderlineToCamelCase("my_struct")    // "MyStruct"
gaia.Title("hello")                        // "Hello"

// Crypto
gaia.Sha256(str)
gaia.Sha512(str)
gaia.BuildMd5(args...)                    // MD5 for unique keys
gaia.EncryptPassword(password)            // salted MD5

// Base64
gaia.Base64Encode(str)
gaia.Base64Decode(encoded)

// URL
gaia.AppendParamsToURL(rawUrl, map[string]string{"key": "val"})

// JSON helpers
gaia.IsJsonString(str)
gaia.PrettyString(v)                      // formatted JSON string
gaia.MustPrettyString(v)                  // no error return

// UUID (base36-encoded nanotime + random)
gaia.GetUUID()

// Split and join
gaia.SplitStr("a,b;c；d")                 // splits on , ; ； ；
gaia.FetchTextVariables("${name}/${path}") // extracts ["name", "path"]

// Validation helpers
gaia.CheckPassComplexity(password)         // requires digits + upper + lower, >= 8
gaia.IsAlphaNum(str)                       // alphanumeric + underscore
gaia.IsHexStr(str)                         // hex characters only
```

### Date/Time (`gaia` package, datetime.go)

Timezone defaults to UTC+8 (Asia/Shanghai) via init().

```go
// Format (uses PHP-style format tokens: Y, m, d, H, i, s)
now := gaia.Date("Y-m-d H:i:s")
today := gaia.Date("Y-m-d")

// From Unix timestamp (auto-detects seconds/millis/micros/nanos)
str := gaia.DateFromUnix("Y-m-d H:i:s", unixStamp)

// Parse string to time
t, err := gaia.StrToTime("2026-06-27 15:30:00")

// Time to string
str := gaia.TimeToStr(t, "Y-m-d H:i:s")

// String to Unix
unix, err := gaia.StrToUnix("2026-06-27 15:30:00")

// Time truncation
truncated := gaia.TimeTruncate(t, 5*time.Minute)   // round down to 5min
truncated := gaia.TimeTruncate(t, -60*time.Second) // round UP to 1min

// Time diff
seconds := gaia.DiffNowToBeforeTime(startNano)
seconds := gaia.DiffNowToFormatTime("2026-06-27 12:00:00")

// Expiration
expireDate, err := gaia.CalExpireDate("2026-06-27", 30)

// LocalTime type — use for JSON serialization and DB storage
type MyModel struct {
    CreatedAt gaia.LocalTime `json:"created_at"`
}
// Automatically handles JSON marshal (2006-01-02 15:04:05) and sql.Scanner/driver.Valuer
```

### Data Conversion (`gaia` package, convert.go)

```go
// String to typed list
list := gaia.StringToList("a,b;c|d")
list := gaia.TextToList("line1\nline2,line3")
list := gaia.StringToListWithDelimit("a|b|c", "|")

// String to KV map
kv, err := gaia.StringToKV("key1:val1;key2:val2")

// KV map to string
str := gaia.KvToString(map[string]string{"k": "v"}) // "k:v"

// Generic data to struct
gaia.ParseDataToStruct(srcData, &targetStruct)    // handles string/[]byte/object
gaia.ParseJsonToStruct(jsonStr, &targetStruct)

// Struct to map
m, err := gaia.ParseStructToMap(&myStruct)         // exported fields only

// Map[string]string to struct (supports tag aliases, nested structs, slices, maps)
// Struct fields can use tags to match map keys:
//   type User struct { ID uint "user_id" }
gaia.ParseMapStringToStruct(dbRow, &user)

// Byte formatting
gaia.UnitBytesToReadable(1024000)     // "1000K"
gaia.UnitNanosecondToReadable(ns)      // human-readable duration

// Float rounding
gaia.Round(3.14159, 2)                // "3.14"
gaia.FloatTrunc(3.14159, 2)           // 3.14 (float64)

// Bytes <-> int32
bytes, err := gaia.IntToBytes(n)
n, err := gaia.BytesToInt(bytes)
```

### File System (`gaia` package, filesystem.go)

```go
// Existence
gaia.FileExists(path)

// Read
lines, err := gaia.ReadLines(filename)       // trimmed, non-empty lines
lines, err := gaia.ReadLinesRaw(filename)    // all lines as-is
data, err := gaia.ReadFileAll(filename)      // entire file as []byte

// Write
gaia.FilePutContent(filename, "content")     // overwrite
gaia.FileAppendContent(filename, "content")  // append
gaia.FileAppendBytes(filename, []byte{...})  // append bytes

// Path manipulation
gaia.FileBaseName("/path/to/file.txt")       // "file.txt"
gaia.FileRemoveSuffix("/path/to/file.txt")   // "file"
gaia.FileSafetyCheck(path, ".txt")           // checks for path traversal

// Directory operations
gaia.MkDirAll("/some/path", 0755)
files, err := gaia.GetAllFilesInDir(dir)      // recursive
dirs, err := gaia.GetAllDirsInDir(dir)        // recursive, with trailing separator
fullPath, err := gaia.FindFilepathInDir(dir, "filename.txt")

// Current path
gaia.GetCurrentAbPath()                       // works for both go run and binary
```

### Cache (`gaia` package, cache.go)

```go
gaia.CacheSet("key", value, 3600)             // value can be any type, TTL in seconds
data, found := gaia.CacheGet("key")

gaia.CacheMSet(map[string]any{
    "key1": "val1",
    "key2": 123,
}, 3600)

gaia.CacheMDel("key1", "key2")
```

### Random (`gaia` package, random.go)

```go
n := gaia.Rand(1, 100)                         // random int between min and max (inclusive)
```

### Error Handling (`gaia` package, errwrap/)

```go
import "github.com/xxzhwl/gaia/errwrap"

// Logic error (business logic failures)
errwrap.NewLogicError("用户不存在")
errwrap.NewLogicErrorF("用户[%s]不存在", username)
// wraps and preserves error code [code] from message

// Not found error
errwrap.NewNotFoundErrorF("resource %s not found", id)

// Error wrap with stack trace
errwrap.Error(err)     // wraps with call location
errwrap.ErrorF(...)    // wraps formatted error with call location
```

### Definitions (`gaia` package, defs/)

```go
import "github.com/xxzhwl/gaia/defs"

// Common structs
defs.KVStct{Key: "k", Value: "v"}      // key-value pair
defs.KNStct{Key: "k", Name: "name"}    // key-name pair
defs.ValueChange{OriginValue: "old", NewValue: "new"}

// Type constraints
defs.IntegerType  // int8|int16|int32|int64|int|uint8|uint16|uint32|uint64|uint
defs.FloatType    // float32|float64
defs.NumberType   // IntegerType|FloatType

// HTTP headers
defs.HeaderAuthorization  // "Authorization"
defs.HeaderTransFormat    // "TransFormat" (cipher/deflate)
```

### Dictionary Operations (`gaia` package, dic/ and dics/)

```go
import "github.com/xxzhwl/gaia/dic"

// Safe accessors from map[string]any (dic)
val := dic.S(m, "key")                    // string, default ""
n := dic.I(m, "key")                      // int64, default 0
f := dic.F(m, "key")                      // float64, default 0
b := dic.GetSafeBool(m, "key", false)     // bool with default

// Value path access (dot-separated nested paths like "user.profile.name")
val := dic.GetValueByMapPath(data, "key1.key2.list[0].name")

import "github.com/xxzhwl/gaia/dics"

// Safe accessors from map[string]string (dics)
val := dics.S(m, "key")                   // string, default ""
n := dics.I(m, "key")                     // int64, default 0
f := dics.F(m, "key")                     // float64, default 0
```

### Type Conversion (`gaia` package, cvt/)

```go
import "github.com/xxzhwl/gaia/cvt"

n := cvt.GetSafeInt64("123", 0)           // with default
s := cvt.GetSafeString(123, "")           // with default
f := cvt.GetFloat64("3.14", "field", 0)   // with label for error context

// Flatten nested map
flat := cvt.Flatten(nestedMap)            // {"a.b.c": val} from {"a":{"b":{"c":val}}}
```

### Generic Pool (`gaia` package, grpoolgt/)

```go
import "github.com/xxzhwl/gaia/grpoolgt"

pool := grpoolgt.NewPool[Task](10)        // generic pool with 10 workers
pool.Start()
pool.Submit(task)
```

### Reflection Utilities (`gaia` package)

```go
// Get struct exported fields
fields := gaia.GetStructFields(myStruct)                // field names
fields := gaia.GetStructFieldsByJsonTagPreference(myStruct) // json tag names

// Get slice element type
kind, err := gaia.GetSliceInnerType(slice)
```

---

## Framework Package (`framework/`)

### Initialization (`framework/init/base.go`)

```go
import "github.com/xxzhwl/gaia/framework"

func main() {
    framework.Init()
    // Initializes: logging, system name, remote config, tracing, HTTP client preprocessors,
    // message notifications, DB logging
}
```

### HTTP Client (`framework/httpclient/`)

```go
import "github.com/xxzhwl/gaia/framework/httpclient"
// Pre-configured HTTP client that integrates with gaia's tracing and logging
```

### Message Notifications (`framework/messageImpl/`)

```go
import "github.com/xxzhwl/gaia/framework/messageImpl"
// Feishu (Lark) bot integration
// baseMessage for custom notification channels
```

### Metrics (`framework/metrics/`)

```go
import "github.com/xxzhwl/gaia/framework/metrics"
// OpenTelemetry metrics with OTLP exporter
```

### Account System (`framework/account/`)

Complete OAuth and account management:
- `account/id.go` — user identity
- `account/session.go` — session management
- `account/security.go` — security policies, key rotation
- `account/verification.go` — email/phone verification
- `account/audit.go` — audit logging
- `account/risk.go` — risk assessment
- `account/health.go` — account health checks
- `account/qq.go`, `account/wechat.go`, `account/google.go`, `account/github.go` — OAuth providers
- `account/denylist.go` — user blocking
- `account/outbox.go` — event outbox pattern

### System (`framework/system/gaia.go`)

Core system-level configuration and utilities.

### SSE (`framework/server/sse.go`)

Server-Sent Events support for real-time streaming.

---

## Components (`components/`)

### Async Task (`components/asynctask/`)

```go
import "github.com/xxzhwl/gaia/components/asynctask"

// API endpoints for task submission and monitoring
asynctask.RegisterTask("task_name", factoryFunc)
asynctask.SubmitTask("task_name", jsonData)

// Scheduler for background task processing
```

### Cron Jobs (`components/jobs/`)

```go
import "github.com/xxzhwl/gaia/components/jobs"

type MyJob struct{}
func (j *MyJob) Run() { /* ... */ }

jobs.RegisterCronJob("job_name", &MyJob{}, "0 2 * * *")
jobs.StartCronService()
```

### Redis (`components/redis/`)

```go
import "github.com/xxzhwl/gaia/components/redis"

client := redis.NewClient()
client.Set("key", "value", 3600*time.Second)

// Distributed lock
lock := redis.NewLock("resource:lock", 10*time.Second)
if lock.Acquire() {
    defer lock.Release()
    // critical section
}

// Rate limiter
limiter := redis.NewRateLimiter(...)
```

### Storage (`components/storage/`)

```go
import "github.com/xxzhwl/gaia/components/storage"
import "github.com/xxzhwl/gaia/components/storage/s3"
import "github.com/xxzhwl/gaia/components/storage/oss"

// Unified storage interface, S3-compatible (MinIO, COS), and Alibaba OSS
```

### Kafka (`components/kafka/`)

```go
import "github.com/xxzhwl/gaia/components/kafka"
// Kafka client for message queue integration
```

### Elasticsearch (`components/es/`)

```go
import "github.com/xxzhwl/gaia/components/es"
// Elasticsearch client for search and analytics
```

### Other Components

- `components/clickhouse/` — ClickHouse analytical DB
- `components/mongo/` — MongoDB client
- `components/mqtt/` — MQTT client
- `components/rabbitmq/` — RabbitMQ client
- `components/influxdb/` — InfluxDB time-series DB
- `components/lock/` — Generic locking abstraction
- `components/buffer/` — Buffered map operations

---

## Coding Patterns for Gaia Projects

### Pattern 1: Handler + Service + Repository

```go
// handler/user_handler.go
func GetUserHandler(arg server.Request) (any, error) {
    id := arg.GetUrlParam("id")
    svc := gaia.GetProxy("service", "user").(*UserService)
    return svc.GetUser(arg.TraceContext, id)
}

// service/user_service.go
type UserService struct{}
func (s *UserService) GetUser(ctx context.Context, id string) (*User, error) {
    repo := gaia.GetProxy("repository", "user").(*UserRepository)
    return repo.FindById(ctx, id)
}

// repository/user_repository.go
type UserRepository struct{}
func (r *UserRepository) FindById(ctx context.Context, id string) (*User, error) {
    db, _ := gaia.NewFrameworkMysql()
    var user User
    result := db.GetGormDb().WithContext(ctx).Where("id = ?", id).First(&user)
    return &user, result.Error
}
```

### Pattern 2: Request Validation

Always define request structs with validation tags:

```go
type CreateOrderRequest struct {
    ProductID int64   `json:"product_id" require:"1" gt:"0"`
    Quantity  int     `json:"quantity" require:"1" gt:"0" lte:"999"`
    Coupon    string  `json:"coupon" length:",32"`
    AddressID int64   `json:"address_id" require:"1"`
}
```

### Pattern 3: Error Messages with Codes

Use bracketed error codes in messages for structured error handling:

```go
return nil, fmt.Errorf("[50001]用户不存在")
return nil, fmt.Errorf("[40001]参数%s不能为空", fieldName)

// Client extracts code:
code := gaia.GetCodeByMessage(err.Error(), 50000)
```

### Pattern 4: Logging

```go
gaia.Info("message")
gaia.InfoF("formatted: %s", value)
gaia.Error("message")
gaia.ErrorF("error: %s", err.Error())
gaia.Log(gaia.LogErrorLevel, "custom level message")
```

### Pattern 5: Proxy Registration in init()

```go
func init() {
    gaia.RegisterProxy("service", "user", &UserService{})
    gaia.RegisterProxy("repository", "user", &UserRepository{})
}
```

---

## Configuration File Convention

```json
{
  "SystemEnName": "MyApp",
  "SystemCnName": "我的应用",
  "Framework": {
    "Mysql": "user:pass@tcp(localhost:3306)/db?charset=utf8mb4&parseTime=True&loc=Local",
    "Redis": { "Address": "localhost:6379", "Password": "" },
    "ES": { "Address": "http://localhost:9200", "UserName": "", "Password": "" }
  },
  "Server": {
    "Port": "8080",
    "Cors": { "Enable": true, "AllowOrigins": ["http://localhost:3000"] },
    "Logger": { "PrintConsole": true, "DetailMode": false, "EnablePushLog": true }
  },
  "AsyncTask": {
    "Port": "8081",
    "Mysql": "user:pass@tcp(localhost:3306)/task_db?charset=utf8mb4&parseTime=True&loc=Local"
  }
}
```

---

## Import Conventions

```go
import (
    "github.com/xxzhwl/gaia"                    // core utilities
    "github.com/xxzhwl/gaia/defs"               // type definitions
    "github.com/xxzhwl/gaia/dic"                // map[string]any accessors
    "github.com/xxzhwl/gaia/dics"               // map[string]string accessors
    "github.com/xxzhwl/gaia/cvt"                // type conversion
    "github.com/xxzhwl/gaia/errwrap"            // error wrapping
    "github.com/xxzhwl/gaia/framework"          // framework init
    "github.com/xxzhwl/gaia/framework/server"   // Hertz-based HTTP server
    "github.com/xxzhwl/gaia/components/asynctask"
    "github.com/xxzhwl/gaia/components/jobs"
    "github.com/xxzhwl/gaia/components/redis"
    "github.com/xxzhwl/gaia/components/storage"
    // etc.
)
```

---

## Development Workflow

When building a new feature with Gaia SDK:

1. **Define the request/response structs** with validation tags
2. **Create the handler** using `server.MakeHandler()`, validate with `BindJsonWithChecker()`
3. **Create the service layer** and register it via `gaia.RegisterProxy()`
4. **Create the repository layer** if DB access is needed, using `gaia.NewFrameworkMysql()`
5. **Register routes** in `main.go` or a route setup function
6. **Add config** to `configs/local/config.json` if new external services are needed
7. **Write tests** following the project's test conventions

---

## Common Pitfalls

1. **Forgetting `framework.Init()`** — must be called before any gaia feature is used
2. **Using wrong map accessor** — `dic` is for `map[string]any`, `dics` is for `map[string]string`
3. **Timezone assumptions** — gaia defaults to UTC+8 (Beijing time). Use `StrToTime` for parsing, not raw `time.Parse`
4. **Empty vs nil** — use `gaia.Empty(v)` for unified emptiness check, not raw comparisons
5. **Not using `LocalTime` for DB times** — use `gaia.LocalTime` in model structs for automatic JSON/DB serialization
6. **Comment style** — new code should use Chinese comments to match the existing codebase convention
