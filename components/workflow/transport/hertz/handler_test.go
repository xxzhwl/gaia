package hertz

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/xxzhwl/gaia/components/workflow"
	"github.com/xxzhwl/gaia/components/workflow/domain"
	workflowengine "github.com/xxzhwl/gaia/components/workflow/engine"
	"github.com/xxzhwl/gaia/components/workflow/testfixture"
)

func newTestRouter(t *testing.T, mws ...app.HandlerFunc) (*route.Engine, *workflow.Engine) {
	t.Helper()
	engine := workflow.NewMemoryEngine()
	opt := config.NewOptions(nil)
	r := route.NewEngine(opt)
	NewHandler(engine).Register(r.Group("/api/workflow"), mws...)
	return r, engine
}

func deployDefinition(t *testing.T, engine *workflow.Engine) domain.ProcessDefinition {
	t.Helper()
	def, err := engine.DeployDefinition(context.Background(), testfixture.OrderApprovalDefinition("https://worker.example.com/tasks"))
	if err != nil {
		t.Fatalf("deploy definition: %v", err)
	}
	return def
}

func TestHertzListDefinitionsWrapsUnifiedResponse(t *testing.T) {
	r, engine := newTestRouter(t)
	deployDefinition(t, engine)

	w := ut.PerformRequest(r, consts.MethodGet, "/api/workflow/definitions", nil)
	resp := w.Result()
	if resp.StatusCode() != consts.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode())
	}
	var body struct {
		Code int64          `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		t.Fatalf("unmarshal response: %v body=%s", err, resp.Body())
	}
	if body.Code != 0 {
		t.Fatalf("expected code 0, got %d (%s)", body.Code, body.Msg)
	}
	if body.Data == nil || body.Data["total"] == nil {
		t.Fatalf("expected paged data with total, got %#v", body.Data)
	}
}

func TestHertzStartProcessRoute(t *testing.T) {
	r, engine := newTestRouter(t)
	def := deployDefinition(t, engine)

	payload, _ := json.Marshal(workflowengine.StartProcessRequest{
		BusinessKey: "ORDER_HZ_1",
		Starter:     "user_1",
		Variables:   map[string]any{"orderId": "ORDER_HZ_1"},
	})
	w := ut.PerformRequest(r, consts.MethodPost, "/api/workflow/processes/"+def.Key+"/start",
		&ut.Body{Body: bytes.NewReader(payload), Len: len(payload)},
		ut.Header{Key: "Content-Type", Value: "application/json"},
	)
	resp := w.Result()
	if resp.StatusCode() != consts.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode(), resp.Body())
	}
	var body struct {
		Code int64          `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if body.Code != 0 || body.Data["Status"] != string(domain.InstanceStatusRunning) {
		t.Fatalf("unexpected start process response: %#v", body)
	}
}

func TestHertzMiddlewareCanBlockRequest(t *testing.T) {
	blocker := func(c context.Context, ctx *app.RequestContext) {
		ctx.AbortWithStatusJSON(consts.StatusUnauthorized, map[string]any{"code": 401, "msg": "unauthorized"})
	}
	r, _ := newTestRouter(t, blocker)

	w := ut.PerformRequest(r, consts.MethodGet, "/api/workflow/definitions", nil)
	if w.Result().StatusCode() != consts.StatusUnauthorized {
		t.Fatalf("expected middleware to block with 401, got %d", w.Result().StatusCode())
	}
}
