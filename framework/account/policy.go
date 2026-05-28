package account

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xxzhwl/gaia"
	"gorm.io/gorm"
)

// Policy 定义 ABAC 策略，用于基于属性的授权判断。
type Policy struct {
	ID         string    `json:"id" gorm:"size:36;primaryKey"`
	TenantID   string    `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_policies_code,priority:1"`
	Code       string    `json:"code" gorm:"size:120;not null;uniqueIndex:uniq_acct_policies_code,priority:2"`
	Name       string    `json:"name" gorm:"size:120;not null"`
	Effect     string    `json:"effect" gorm:"size:16;not null;default:allow"` // allow / deny
	Expression string    `json:"expression" gorm:"type:text;not null"`
	ResourceType string  `json:"resource_type" gorm:"size:80;not null;default:*"`
	Action     string    `json:"action" gorm:"size:80;not null;default:*"`
	Priority   int       `json:"priority" gorm:"not null;default:0"`
	Version    int64     `json:"version" gorm:"not null;default:1"`
	Status     string    `json:"status" gorm:"size:20;not null;default:enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Policy) TableName() string { return "acct_policies" }

// PolicyService 提供 ABAC 策略的 CRUD 和评估功能。
type PolicyService struct {
	m *Manager
}

// CreatePolicyRequest 创建策略的请求参数。
type CreatePolicyRequest struct {
	TenantID     string
	Code         string
	Name         string
	Effect       string // allow / deny
	Expression   string
	ResourceType string // * 表示匹配所有资源类型
	Action       string // * 表示匹配所有操作
	Priority     int    // 越大越优先
}

// PolicyDecision 策略评估结果。
type PolicyDecision struct {
	Allowed bool
	Reason  string
	Matched bool // 是否有策略匹配
}

// CreatePolicy 创建新的 ABAC 策略。
func (s *PolicyService) CreatePolicy(ctx context.Context, req CreatePolicyRequest) (*Policy, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.policy.create")
	defer span.End()

	effect := req.Effect
	if effect != "deny" {
		effect = "allow"
	}
	rt := req.ResourceType
	if rt == "" {
		rt = "*"
	}
	action := req.Action
	if action == "" {
		action = "*"
	}

	policy := &Policy{
		ID:           newID(),
		TenantID:     s.m.tenantID(req.TenantID),
		Code:         req.Code,
		Name:         req.Name,
		Effect:       effect,
		Expression:   req.Expression,
		ResourceType: rt,
		Action:       action,
		Priority:     req.Priority,
		Version:      1,
		Status:       "enabled",
	}
	if err := s.m.db.WithContext(ctx).Create(policy).Error; err != nil {
		return nil, fmt.Errorf("create policy: %w", err)
	}
	return policy, nil
}

// UpdatePolicy 更新策略。
func (s *PolicyService) UpdatePolicy(ctx context.Context, policyID string, req CreatePolicyRequest) error {
	ctx, span := s.m.tracer.Start(ctx, "account.policy.update")
	defer span.End()

	updates := map[string]any{
		"name":          req.Name,
		"expression":    req.Expression,
		"resource_type": req.ResourceType,
		"action":        req.Action,
		"priority":      req.Priority,
		"version":       gorm.Expr("version + 1"),
	}
	if req.Effect == "deny" || req.Effect == "allow" {
		updates["effect"] = req.Effect
	}
	return s.m.db.WithContext(ctx).Model(&Policy{}).Where("id = ?", policyID).Updates(updates).Error
}

// DeletePolicy 软删除策略。
func (s *PolicyService) DeletePolicy(ctx context.Context, policyID string) error {
	return s.m.db.WithContext(ctx).Model(&Policy{}).Where("id = ?", policyID).
		Update("status", "deleted").Error
}

// ListPolicies 列出租户下的所有启用策略。
func (s *PolicyService) ListPolicies(ctx context.Context, tenantID string) ([]Policy, error) {
	var policies []Policy
	err := s.m.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", s.m.tenantID(tenantID), "enabled").
		Order("priority DESC, code").
		Find(&policies).Error
	return policies, err
}

// EvaluatePolicies 评估所有匹配的策略并返回决策。
// 评估逻辑：
// 1. 按 priority 降序排列
// 2. 匹配 resource_type 和 action（* 为通配）
// 3. 评估表达式
// 4. 第一个匹配的 deny 直接拒绝
// 5. 任意匹配的 allow 则允许
// 6. 无匹配则返回未匹配
func (s *PolicyService) EvaluatePolicies(ctx context.Context, req AuthzRequest) (*PolicyDecision, error) {
	ctx, span := s.m.tracer.Start(ctx, "account.policy.evaluate")
	defer span.End()

	tenantID := s.m.tenantID(req.Subject.TenantID)

	var policies []Policy
	err := s.m.db.WithContext(ctx).
		Where("tenant_id = ? AND status = ?", tenantID, "enabled").
		Order("priority DESC, code").
		Find(&policies).Error
	if err != nil {
		return nil, fmt.Errorf("load policies: %w", err)
	}

	env := buildPolicyEnv(req)

	var anyAllowed bool
	for _, p := range policies {
		if !matchResourceAction(p, req) {
			continue
		}
		matched, err := evalExpression(p.Expression, env)
		if err != nil {
			gaia.WarnF("[account] policy %s eval error: %v", p.Code, err)
			continue
		}
		if !matched {
			continue
		}
		if p.Effect == "deny" {
			return &PolicyDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("denied by policy %s", p.Code),
				Matched: true,
			}, nil
		}
		anyAllowed = true
	}

	if anyAllowed {
		return &PolicyDecision{Allowed: true, Reason: "allowed by policy", Matched: true}, nil
	}
	return &PolicyDecision{Matched: false}, nil
}

// matchResourceAction 检查策略是否匹配请求的资源类型和操作。
func matchResourceAction(p Policy, req AuthzRequest) bool {
	if p.ResourceType != "*" && p.ResourceType != req.ResourceType {
		return false
	}
	if p.Action != "*" {
		perm := req.Permission
		if idx := strings.LastIndex(perm, ":"); idx >= 0 {
			perm = perm[idx+1:]
		}
		if p.Action != perm {
			return false
		}
	}
	return true
}

// PolicyEnv 策略表达式求值环境。
type PolicyEnv struct {
	Subject  map[string]string
	Resource map[string]string
}

// buildPolicyEnv 从 AuthzRequest 构建策略求值环境。
func buildPolicyEnv(req AuthzRequest) PolicyEnv {
	env := PolicyEnv{
		Subject: map[string]string{
			"user_id": req.Subject.UserID,
			"tenant_id": req.Subject.TenantID,
		},
		Resource: map[string]string{
			"type":    req.ResourceType,
			"id":      req.ResourceID,
			"owner_id": req.OwnerID,
		},
	}
	if len(req.Subject.Roles) > 0 {
		env.Subject["roles"] = strings.Join(req.Subject.Roles, ",")
	}
	return env
}

// evalExpression 评估策略表达式。
// 支持的语法：
//   subject.user_id == resource.owner_id
//   subject.roles contains "admin"
//   resource.type == "document" && subject.user_id == resource.owner_id
//   subject.user_id in ["id1", "id2"]
//   resource.type == "order"
//   contains(subject.roles, "admin")
func evalExpression(expr string, env PolicyEnv) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}

	// Handle AND (&&)
	if parts := splitLogical(expr, "&&"); len(parts) > 1 {
		for _, part := range parts {
			ok, err := evalExpression(part, env)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}

	// Handle OR (||)
	if parts := splitLogical(expr, "||"); len(parts) > 1 {
		for _, part := range parts {
			ok, err := evalExpression(part, env)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}

	// Handle NOT (!)
	if strings.HasPrefix(expr, "!") {
		inner := strings.TrimSpace(expr[1:])
		if strings.HasPrefix(inner, "(") && strings.HasSuffix(inner, ")") {
			inner = strings.TrimSpace(inner[1 : len(inner)-1])
		}
		ok, err := evalExpression(inner, env)
		return !ok, err
	}

	// Handle parenthesized expression
	if strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		return evalExpression(strings.TrimSpace(expr[1:len(expr)-1]), env)
	}

	// Handle "contains(attr, value)" function
	if strings.HasPrefix(expr, "contains(") && strings.HasSuffix(expr, ")") {
		return evalContains(expr, env)
	}

	// Handle "attr in [val1, val2]" syntax
	if idx := strings.Index(expr, " in "); idx > 0 {
		return evalIn(expr[:idx], expr[idx+4:], env)
	}

	// Handle comparison operators
	for _, op := range []string{"==", "!=", ">=", "<=", ">", "<"} {
		if idx := strings.Index(expr, op); idx > 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.TrimSpace(expr[idx+len(op):])
			return evalCompare(left, op, right, env)
		}
	}

	return false, fmt.Errorf("unsupported expression: %s", expr)
}

// splitLogical 按逻辑运算符拆分表达式，尊重括号嵌套。
func splitLogical(expr, op string) []string {
	var parts []string
	depth := 0
	last := 0
	for i := 0; i <= len(expr)-len(op); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && expr[i:i+len(op)] == op {
			part := strings.TrimSpace(expr[last:i])
			if part != "" {
				parts = append(parts, part)
			}
			last = i + len(op)
		}
	}
	part := strings.TrimSpace(expr[last:])
	if part != "" {
		parts = append(parts, part)
	}
	return parts
}

// resolveAttr 解析属性路径，如 "subject.user_id"。
func resolveAttr(path string, env PolicyEnv) (string, bool) {
	path = strings.TrimSpace(path)
	parts := strings.SplitN(path, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	prefix, key := parts[0], parts[1]
	switch prefix {
	case "subject":
		v, ok := env.Subject[key]
		return v, ok
	case "resource":
		v, ok := env.Resource[key]
		return v, ok
	}
	return "", false
}

// resolveValue 解析值：支持属性引用、字符串字面量和数字。
func resolveValue(val string, env PolicyEnv) string {
	val = strings.TrimSpace(val)
	// String literal
	if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
		(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
		return val[1 : len(val)-1]
	}
	// Attribute reference
	if v, ok := resolveAttr(val, env); ok {
		return v
	}
	return val
}

// evalCompare 评估比较表达式。
func evalCompare(left, op, right string, env PolicyEnv) (bool, error) {
	l := resolveValue(left, env)
	r := resolveValue(right, env)
	switch op {
	case "==":
		return l == r, nil
	case "!=":
		return l != r, nil
	case ">":
		return l > r, nil
	case "<":
		return l < r, nil
	case ">=":
		return l >= r, nil
	case "<=":
		return l <= r, nil
	}
	return false, fmt.Errorf("unsupported operator: %s", op)
}

// evalContains 评估 contains(attr, value) 函数。
func evalContains(expr string, env PolicyEnv) (bool, error) {
	inner := strings.TrimSpace(expr[9 : len(expr)-1])
	parts := splitFuncArgs(inner)
	if len(parts) != 2 {
		return false, fmt.Errorf("contains() requires 2 arguments")
	}
	haystack := resolveValue(parts[0], env)
	needle := resolveValue(parts[1], env)
	return strings.Contains(haystack, needle), nil
}

// evalIn 评估 "attr in [val1, val2]" 表达式。
func evalIn(left, right string, env PolicyEnv) (bool, error) {
	val := resolveValue(left, env)
	right = strings.TrimSpace(right)
	if !strings.HasPrefix(right, "[") || !strings.HasSuffix(right, "]") {
		return false, fmt.Errorf("in operator requires list on right side")
	}
	items := strings.Split(right[1:len(right)-1], ",")
	for _, item := range items {
		item = resolveValue(strings.TrimSpace(item), env)
		if val == item {
			return true, nil
		}
	}
	return false, nil
}

// splitFuncArgs 拆分函数参数，尊重引号。
func splitFuncArgs(s string) []string {
	var args []string
	depth := 0
	inQuote := byte(0)
	last := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			if inQuote == 0 {
				inQuote = s[i]
			} else if inQuote == s[i] {
				inQuote = 0
			}
		case '(':
			if inQuote == 0 {
				depth++
			}
		case ')':
			if inQuote == 0 {
				depth--
			}
		case ',':
			if inQuote == 0 && depth == 0 {
				args = append(args, strings.TrimSpace(s[last:i]))
				last = i + 1
			}
		}
	}
	args = append(args, strings.TrimSpace(s[last:]))
	return args
}
