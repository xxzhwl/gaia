package domain

import "context"

// tenantContextKeyType 是租户上下文键的私有类型，避免跨包碰撞。
type tenantContextKeyType struct{}

// tenantContextKey 是在 context 中存放租户标识的键。
var tenantContextKey = tenantContextKeyType{}

// WithTenant 把租户标识写入 context，供下游运行时按租户隔离数据。
//
// 通常由传输层鉴权中间件/拦截器在解析出调用方身份后调用。空租户视为不写入。
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantContextKey, tenantID)
}

// TenantFromContext 从 context 中读取租户标识；不存在时返回空串与 false。
func TenantFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(tenantContextKey).(string)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}
