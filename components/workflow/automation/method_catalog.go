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
)

// MethodCatalog 通过反射管理 worker 暴露的自动化任务方法。
type MethodCatalog struct {
	handlers map[string]methodHandler
	order    []string
}

type methodHandler struct {
	task        Task
	handler     reflect.Value
	hasContext  bool
	inputType   reflect.Type
	outputType  reflect.Type
	errorReturn bool
}

// MethodOption 用于覆盖反射生成的自动化任务元数据。
type MethodOption func(*Task)

// WithTaskName 设置自动化任务展示名称。
func WithTaskName(name string) MethodOption {
	return func(task *Task) {
		task.Name = name
	}
}

// WithTaskDescription 设置自动化任务描述。
func WithTaskDescription(description string) MethodOption {
	return func(task *Task) {
		task.Description = description
	}
}

// WithTaskMethod 设置自动化任务 HTTP 方法。
func WithTaskMethod(method string) MethodOption {
	return func(task *Task) {
		task.Method = method
	}
}

// WithTaskEndpoint 设置自动化任务实际调用地址。
func WithTaskEndpoint(endpoint string) MethodOption {
	return func(task *Task) {
		task.Endpoint = endpoint
	}
}

// NewMethodCatalog 创建自动化任务方法目录。
func NewMethodCatalog() *MethodCatalog {
	return &MethodCatalog{handlers: map[string]methodHandler{}}
}

// Register 注册一个自动化任务方法，并从函数入参/返回值反射生成参数结构。
func (c *MethodCatalog) Register(key string, handler any, opts ...MethodOption) error {
	if c == nil {
		return fmt.Errorf("automation method catalog is nil")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("automation task key is required")
	}
	compiled, err := compileHandler(key, handler)
	if err != nil {
		return err
	}
	for _, opt := range opts {
		opt(&compiled.task)
	}
	if strings.TrimSpace(compiled.task.Name) == "" {
		compiled.task.Name = humanizeKey(key)
	}
	if strings.TrimSpace(compiled.task.Method) == "" {
		compiled.task.Method = http.MethodPost
	}
	if _, exists := c.handlers[key]; !exists {
		c.order = append(c.order, key)
		sort.Strings(c.order)
	}
	c.handlers[key] = compiled
	return nil
}

// MustRegister 注册自动化任务方法，失败时 panic。
func (c *MethodCatalog) MustRegister(key string, handler any, opts ...MethodOption) {
	if err := c.Register(key, handler, opts...); err != nil {
		panic(err)
	}
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

// Execute 根据任务 key 执行已注册的方法。
func (c *MethodCatalog) Execute(ctx context.Context, key string, variables map[string]any) (map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("automation method catalog is nil")
	}
	handler, ok := c.handlers[strings.TrimSpace(key)]
	if !ok {
		return nil, fmt.Errorf("automation task %s not found", key)
	}
	args := make([]reflect.Value, 0, 2)
	if handler.hasContext {
		args = append(args, reflect.ValueOf(ctx))
	}
	if handler.inputType != nil {
		input, err := buildInputValue(handler.inputType, variables)
		if err != nil {
			return nil, err
		}
		args = append(args, input)
	}
	results := handler.handler.Call(args)
	if handler.errorReturn {
		errValue := results[len(results)-1]
		if !errValue.IsNil() {
			return nil, errValue.Interface().(error)
		}
		results = results[:len(results)-1]
	}
	if len(results) == 0 {
		return map[string]any{}, nil
	}
	return valueToMap(results[0])
}

func compileHandler(key string, handler any) (methodHandler, error) {
	value := reflect.ValueOf(handler)
	if !value.IsValid() || value.Kind() != reflect.Func {
		return methodHandler{}, fmt.Errorf("automation task %s handler must be a function", key)
	}
	typ := value.Type()
	if typ.IsVariadic() {
		return methodHandler{}, fmt.Errorf("automation task %s handler cannot be variadic", key)
	}
	compiled := methodHandler{
		task: Task{
			Key:    key,
			Method: http.MethodPost,
		},
		handler: value,
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
			compiled.errorReturn = true
		}
	}
	outputCount := typ.NumOut()
	if compiled.errorReturn {
		outputCount--
	}
	if outputCount > 1 {
		return methodHandler{}, fmt.Errorf("automation task %s handler supports at most one output return", key)
	}
	if outputCount == 1 {
		compiled.outputType = typ.Out(0)
		compiled.task.OutputSchema = schemaFromType(compiled.outputType, false)
	}
	return compiled, nil
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

func valueToMap(value reflect.Value) (map[string]any, error) {
	if !value.IsValid() {
		return map[string]any{}, nil
	}
	if value.Kind() == reflect.Pointer && value.IsNil() {
		return map[string]any{}, nil
	}
	raw := value.Interface()
	if result, ok := raw.(map[string]any); ok {
		return result, nil
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]any{"value": raw}, nil
	}
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
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
