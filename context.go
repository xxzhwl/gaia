package gaia

// 跟踪一次协程(goroutine)内执行的上下文环境，同时，如果是http webserver会话环境，也会将http相关信息传入上下文
// 使用场景:
// 对于HTTP服务，可以将一些头部信息封装在当前处理协程中，这样可以避免显式的传递*http.Request对象才能获取到相关信息，如请求IP等信息
// tracer所能提供的信息通过GoroutineId进行协程内唯一标识
// @author wanlizhan
// @Create 2023-12-16

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	TraceIdKey      = "TraceId"
	FollowFromKey   = "FollowFrom"
	HttpContextType = "HttpType"
)

var traceDataStack map[string]*TraceData
var rwTraceDataStack sync.RWMutex

// 上下文跟踪ID后缀
var traceDataIdSuffix string

func init() {
	traceDataStack = make(map[string]*TraceData)
}

// SetTraceDataIdSuffix 设定上下文跟踪ID生成后缀，此函数在一个进程中一般只会调用一次进行初始化
func SetTraceDataIdSuffix(suffix string) {
	traceDataIdSuffix = suffix
}

// TraceData 装载上下文环境跟踪的数据体
type TraceData struct {
	Id string

	//以下 Request / ResponseWriter / ClientIP / FollowFrom 仅对于HTTP环境适用
	Request        *http.Request
	ResponseWriter http.ResponseWriter
	ClientIP       string //客户端ID
	Pid            int    //进程ID，将进程ID设置到上下文中，便于统一获取

	//上游调用系统名称，通过HTTP头部字段FollowFrom传递过来，如果非HTTP环境或HTTP头部不存在，则置空
	//该字段主要用于记录日志
	FollowFrom string

	//不同系统之间全链路跟踪标识，上层通过HTTP头部某个字段来传递，比如 TraceId，如果上层未传入，则设置与Id等值
	TraceId string
	//可在上下文传递的key-value数据
	KvData map[string]interface{}

	ParentContext context.Context

	ContextType string

	// 调用的各步骤耗时情况
	traceSteps map[string][]StepDuration
}

// StepDuration 一个执行步骤的耗时
type StepDuration struct {
	Tag      string
	start    time.Time
	Duration time.Duration
}

// BuildContextTrace 构建TraceData，后台定时任务，则逻辑启动时，也应该被执行
func BuildContextTrace() {
	goid := GetGoRoutineId()
	if !_allowBuildContext(goid) {
		return
	}

	uuid := _newUUID()
	clientIp := ""
	followFrom := ""
	traceId := uuid
	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()

	traceDataStack[goid] = &TraceData{
		Id:         uuid,
		ClientIP:   clientIp,
		Pid:        os.Getpid(),
		FollowFrom: followFrom,
		TraceId:    traceId,
		KvData:     make(map[string]interface{}),
		traceSteps: make(map[string][]StepDuration),
	}
}

// BuildHttpContextTraceWithRw 利用Response和Request构建TraceData，该函数在接收一个http请求时应该首先被执行
func BuildHttpContextTraceWithRw(w http.ResponseWriter, r *http.Request) {
	goid := GetGoRoutineId()
	if !_allowBuildContext(goid) {
		return
	}

	uuid := _newUUID()
	clientIp := ""
	followFrom := ""
	traceId := ""
	if r != nil {
		//说明是http上下文环境
		clientIp = r.Host
		//从HTTP HEADER中定位TraceId并继承
		traceId = r.Header.Get(TraceIdKey)
		//从HTTP HEADER中定位FollowFrom并设置到上下文中，以便于日志记录
		followFrom = r.Header.Get(FollowFromKey)
	}
	if len(traceId) == 0 {
		//如果TraceId在上游系统调用中不存在或当前运行环境并非HTTP服务环境，则使用Id这个值，即将与LogId相同的值
		traceId = uuid
	}

	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()

	traceDataStack[goid] = &TraceData{
		Id:             uuid,
		Request:        r,
		ResponseWriter: w,
		ClientIP:       clientIp,
		Pid:            os.Getpid(),
		FollowFrom:     followFrom,
		TraceId:        traceId,
		ContextType:    HttpContextType,
		KvData:         make(map[string]interface{}),
		traceSteps:     make(map[string][]StepDuration),
	}
}

// BuildHttpContextTrace 利用Response和Request构建TraceData，该函数在接收一个http请求时应该首先被执行
func BuildHttpContextTrace(traceId, clientIp, followFrom string) {
	goid := GetGoRoutineId()
	if !_allowBuildContext(goid) {
		return
	}

	uuid := _newUUID()
	if len(traceId) == 0 {
		//如果TraceId在上游系统调用中不存在或当前运行环境并非HTTP服务环境，则使用Id这个值，即将与LogId相同的值
		traceId = uuid
	}

	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()

	traceDataStack[goid] = &TraceData{
		Id:          uuid,
		ClientIP:    clientIp,
		Pid:         os.Getpid(),
		FollowFrom:  followFrom,
		TraceId:     traceId,
		ContextType: HttpContextType,
		KvData:      make(map[string]interface{}),
		traceSteps:  make(map[string][]StepDuration),
	}
}

// 是否允许构建上下文环境，如果已经存在上下文环境，则不允许再构建
func _allowBuildContext(goid string) bool {
	rwTraceDataStack.RLock()
	defer rwTraceDataStack.RUnlock()
	if len(traceDataStack) > 0 {
		if _, ok := traceDataStack[goid]; ok {
			//说明上下文环境已经存在，跳过构建，避免多次构建导致上下文不一致
			return false
		}
	}
	return true
}

func _newUUID() string {
	uuid := GetUUID() + traceDataIdSuffix
	sysName := GetSystemEnName()
	if len(sysName) > 0 {
		//将当前系统名称附在uuid上
		uuid = fmt.Sprintf("%s%s", strings.ToLower(sysName), uuid)
	}
	return uuid
}

// RenewId 重置上下文追踪的日志id，可用于继承上下文后区分后续日志的场景. 如果未构建上下文，则不作任何操作.
func RenewId() {
	RenewIdByValue(_newUUID())
}

// RenewIdByValue 手动设置上下文追踪的日志id.如果未构建上下文，则不作任何操作.
func RenewIdByValue(id string) {
	goid := GetGoRoutineId()

	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()

	// 不存在上下文数据，直接返回. 注意这里获取的是指针
	stack, ok := traceDataStack[goid]
	if !ok {
		return
	}

	//这里需要考虑复制出一个新的结构体，否则多次刷新ID的情况下，会出现相互覆盖的情况
	newStack := *stack
	newStack.Id = id
	traceDataStack[goid] = &newStack

}

// RemoveContextTrace 移除当前上下文环境中的 TraceData，该函数在执行完一次完整的协程处理后(比如 http请求)执行
func RemoveContextTrace() {
	goid := GetGoRoutineId()
	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()
	delete(traceDataStack, goid)
}

// SetTraceStep 记录当前执行步骤的时间点, 用于统计程序运行各步骤耗时
// 调用前需要先 BuildContextTrace，否则无法记录
func SetTraceStep(tag string) {
	goid := GetGoRoutineId()

	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()

	stack, ok := traceDataStack[goid]
	if !ok {
		return
	}

	stack.traceSteps[goid] = append(stack.traceSteps[goid], StepDuration{tag, time.Now(), 0})
	traceDataStack[goid] = stack
}

// GetTraceSteps 获取当前协程所有执行步骤的耗时情况
func GetTraceSteps() []StepDuration {
	stack := GetContextTrace()
	if stack == nil {
		return nil
	}

	steps := stack.traceSteps[GetGoRoutineId()]
	for i := 0; i < len(steps); i++ {
		curTick := steps[i].start
		if i == len(steps)-1 {
			steps[i].Duration = time.Since(curTick)
		} else {
			steps[i].Duration = steps[i+1].start.Sub(curTick)
		}
	}

	return steps
}

// GetContextTrace 获取上下文环境信息，如果不存在，则返回nil
func GetContextTrace() *TraceData {
	//定位当前goroutine id
	goid := GetGoRoutineId()

	rwTraceDataStack.RLock()
	defer rwTraceDataStack.RUnlock()

	if contextTrace, ok := traceDataStack[goid]; ok {
		return contextTrace
	} else {
		return nil
	}
}

// ResetContextTrace 将某一个特定协程ID上下文范围内的 TraceData，设定到当前协程的上下文环境中
// 主要应用在协程之间上下文继承的场景中，比如在某一个http处理的过程中，开启了异步协程，而异步协程也想要继承前置协程的上下文环境
func ResetContextTrace(otherGoroutineId string) {
	goid := GetGoRoutineId()
	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()

	if traceData, ok := traceDataStack[otherGoroutineId]; ok {
		//定位TraceData成功
		traceDataStack[goid] = traceData
	}
}

// ResetContextTraceByData 通过实际的TraceData，来重置当前协程的上下文环境信息
// 此函数逻辑主要应用于 server.Go 中。
// 如果通过协程ID来继承上下文环境，被继承的协程在获取数据时有可能已结束，导致获取数据为空，
// 因此，在前端协程中将数据生成后，后续协程直接使用才是完全安全的
func ResetContextTraceByData(data *TraceData) {
	if data == nil {
		return
	}
	goid := GetGoRoutineId()
	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()
	traceDataStack[goid] = data
}

// IsHttpContextTrace 判断当前上下文环境是否为http上下文
func IsHttpContextTrace() bool {
	data := GetContextTrace()
	if data != nil && data.ContextType == HttpContextType {
		//说明是http上下文环境
		return true
	}
	return false
}

// GetContextTraceLen 获取上下文总数量，对于web server应用来说，理论上有同时有多少个http请求，总数量就是多少
// 这个函数也可以用于监控并发请求的情况
func GetContextTraceLen() int {
	rwTraceDataStack.RLock()
	defer rwTraceDataStack.RUnlock()
	return len(traceDataStack)
}

// ContextTrace 针对ContextTrace的安全使用封装
type ContextTrace struct {
}

// NewContextTrace 实例化
func NewContextTrace() *ContextTrace {
	return &ContextTrace{}
}

// GetId 获取TraceData的Id值
func (ct *ContextTrace) GetId() string {
	data := GetContextTrace()
	if data != nil {
		return data.Id
	}
	return ""
}

// GetTraceId 获取TraceData的TraceId值
func (ct *ContextTrace) GetTraceId() string {
	data := GetContextTrace()
	if data != nil {
		return data.TraceId
	}
	return ""
}

// GetFollowFrom 获取TraceData的FollowFrom值
func (ct *ContextTrace) GetFollowFrom() string {
	data := GetContextTrace()
	if data != nil {
		return data.FollowFrom
	}
	return ""
}

// GetClientIP 获取TraceData的 ClientIP 值
func (ct *ContextTrace) GetClientIP() string {
	data := GetContextTrace()
	if data != nil {
		return data.ClientIP
	}
	return ""
}

// Getpid 获取当前进程ID
func (ct *ContextTrace) Getpid() int {
	data := GetContextTrace()
	if data != nil {
		return data.Pid
	}
	return 0
}

// GetHttpRequest 获取TraceData的 Request 值
func (ct *ContextTrace) GetHttpRequest() *http.Request {
	data := GetContextTrace()
	if data != nil {
		return data.Request
	}
	return nil
}

// GetHttpResponseWriter 获取TraceData的 ResponseWriter 值
func (ct *ContextTrace) GetHttpResponseWriter() http.ResponseWriter {
	data := GetContextTrace()
	if data != nil {
		return data.ResponseWriter
	}
	return nil
}

// GetParentCtx 获取上下文用于链路追踪
func (ct *ContextTrace) GetParentCtx() context.Context {
	data := GetContextTrace()
	if data != nil {
		return data.ParentContext
	}
	return context.Background()
}

// SetKvData 将某一个key-value数据设置到上下文中
// 如果不存在上下文环境，则返回错误
func (ct *ContextTrace) SetKvData(key string, value interface{}) error {
	//定位当前goroutine id
	goid := GetGoRoutineId()

	rwTraceDataStack.Lock()
	defer rwTraceDataStack.Unlock()

	obj, ok := traceDataStack[goid]
	if !ok {
		return fmt.Errorf("set context trace kv data ERR, context trace data object not found")
	}
	if obj.KvData == nil {
		obj.KvData = make(map[string]interface{})
	}
	obj.KvData[key] = value
	return nil
}

// GetKvData 获取上下文环境中的所有key-value数据集
// 使用时需避免对返回的map进行写操作，避免panic。考虑优先使用GetKvField
func (ct *ContextTrace) GetKvData() map[string]interface{} {
	return ct.kvData()
}

func (ct *ContextTrace) kvData() map[string]any {
	//定位当前goroutine id
	data := GetContextTrace()
	if data != nil {
		return data.KvData
	}
	return make(map[string]interface{})
}

// GetKvField 从上下文kv数据中获取一个特定的字段. 不存在时，返回的是nil
func (ct *ContextTrace) GetKvField(field string) (val any) {
	// 相比于直接返回map，风险更低
	data := ct.GetKvData()
	return data[field]
}
