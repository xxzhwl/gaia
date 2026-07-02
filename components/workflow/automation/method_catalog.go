package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/xxzhwl/gaia"
)

// MethodCatalog 管理 worker 暴露的自动化任务元数据和真实 gaia proxy 绑定。
type MethodCatalog struct {
	handlers map[string]methodHandler
	order    []string
}

// MethodBinding 描述一个 automationTaskKey 最终映射到的 gaia proxy 方法。
type MethodBinding struct {
	Task        Task
	Theme       string
	ServiceName string
	MethodName  string
}

type methodHandler struct {
	task        Task
	theme       string
	serviceName string
	methodName  string
	hasContext  bool
	inputType   reflect.Type
}

type methodSpec struct {
	task  Task
	theme string
}

// MethodOption 用于覆盖反射生成的自动化任务元数据。
type MethodOption func(*methodSpec)

// WithTaskName 设置自动化任务展示名称。
func WithTaskName(name string) MethodOption {
	return func(spec *methodSpec) {
		spec.task.Name = name
	}
}

// WithTaskDescription 设置自动化任务描述。
func WithTaskDescription(description string) MethodOption {
	return func(spec *methodSpec) {
		spec.task.Description = description
	}
}

// WithTaskMethod 设置自动化任务 HTTP 方法。
func WithTaskMethod(method string) MethodOption {
	return func(spec *methodSpec) {
		spec.task.Method = method
	}
}

// WithTaskEndpoint 设置自动化任务实际调用地址。
func WithTaskEndpoint(endpoint string) MethodOption {
	return func(spec *methodSpec) {
		spec.task.Endpoint = endpoint
	}
}

// WithTheme 设置 automation task 对应的 asynctask SystemName/gaia proxy class。
func WithTheme(theme string) MethodOption {
	return func(spec *methodSpec) {
		spec.theme = strings.TrimSpace(theme)
	}
}

// NewMethodCatalog 创建自动化任务方法目录。
func NewMethodCatalog() *MethodCatalog {
	return &MethodCatalog{handlers: map[string]methodHandler{}}
}

// RegisterProxyMethod 注册 automationTaskKey 到真实 gaia proxy 方法的绑定。
func (c *MethodCatalog) RegisterProxyMethod(key, serviceName string, proxy any, methodName string, opts ...MethodOption) error {
	if c == nil {
		return fmt.Errorf("automation method catalog is nil")
	}
	key = strings.TrimSpace(key)
	serviceName = strings.TrimSpace(serviceName)
	methodName = strings.TrimSpace(methodName)
	if key == "" {
		return fmt.Errorf("automation task key is required")
	}
	if serviceName == "" {
		return fmt.Errorf("automation service name is required")
	}
	if methodName == "" {
		return fmt.Errorf("automation method name is required")
	}
	method, err := gaia.GetCallbackFunc(proxy, methodName, fmt.Sprintf("automation method %s.%s not found", serviceName, methodName))
	if err != nil {
		return err
	}
	compiled, err := compileCallable(key, method)
	if err != nil {
		return err
	}
	compiled = applyMethodOptions(key, compiled, opts...)
	// 未显式指定描述时，尝试用 go/doc 从源码里抽方法注释作为默认描述。
	if strings.TrimSpace(compiled.task.Description) == "" {
		if doc := MethodDoc(proxy, methodName); doc != "" {
			compiled.task.Description = doc
		}
	}
	theme, err := resolveTheme(compiled.theme)
	if err != nil {
		return err
	}
	compiled.theme = theme
	compiled.serviceName = serviceName
	compiled.methodName = methodName
	gaia.RegisterProxy(theme, serviceName, proxy)
	c.store(key, compiled)
	return nil
}

// MustRegisterProxyMethod 注册 proxy 方法，失败时 panic。
func (c *MethodCatalog) MustRegisterProxyMethod(key, serviceName string, proxy any, methodName string, opts ...MethodOption) {
	if err := c.RegisterProxyMethod(key, serviceName, proxy, methodName, opts...); err != nil {
		panic(err)
	}
}

type serviceSpec struct {
	theme         string
	methodOptions map[string][]MethodOption
}

// ServiceOption 用于 RegisterService 的批量注册配置。
type ServiceOption func(*serviceSpec)

// WithServiceTheme 设置整个服务下方法默认使用的 theme。
func WithServiceTheme(theme string) ServiceOption {
	return func(spec *serviceSpec) {
		spec.theme = strings.TrimSpace(theme)
	}
}

// WithServiceMethodOptions 设置某个方法对应 automation task 的元数据选项。
func WithServiceMethodOptions(methodName string, opts ...MethodOption) ServiceOption {
	return func(spec *serviceSpec) {
		methodName = strings.TrimSpace(methodName)
		if methodName == "" {
			return
		}
		if spec.methodOptions == nil {
			spec.methodOptions = map[string][]MethodOption{}
		}
		spec.methodOptions[methodName] = append(spec.methodOptions[methodName], opts...)
	}
}

// RegisterService 将 proxy 上全部 exported 方法按方法名注册为 automation task。
func (c *MethodCatalog) RegisterService(serviceName string, proxy any, opts ...ServiceOption) error {
	spec := serviceSpec{methodOptions: map[string][]MethodOption{}}
	for _, opt := range opts {
		opt(&spec)
	}
	value := reflect.ValueOf(proxy)
	if !value.IsValid() {
		return fmt.Errorf("automation service proxy is nil")
	}
	typ := value.Type()
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		methodOpts := append([]MethodOption(nil), spec.methodOptions[method.Name]...)
		if spec.theme != "" {
			methodOpts = append(methodOpts, WithTheme(spec.theme))
		}
		if err := c.RegisterProxyMethod(method.Name, serviceName, proxy, method.Name, methodOpts...); err != nil {
			return err
		}
	}
	return nil
}

// Binding 查询 automationTaskKey 对应的真实 gaia proxy 方法绑定。
func (c *MethodCatalog) Binding(key string) (MethodBinding, bool) {
	if c == nil {
		return MethodBinding{}, false
	}
	handler, ok := c.handlers[strings.TrimSpace(key)]
	if !ok || handler.theme == "" || handler.serviceName == "" || handler.methodName == "" {
		return MethodBinding{}, false
	}
	return handler.binding(), true
}

// Bindings 返回当前目录下全部已绑定的 proxy 方法。
func (c *MethodCatalog) Bindings() []MethodBinding {
	if c == nil {
		return nil
	}
	bindings := make([]MethodBinding, 0, len(c.order))
	for _, key := range c.order {
		handler := c.handlers[key]
		if handler.theme == "" || handler.serviceName == "" || handler.methodName == "" {
			continue
		}
		bindings = append(bindings, handler.binding())
	}
	return bindings
}

// BuildArgsJSON 将 workflow variables 转成目标方法的 JSON 入参。
func (c *MethodCatalog) BuildArgsJSON(key string, variables map[string]any) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("automation method catalog is nil")
	}
	handler, ok := c.handlers[strings.TrimSpace(key)]
	if !ok {
		return nil, fmt.Errorf("automation task %s not found", key)
	}
	if handler.inputType == nil {
		return []byte("{}"), nil
	}
	input, err := buildInputValue(handler.inputType, variables)
	if err != nil {
		return nil, err
	}
	return json.Marshal(input.Interface())
}

// Tasks 返回当前方法目录对外暴露的任务定义。
func (c *MethodCatalog) Tasks(serviceID, endpoint string) []Task {
	if c == nil {
		return nil
	}
	tasks := make([]Task, 0, len(c.order))
	for _, key := range c.order {
		handler := c.handlers[key]
		task := handler.task
		task.ServiceID = serviceID
		if strings.TrimSpace(task.Endpoint) == "" {
			task.Endpoint = endpoint
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func compileCallable(key string, value reflect.Value) (methodHandler, error) {
	typ := value.Type()
	if typ.IsVariadic() {
		return methodHandler{}, fmt.Errorf("automation task %s handler cannot be variadic", key)
	}
	compiled := methodHandler{
		task: Task{
			Key:    key,
			Method: http.MethodPost,
		},
	}
	argOffset := 0
	if typ.NumIn() > 0 && typ.In(0).Implements(contextType()) {
		compiled.hasContext = true
		argOffset = 1
	}
	if typ.NumIn()-argOffset > 1 {
		return methodHandler{}, fmt.Errorf("automation task %s handler supports at most one input argument", key)
	}
	if typ.NumIn()-argOffset == 1 {
		compiled.inputType = typ.In(argOffset)
		compiled.task.InputSchema = schemaFromType(compiled.inputType, true)
	}
	if typ.NumOut() > 2 {
		return methodHandler{}, fmt.Errorf("automation task %s handler supports at most output and error returns", key)
	}
	if typ.NumOut() > 0 {
		last := typ.Out(typ.NumOut() - 1)
		if last.Implements(errorType()) {
			outputCount := typ.NumOut() - 1
			if outputCount > 1 {
				return methodHandler{}, fmt.Errorf("automation task %s handler supports at most one output return", key)
			}
			if outputCount == 1 {
				compiled.task.OutputSchema = schemaFromType(typ.Out(0), false)
			}
			return compiled, nil
		}
	}
	outputCount := typ.NumOut()
	if outputCount > 1 {
		return methodHandler{}, fmt.Errorf("automation task %s handler supports at most one output return", key)
	}
	if outputCount == 1 {
		compiled.task.OutputSchema = schemaFromType(typ.Out(0), false)
	}
	return compiled, nil
}

func applyMethodOptions(key string, compiled methodHandler, opts ...MethodOption) methodHandler {
	spec := methodSpec{task: compiled.task, theme: compiled.theme}
	for _, opt := range opts {
		opt(&spec)
	}
	compiled.task = spec.task
	compiled.theme = spec.theme
	if strings.TrimSpace(compiled.task.Name) == "" {
		compiled.task.Name = humanizeKey(key)
	}
	if strings.TrimSpace(compiled.task.Method) == "" {
		compiled.task.Method = http.MethodPost
	}
	return compiled
}

func (c *MethodCatalog) store(key string, handler methodHandler) {
	if _, exists := c.handlers[key]; !exists {
		c.order = append(c.order, key)
		sort.Strings(c.order)
	}
	c.handlers[key] = handler
}

func (h methodHandler) binding() MethodBinding {
	return MethodBinding{
		Task:        h.task,
		Theme:       h.theme,
		ServiceName: h.serviceName,
		MethodName:  h.methodName,
	}
}

func resolveTheme(theme string) (string, error) {
	theme = strings.TrimSpace(theme)
	if theme == "" {
		theme = strings.TrimSpace(gaia.GetSystemEnName())
	}
	if theme == "" {
		return "", fmt.Errorf("automation theme is required")
	}
	return theme, nil
}

func schemaFromType(typ reflect.Type, input bool) []Parameter {
	typ = indirectType(typ)
	if typ == nil || typ.Kind() != reflect.Struct {
		return nil
	}
	params := make([]Parameter, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" || field.Tag.Get("json") == "-" {
			continue
		}
		key, omitempty := jsonFieldName(field)
		if key == "" {
			key = lowerFirst(field.Name)
		}
		meta := parseWorkflowTag(field.Tag.Get("workflow"))
		required := input && !omitempty
		if meta["required"] == "true" {
			required = true
		}
		if meta["optional"] == "true" || field.Tag.Get("required") == "false" {
			required = false
		}
		name := firstNonEmpty(meta["name"], field.Tag.Get("name"), key)
		description := firstNonEmpty(meta["description"], meta["desc"], field.Tag.Get("description"))
		params = append(params, Parameter{
			Key:          key,
			Name:         name,
			Type:         workflowType(field.Type),
			Required:     required,
			DefaultValue: defaultValue(field),
			Description:  description,
		})
	}
	return params
}

func buildInputValue(typ reflect.Type, variables map[string]any) (reflect.Value, error) {
	if variables == nil {
		variables = map[string]any{}
	}
	if typ.Kind() == reflect.Pointer {
		value := reflect.New(typ.Elem())
		if err := fillValue(value.Elem(), variables); err != nil {
			return reflect.Value{}, err
		}
		return value, nil
	}
	value := reflect.New(typ).Elem()
	if err := fillValue(value, variables); err != nil {
		return reflect.Value{}, err
	}
	return value, nil
}

func fillValue(value reflect.Value, raw any) error {
	if !value.CanSet() {
		return nil
	}
	if raw == nil {
		return nil
	}
	switch value.Kind() {
	case reflect.Struct:
		values, ok := raw.(map[string]any)
		if !ok {
			return decodeByJSON(value, raw)
		}
		typ := value.Type()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" || field.Tag.Get("json") == "-" {
				continue
			}
			key, _ := jsonFieldName(field)
			if key == "" {
				key = lowerFirst(field.Name)
			}
			rawField, exists := values[key]
			if !exists {
				continue
			}
			if err := fillValue(value.Field(i), rawField); err != nil {
				return fmt.Errorf("decode field %s: %w", key, err)
			}
		}
		return nil
	case reflect.Pointer:
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}
		return fillValue(value.Elem(), raw)
	case reflect.String:
		value.SetString(fmt.Sprint(raw))
		return nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(fmt.Sprint(raw))
		if err != nil {
			return err
		}
		value.SetBool(parsed)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := parseInt(raw)
		if err != nil {
			return err
		}
		value.SetInt(parsed)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := parseUint(raw)
		if err != nil {
			return err
		}
		value.SetUint(parsed)
		return nil
	case reflect.Float32, reflect.Float64:
		parsed, err := parseFloat(raw)
		if err != nil {
			return err
		}
		value.SetFloat(parsed)
		return nil
	case reflect.Interface:
		value.Set(reflect.ValueOf(raw))
		return nil
	default:
		return decodeByJSON(value, raw)
	}
}

func decodeByJSON(value reflect.Value, raw any) error {
	body, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	target := reflect.New(value.Type())
	if err := json.Unmarshal(body, target.Interface()); err != nil {
		return err
	}
	value.Set(target.Elem())
	return nil
}

func parseInt(raw any) (int64, error) {
	switch value := raw.(type) {
	case int:
		return int64(value), nil
	case int8:
		return int64(value), nil
	case int16:
		return int64(value), nil
	case int32:
		return int64(value), nil
	case int64:
		return value, nil
	case float32:
		return int64(value), nil
	case float64:
		return int64(value), nil
	default:
		return strconv.ParseInt(fmt.Sprint(raw), 10, 64)
	}
}

func parseUint(raw any) (uint64, error) {
	switch value := raw.(type) {
	case uint:
		return uint64(value), nil
	case uint8:
		return uint64(value), nil
	case uint16:
		return uint64(value), nil
	case uint32:
		return uint64(value), nil
	case uint64:
		return value, nil
	case float32:
		return uint64(value), nil
	case float64:
		return uint64(value), nil
	default:
		return strconv.ParseUint(fmt.Sprint(raw), 10, 64)
	}
}

func parseFloat(raw any) (float64, error) {
	switch value := raw.(type) {
	case int:
		return float64(value), nil
	case int8:
		return float64(value), nil
	case int16:
		return float64(value), nil
	case int32:
		return float64(value), nil
	case int64:
		return float64(value), nil
	case uint:
		return float64(value), nil
	case uint8:
		return float64(value), nil
	case uint16:
		return float64(value), nil
	case uint32:
		return float64(value), nil
	case uint64:
		return float64(value), nil
	case float32:
		return float64(value), nil
	case float64:
		return value, nil
	default:
		return strconv.ParseFloat(fmt.Sprint(raw), 64)
	}
}

func workflowType(typ reflect.Type) string {
	typ = indirectType(typ)
	if typ == nil {
		return "object"
	}
	if typ.PkgPath() == "time" && typ.Name() == "Time" {
		return "string"
	}
	switch typ.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.String:
		return "string"
	case reflect.Slice, reflect.Array:
		return "array"
	default:
		return "object"
	}
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	omitempty := false
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

func parseWorkflowTag(tag string) map[string]string {
	result := map[string]string{}
	for _, item := range strings.Split(tag, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			result[item] = "true"
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result
}

func defaultValue(field reflect.StructField) any {
	value := field.Tag.Get("default")
	if value == "" {
		return nil
	}
	return value
}

func indirectType(typ reflect.Type) reflect.Type {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

func contextType() reflect.Type {
	return reflect.TypeOf((*context.Context)(nil)).Elem()
}

func errorType() reflect.Type {
	return reflect.TypeOf((*error)(nil)).Elem()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func humanizeKey(key string) string {
	parts := strings.Fields(strings.ReplaceAll(key, "_", " "))
	for i := range parts {
		parts[i] = lowerFirst(parts[i])
	}
	return strings.Join(parts, " ")
}
