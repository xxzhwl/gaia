// Package hertz 提供基于 gaia framework/server（Hertz）的工作流 HTTP 适配层。
//
// 与 transport/http（标准库零依赖实现）不同，本适配层融入 gaia 框架生态：
//   - 路由通过 route.RouterGroup 注册，风格与 framework/account 的 registerAuthRoutes 一致；
//   - 处理器用 server.MakeHandler 包装，自动获得统一响应体 {code,msg,data,ext}、
//     链路追踪、panic 恢复与错误码到 HTTP 状态码的映射；
//   - 可直接挂载 framework/account 的鉴权/授权中间件（app.HandlerFunc），
//     组件本身不实现任何鉴权逻辑，只提供注入入口并透传租户上下文。
package hertz

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/xxzhwl/gaia/components/workflow/automation"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	"github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/framework/server"
)

// callbackTokenHeader 是外部 worker 回调完成任务时携带回调令牌的请求头。
const callbackTokenHeader = "X-Workflow-Callback-Token"

// Engine 定义 Hertz 适配层需要调用的工作流能力，与 transport/http 的 Engine 保持一致。
type Engine interface {
	CreateDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	UpdateDefinition(ctx context.Context, definitionID string, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	DeployDefinition(ctx context.Context, def domain.ProcessDefinition) (domain.ProcessDefinition, error)
	DisableDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	EnableDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	GetDefinition(ctx context.Context, definitionID string) (domain.ProcessDefinition, error)
	ListDefinitions(ctx context.Context, filter domain.DefinitionListFilter) (domain.PageResult[domain.ProcessDefinition], error)
	StartProcess(ctx context.Context, req engine.StartProcessRequest) (domain.ProcessInstance, error)
	GetInstance(ctx context.Context, instanceID string) (domain.ProcessInstance, error)
	ListInstances(ctx context.Context, filter domain.InstanceListFilter) (domain.PageResult[domain.ProcessInstance], error)
	ListTasks(ctx context.Context, filter domain.TaskListFilter) (domain.PageResult[domain.Task], error)
	Tasks(ctx context.Context, instanceID string) ([]domain.Task, error)
	Variables(ctx context.Context, instanceID string) (map[string]domain.Variable, error)
	VariablesByNames(ctx context.Context, instanceID string, names []string) (map[string]domain.Variable, error)
	UpdateVariables(ctx context.Context, req engine.UpdateVariablesRequest) (map[string]domain.Variable, error)
	DeleteVariables(ctx context.Context, req engine.VariableNamesRequest) (map[string]domain.Variable, error)
	SystemVariables(ctx context.Context, instanceID string) (engine.InstanceSystemVariables, error)
	Timeline(ctx context.Context, instanceID string) ([]domain.AuditEvent, error)
	Outbox(ctx context.Context, filter domain.OutboxListFilter) (domain.PageResult[domain.OutboxEvent], error)
	PurgeOutbox(ctx context.Context, limit int) (int64, error)
	CompleteTask(ctx context.Context, req engine.CompleteTaskRequest) (domain.ProcessInstance, error)
	FailTask(ctx context.Context, req engine.FailTaskRequest) (domain.ProcessInstance, error)
	RetryTask(ctx context.Context, req engine.RetryTaskRequest) (domain.Task, error)
	ClaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	UnclaimTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	TransferTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	DelegateTask(ctx context.Context, req engine.TaskOperationRequest) (domain.Task, error)
	RejectTask(ctx context.Context, req engine.TaskOperationRequest) (domain.ProcessInstance, error)
	ScanTimeoutTasks(ctx context.Context, req engine.ScanTimeoutTasksRequest) (engine.ScanTimeoutTasksResult, error)
	TerminateInstance(ctx context.Context, req engine.InstanceLifecycleRequest) (domain.ProcessInstance, error)
	SuspendInstance(ctx context.Context, req engine.InstanceLifecycleRequest) (domain.ProcessInstance, error)
	ResumeInstance(ctx context.Context, req engine.InstanceLifecycleRequest) (domain.ProcessInstance, error)
	RegisterAutomationService(ctx context.Context, service automation.Service) (automation.Service, error)
	UnregisterAutomationService(ctx context.Context, serviceID string) error
	ListAutomationServices(ctx context.Context) ([]automation.Service, error)
	ListAutomationTasks(ctx context.Context) ([]automation.Task, error)
}

// Handler 将工作流 Engine 暴露为 Hertz 路由。
type Handler struct {
	engine Engine
}

// NewHandler 创建 Hertz 适配器。
func NewHandler(engine Engine) *Handler {
	return &Handler{engine: engine}
}

// Register 在指定 RouterGroup 下注册工作流路由。
//
// middlewares 会作用于该分组下的全部路由（鉴权、授权、租户解析等由宿主提供）。
// 例如：h.Register(srv.Group("/api/workflow"), accountMiddleware.AuthenticateAnyBearer())。
func (h *Handler) Register(group *route.RouterGroup, middlewares ...app.HandlerFunc) {
	for _, mw := range middlewares {
		if mw != nil {
			group.Use(mw)
		}
	}

	group.POST("/definitions", h.wrap(h.createDefinition))
	group.GET("/definitions", h.wrap(h.listDefinitions))
	group.POST("/definitions/deploy", h.wrap(h.deployDefinition))
	group.GET("/definitions/:definitionId", h.wrap(h.getDefinition))
	group.PUT("/definitions/:definitionId", h.wrap(h.updateDefinition))
	group.POST("/definitions/:definitionId/disable", h.wrap(func(req server.Request) (any, error) {
		return h.engine.DisableDefinition(reqCtx(req), req.GetUrlParam("definitionId"))
	}))
	group.POST("/definitions/:definitionId/enable", h.wrap(func(req server.Request) (any, error) {
		return h.engine.EnableDefinition(reqCtx(req), req.GetUrlParam("definitionId"))
	}))

	group.POST("/processes/:definitionKey/start", h.wrap(h.startProcess))

	group.GET("/instances", h.wrap(h.listInstances))
	group.GET("/instances/:instanceId", h.wrap(h.getInstance))
	group.GET("/instances/:instanceId/tasks", h.wrap(h.instanceTasks))
	group.GET("/instances/:instanceId/variables", h.wrap(h.variables))
	group.PATCH("/instances/:instanceId/variables", h.wrap(h.updateVariables))
	group.DELETE("/instances/:instanceId/variables", h.wrap(h.deleteVariables))
	group.GET("/instances/:instanceId/system-context", h.wrap(h.systemVariables))
	group.GET("/instances/:instanceId/timeline", h.wrap(h.timeline))
	group.POST("/instances/:instanceId/terminate", h.wrap(h.lifecycle(h.engine.TerminateInstance)))
	group.POST("/instances/:instanceId/suspend", h.wrap(h.lifecycle(h.engine.SuspendInstance)))
	group.POST("/instances/:instanceId/resume", h.wrap(h.lifecycle(h.engine.ResumeInstance)))

	group.GET("/tasks", h.wrap(h.listTasks))
	group.POST("/tasks/:taskId/complete", h.wrap(h.completeTask))
	group.POST("/tasks/:taskId/fail", h.wrap(h.failTask))
	group.POST("/tasks/:taskId/retry", h.wrap(h.retryTask))
	group.POST("/tasks/:taskId/claim", h.wrap(h.taskOperation(h.engine.ClaimTask)))
	group.POST("/tasks/:taskId/unclaim", h.wrap(h.taskOperation(h.engine.UnclaimTask)))
	group.POST("/tasks/:taskId/transfer", h.wrap(h.taskOperation(h.engine.TransferTask)))
	group.POST("/tasks/:taskId/delegate", h.wrap(h.taskOperation(h.engine.DelegateTask)))
	group.POST("/tasks/:taskId/reject", h.wrap(h.rejectTask))

	group.GET("/outbox", h.wrap(h.outbox))
	group.POST("/outbox/purge", h.wrap(h.purgeOutbox))
	group.POST("/sla/scan", h.wrap(h.slaScan))

	group.GET("/automation/services", h.wrap(h.listAutomationServices))
	group.POST("/automation/services", h.wrap(h.registerAutomationService))
	group.DELETE("/automation/services/:serviceId", h.wrap(h.unregisterAutomationService))
	group.GET("/automation/tasks", h.wrap(h.listAutomationTasks))
}

// wrap 用 framework 的 MakeHandler 统一包装处理器，获得统一响应、追踪与 panic 恢复。
func (h *Handler) wrap(fn func(req server.Request) (any, error)) app.HandlerFunc {
	return server.MakeHandler(fn)
}

// reqCtx 返回带租户信息的请求上下文：优先复用中间件写入的租户，否则回退到请求头。
func reqCtx(req server.Request) context.Context {
	ctx := req.TraceContext
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := domain.TenantFromContext(ctx); ok {
		return ctx
	}
	if tenant := string(req.C().GetHeader("X-Tenant-ID")); tenant != "" {
		ctx = domain.WithTenant(ctx, tenant)
	}
	return ctx
}

func (h *Handler) createDefinition(req server.Request) (any, error) {
	var def domain.ProcessDefinition
	if err := req.BindJson(&def); err != nil {
		return nil, err
	}
	return h.engine.CreateDefinition(reqCtx(req), def)
}

func (h *Handler) updateDefinition(req server.Request) (any, error) {
	var def domain.ProcessDefinition
	if err := req.BindJson(&def); err != nil {
		return nil, err
	}
	return h.engine.UpdateDefinition(reqCtx(req), req.GetUrlParam("definitionId"), def)
}

func (h *Handler) deployDefinition(req server.Request) (any, error) {
	var def domain.ProcessDefinition
	if err := req.BindJson(&def); err != nil {
		return nil, err
	}
	return h.engine.DeployDefinition(reqCtx(req), def)
}

func (h *Handler) getDefinition(req server.Request) (any, error) {
	return h.engine.GetDefinition(reqCtx(req), req.GetUrlParam("definitionId"))
}

func (h *Handler) listDefinitions(req server.Request) (any, error) {
	return h.engine.ListDefinitions(reqCtx(req), domain.DefinitionListFilter{
		PageRequest: pageRequest(req),
		Key:         req.GetUrlQuery("key"),
		Name:        req.GetUrlQuery("name"),
		Status:      domain.DefinitionStatus(req.GetUrlQuery("status")),
	})
}

func (h *Handler) startProcess(req server.Request) (any, error) {
	var start engine.StartProcessRequest
	if err := req.BindJson(&start); err != nil {
		return nil, err
	}
	start.DefinitionKey = req.GetUrlParam("definitionKey")
	return h.engine.StartProcess(reqCtx(req), start)
}

func (h *Handler) getInstance(req server.Request) (any, error) {
	return h.engine.GetInstance(reqCtx(req), req.GetUrlParam("instanceId"))
}

func (h *Handler) listInstances(req server.Request) (any, error) {
	return h.engine.ListInstances(reqCtx(req), domain.InstanceListFilter{
		PageRequest:   pageRequest(req),
		DefinitionKey: req.GetUrlQuery("definition_key"),
		Status:        domain.InstanceStatus(req.GetUrlQuery("status")),
	})
}

func (h *Handler) instanceTasks(req server.Request) (any, error) {
	return h.engine.Tasks(reqCtx(req), req.GetUrlParam("instanceId"))
}

func (h *Handler) variables(req server.Request) (any, error) {
	names := variableNamesFromRequest(req)
	if len(names) > 0 {
		return h.engine.VariablesByNames(reqCtx(req), req.GetUrlParam("instanceId"), names)
	}
	return h.engine.Variables(reqCtx(req), req.GetUrlParam("instanceId"))
}

func (h *Handler) updateVariables(req server.Request) (any, error) {
	var update engine.UpdateVariablesRequest
	if err := req.BindJson(&update); err != nil {
		return nil, err
	}
	update.InstanceID = req.GetUrlParam("instanceId")
	return h.engine.UpdateVariables(reqCtx(req), update)
}

func (h *Handler) deleteVariables(req server.Request) (any, error) {
	deleteReq := engine.VariableNamesRequest{
		InstanceID: req.GetUrlParam("instanceId"),
		Names:      variableNamesFromRequest(req),
	}
	if len(deleteReq.Names) == 0 && len(req.C().Request.Body()) > 0 {
		if err := req.BindJson(&deleteReq); err != nil {
			return nil, err
		}
		deleteReq.InstanceID = req.GetUrlParam("instanceId")
	}
	return h.engine.DeleteVariables(reqCtx(req), deleteReq)
}

func (h *Handler) systemVariables(req server.Request) (any, error) {
	return h.engine.SystemVariables(reqCtx(req), req.GetUrlParam("instanceId"))
}

func (h *Handler) timeline(req server.Request) (any, error) {
	return h.engine.Timeline(reqCtx(req), req.GetUrlParam("instanceId"))
}

func (h *Handler) listTasks(req server.Request) (any, error) {
	return h.engine.ListTasks(reqCtx(req), domain.TaskListFilter{
		PageRequest: pageRequest(req),
		InstanceID:  req.GetUrlQuery("instance_id"),
		Status:      domain.TaskStatus(req.GetUrlQuery("status")),
		Type:        domain.TaskType(req.GetUrlQuery("type")),
		Assignee:    req.GetUrlQuery("assignee"),
	})
}

func (h *Handler) completeTask(req server.Request) (any, error) {
	var complete engine.CompleteTaskRequest
	if err := req.BindJson(&complete); err != nil {
		return nil, err
	}
	complete.TaskID = req.GetUrlParam("taskId")
	if complete.CallbackToken == "" {
		complete.CallbackToken = string(req.C().GetHeader(callbackTokenHeader))
	}
	return h.engine.CompleteTask(reqCtx(req), complete)
}

func (h *Handler) failTask(req server.Request) (any, error) {
	var fail engine.FailTaskRequest
	if err := req.BindJson(&fail); err != nil {
		return nil, err
	}
	fail.TaskID = req.GetUrlParam("taskId")
	if fail.CallbackToken == "" {
		fail.CallbackToken = string(req.C().GetHeader(callbackTokenHeader))
	}
	return h.engine.FailTask(reqCtx(req), fail)
}

func (h *Handler) retryTask(req server.Request) (any, error) {
	var retry engine.RetryTaskRequest
	if err := req.BindJson(&retry); err != nil {
		return nil, err
	}
	retry.TaskID = req.GetUrlParam("taskId")
	return h.engine.RetryTask(reqCtx(req), retry)
}

func (h *Handler) rejectTask(req server.Request) (any, error) {
	op, err := bindOperation(req)
	if err != nil {
		return nil, err
	}
	return h.engine.RejectTask(reqCtx(req), op)
}

// taskOperation 把签名一致的认领/取消认领/转办/委托操作包装成处理器。
func (h *Handler) taskOperation(fn func(context.Context, engine.TaskOperationRequest) (domain.Task, error)) func(server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		op, err := bindOperation(req)
		if err != nil {
			return nil, err
		}
		return fn(reqCtx(req), op)
	}
}

// lifecycle 把签名一致的终止/挂起/恢复操作包装成处理器。
func (h *Handler) lifecycle(fn func(context.Context, engine.InstanceLifecycleRequest) (domain.ProcessInstance, error)) func(server.Request) (any, error) {
	return func(req server.Request) (any, error) {
		var lc engine.InstanceLifecycleRequest
		if len(req.C().Request.Body()) > 0 {
			if err := req.BindJson(&lc); err != nil {
				return nil, err
			}
		}
		lc.InstanceID = req.GetUrlParam("instanceId")
		return fn(reqCtx(req), lc)
	}
}

func (h *Handler) outbox(req server.Request) (any, error) {
	return h.engine.Outbox(reqCtx(req), domain.OutboxListFilter{
		PageRequest: pageRequest(req),
		EventType:   firstQuery(req, "event_type", "eventType"),
		Status:      domain.OutboxStatus(strings.TrimSpace(req.GetUrlQuery("status"))),
	})
}

func (h *Handler) purgeOutbox(req server.Request) (any, error) {
	limit := queryInt(req, "limit", 500)
	if len(req.C().Request.Body()) > 0 {
		var body struct {
			Limit int `json:"limit"`
		}
		if err := req.BindJson(&body); err != nil {
			return nil, err
		}
		if body.Limit > 0 {
			limit = body.Limit
		}
	}
	count, err := h.engine.PurgeOutbox(reqCtx(req), limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"purged": count}, nil
}

func (h *Handler) slaScan(req server.Request) (any, error) {
	var scan engine.ScanTimeoutTasksRequest
	if len(req.C().Request.Body()) > 0 {
		if err := req.BindJson(&scan); err != nil {
			return nil, err
		}
	}
	return h.engine.ScanTimeoutTasks(reqCtx(req), scan)
}

func (h *Handler) listAutomationServices(req server.Request) (any, error) {
	return h.engine.ListAutomationServices(reqCtx(req))
}

func (h *Handler) registerAutomationService(req server.Request) (any, error) {
	var svc automation.Service
	if err := req.BindJson(&svc); err != nil {
		return nil, err
	}
	return h.engine.RegisterAutomationService(reqCtx(req), svc)
}

func (h *Handler) unregisterAutomationService(req server.Request) (any, error) {
	if err := h.engine.UnregisterAutomationService(reqCtx(req), req.GetUrlParam("serviceId")); err != nil {
		return nil, err
	}
	return map[string]bool{"deleted": true}, nil
}

func (h *Handler) listAutomationTasks(req server.Request) (any, error) {
	return h.engine.ListAutomationTasks(reqCtx(req))
}

func bindOperation(req server.Request) (engine.TaskOperationRequest, error) {
	var op engine.TaskOperationRequest
	if len(req.C().Request.Body()) > 0 {
		if err := req.BindJson(&op); err != nil {
			return engine.TaskOperationRequest{}, err
		}
	}
	op.TaskID = req.GetUrlParam("taskId")
	return op, nil
}

func pageRequest(req server.Request) domain.PageRequest {
	return domain.PageRequest{
		Page:     queryInt(req, "page", 1),
		PageSize: queryIntFallback(req, "page_size", "pageSize", 10),
	}
}

func queryInt(req server.Request, key string, fallback int) int {
	return parseIntDefault(req.GetUrlQuery(key), fallback)
}

func queryIntFallback(req server.Request, primary, secondary string, fallback int) int {
	if raw := req.GetUrlQuery(primary); raw != "" {
		return parseIntDefault(raw, fallback)
	}
	return parseIntDefault(req.GetUrlQuery(secondary), fallback)
}

func firstQuery(req server.Request, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(req.GetUrlQuery(key)); value != "" {
			return value
		}
	}
	return ""
}
