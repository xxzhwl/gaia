// Package http 提供工作流组件的标准库 HTTP 适配层。
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
)

// Engine 定义 HTTP 适配层需要调用的工作流能力。
type Engine interface {
	CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error)
	StartProcess(ctx context.Context, req engine.StartProcessRequest) (domain.ProcessInstance, error)
	GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error)
	ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error)
	ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error)
	Tasks(ctx context.Context, instanceID string) ([]domain.Task, error)
	Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error)
	Timeline(ctx context.Context, instanceID string) ([]domain.AuditEvent, error)
	Outbox(ctx context.Context) ([]domain.OutboxEvent, error)
	CompleteTask(ctx context.Context, req engine.CompleteTaskRequest) (domain.ProcessInstance, error)
	ClaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	UnclaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	TransferTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	DelegateTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	RejectTask(ctx context.Context, req engine.TaskOperationRequest) (domain.ProcessInstance, error)
	ScanTimeoutTasks(ctx context.Context, req engine.ScanTimeoutTasksRequest) (engine.ScanTimeoutTasksResult, error)
	RegisterAutomationService(ctx context.Context, service automation.Service) (automation.Service, error)
	UnregisterAutomationService(ctx context.Context, serviceID string) error
	ListAutomationServices(ctx context.Context) ([]automation.Service, error)
	ListAutomationTasks(ctx context.Context) ([]automation.Task, error)
}

// Handler 将工作流 Engine 暴露为标准库 HTTP Handler。
type Handler struct {
	engine Engine
}

// NewHandler 创建 HTTP 适配器。
func NewHandler(engine Engine) *Handler {
	return &Handler{engine: engine}
}

// Register 将工作流 HTTP 路由注册到 mux。
func (h *Handler) Register(mux *http.ServeMux, prefix string) {
	prefix = "/" + strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "/" {
		mux.Handle("/", h)
		return
	}
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
	mux.Handle(prefix, http.StripPrefix(prefix, h))
}

// ServeHTTP 分发工作流 HTTP 请求。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.engine == nil {
		writeError(w, http.StatusInternalServerError, "workflow http handler engine is nil")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := splitPath(path)
	switch {
	case path == "definitions":
		h.handleDefinitions(w, r)
	case path == "definitions/deploy":
		h.handleDeployDefinition(w, r)
	case len(parts) == 2 && parts[0] == "definitions":
		h.handleDefinition(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "processes" && parts[2] == "start":
		h.handleStartProcess(w, r, parts[1])
	case path == "instances":
		h.handleInstances(w, r)
	case len(parts) == 2 && parts[0] == "instances":
		h.handleInstance(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "instances" && parts[2] == "tasks":
		h.handleInstanceTasks(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "instances" && parts[2] == "variables":
		h.handleVariables(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "instances" && parts[2] == "timeline":
		h.handleTimeline(w, r, parts[1])
	case path == "tasks":
		h.handleTasks(w, r)
	case len(parts) == 3 && parts[0] == "tasks":
		h.handleTaskOperation(w, r, parts[1], parts[2])
	case path == "outbox":
		h.handleOutbox(w, r)
	case path == "sla/scan":
		h.handleSLAScan(w, r)
	case path == "automation/services":
		h.handleAutomationServices(w, r)
	case path == "automation/tasks":
		h.handleAutomationTasks(w, r)
	case len(parts) == 3 && parts[0] == "automation" && parts[1] == "services":
		h.handleAutomationService(w, r, parts[2])
	default:
		writeError(w, http.StatusNotFound, "workflow route not found")
	}
}

func (h *Handler) handleDefinitions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		result, err := h.engine.ListDefinitions(r.Context(), domain.DefinitionListFilter{
			PageRequest: pageRequest(r),
			Key:         strings.TrimSpace(r.URL.Query().Get("key")),
			Name:        strings.TrimSpace(r.URL.Query().Get("name")),
			Status:      domain.DefinitionStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		})
		writeResult(w, result, err)
	case http.MethodPost:
		var req domain.ProcessDefinition
		if bindJSON(w, r, &req) {
			result, err := h.engine.CreateDefinition(r.Context(), req)
			writeResult(w, result, err)
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleDeployDefinition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req domain.ProcessDefinition
	if bindJSON(w, r, &req) {
		result, err := h.engine.DeployDefinition(r.Context(), req)
		writeResult(w, result, err)
	}
}

func (h *Handler) handleDefinition(w http.ResponseWriter, r *http.Request, definitionID string) {
	switch r.Method {
	case http.MethodGet:
		result, err := h.engine.GetDefinition(r.Context(), definitionID)
		writeResult(w, result, err)
	case http.MethodPut:
		var req domain.ProcessDefinition
		if bindJSON(w, r, &req) {
			result, err := h.engine.UpdateDefinition(r.Context(), definitionID, req)
			writeResult(w, result, err)
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleStartProcess(w http.ResponseWriter, r *http.Request, definitionKey string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req engine.StartProcessRequest
	if bindJSON(w, r, &req) {
		req.DefinitionKey = definitionKey
		result, err := h.engine.StartProcess(r.Context(), req)
		writeResult(w, result, err)
	}
}

func (h *Handler) handleInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.ListInstances(r.Context(), domain.InstanceListFilter{
		PageRequest:   pageRequest(r),
		DefinitionKey: strings.TrimSpace(r.URL.Query().Get("definition_key")),
		Status:        domain.InstanceStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
	})
	writeResult(w, result, err)
}

func (h *Handler) handleInstance(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.GetInstance(r.Context(), instanceID)
	writeResult(w, result, err)
}

func (h *Handler) handleInstanceTasks(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.Tasks(r.Context(), instanceID)
	writeResult(w, result, err)
}

func (h *Handler) handleVariables(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.Variables(r.Context(), instanceID)
	writeResult(w, result, err)
}

func (h *Handler) handleTimeline(w http.ResponseWriter, r *http.Request, instanceID string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.Timeline(r.Context(), instanceID)
	writeResult(w, result, err)
}

func (h *Handler) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.ListTasks(r.Context(), domain.TaskListFilter{
		PageRequest: pageRequest(r),
		InstanceID:  strings.TrimSpace(r.URL.Query().Get("instance_id")),
		Status:      domain.TaskStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		Type:        domain.TaskType(strings.TrimSpace(r.URL.Query().Get("type"))),
		Assignee:    strings.TrimSpace(r.URL.Query().Get("assignee")),
	})
	writeResult(w, result, err)
}

func (h *Handler) handleTaskOperation(w http.ResponseWriter, r *http.Request, taskID, operation string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req engine.TaskOperationRequest
	if operation == "complete" {
		var completeReq engine.CompleteTaskRequest
		if bindJSON(w, r, &completeReq) {
			completeReq.TaskID = taskID
			result, err := h.engine.CompleteTask(r.Context(), completeReq)
			writeResult(w, result, err)
		}
		return
	}
	if !bindJSON(w, r, &req) {
		return
	}
	req.TaskID = taskID
	switch operation {
	case "claim":
		result, err := h.engine.ClaimTask(r.Context(), req)
		writeResult(w, result, err)
	case "unclaim":
		result, err := h.engine.UnclaimTask(r.Context(), req)
		writeResult(w, result, err)
	case "transfer":
		result, err := h.engine.TransferTask(r.Context(), req)
		writeResult(w, result, err)
	case "delegate":
		result, err := h.engine.DelegateTask(r.Context(), req)
		writeResult(w, result, err)
	case "reject":
		result, err := h.engine.RejectTask(r.Context(), req)
		writeResult(w, result, err)
	default:
		writeError(w, http.StatusNotFound, "task operation not found")
	}
}

func (h *Handler) handleOutbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.Outbox(r.Context())
	writeResult(w, result, err)
}

func (h *Handler) handleSLAScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req engine.ScanTimeoutTasksRequest
	if r.Body != nil && r.ContentLength != 0 && !bindJSON(w, r, &req) {
		return
	}
	result, err := h.engine.ScanTimeoutTasks(r.Context(), req)
	writeResult(w, result, err)
}

func (h *Handler) handleAutomationServices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		result, err := h.engine.ListAutomationServices(r.Context())
		writeResult(w, result, err)
	case http.MethodPost:
		var req automation.Service
		if bindJSON(w, r, &req) {
			result, err := h.engine.RegisterAutomationService(r.Context(), req)
			writeResult(w, result, err)
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) handleAutomationTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	result, err := h.engine.ListAutomationTasks(r.Context())
	writeResult(w, result, err)
}

func (h *Handler) handleAutomationService(w http.ResponseWriter, r *http.Request, serviceID string) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeResult(w, map[string]bool{"deleted": true}, h.engine.UnregisterAutomationService(r.Context(), serviceID))
}

func bindJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, data any, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "msg": "ok", "data": data})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"code": status, "msg": message})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func pageRequest(r *http.Request) domain.PageRequest {
	return domain.PageRequest{
		Page:     queryInt(r, "page", 1),
		PageSize: queryInt(r, "page_size", queryInt(r, "pageSize", 10)),
	}
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	parts := strings.Split(path, "/")
	result := parts[:0]
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
