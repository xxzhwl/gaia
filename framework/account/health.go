package account

import (
	"context"
)

// HealthStatus 健康检查结果。
type HealthStatus struct {
	Status    string                   `json:"status"`
	Checks    map[string]*HealthCheck  `json:"checks"`
}

// HealthCheck 单项健康检查结果。
type HealthCheck struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// HealthService 提供 SDK 各组件的健康检查。
type HealthService struct {
	m *Manager
}

// Check 对所有依赖组件执行健康检查，返回聚合结果。
// 所有组件正常时 Status 为 "ok"，否则为 "degraded"。
func (s *HealthService) Check(ctx context.Context) *HealthStatus {
	checks := make(map[string]*HealthCheck)
	allOK := true

	checks["database"] = s.checkDB(ctx)
	if checks["database"].Status != "ok" {
		allOK = false
	}

	checks["redis"] = s.checkCache(ctx)
	if checks["redis"].Status != "ok" {
		allOK = false
	}

	checks["jwt"] = s.checkJWT()
	if checks["jwt"].Status != "ok" {
		allOK = false
	}

	if s.m.cfg.NotifyProvider != nil {
		checks["notify_provider"] = &HealthCheck{Status: "ok", Message: "configured"}
	}

	status := "ok"
	if !allOK {
		status = "degraded"
	}
	return &HealthStatus{Status: status, Checks: checks}
}

func (s *HealthService) checkDB(ctx context.Context) *HealthCheck {
	sqlDB, err := s.m.db.DB()
	if err != nil {
		return &HealthCheck{Status: "error", Message: err.Error()}
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return &HealthCheck{Status: "error", Message: err.Error()}
	}
	return &HealthCheck{Status: "ok"}
}

func (s *HealthService) checkCache(ctx context.Context) *HealthCheck {
	if s.m.cache == nil {
		return &HealthCheck{Status: "ok", Message: "not configured"}
	}
	if pinger, ok := s.m.cache.(interface{ Ping(ctx context.Context) error }); ok {
		if err := pinger.Ping(ctx); err != nil {
			return &HealthCheck{Status: "error", Message: err.Error()}
		}
	}
	return &HealthCheck{Status: "ok"}
}

func (s *HealthService) checkJWT() *HealthCheck {
	if s.m.cfg.JWT.KeySet == nil && s.m.cfg.JWT.SecretKey == "" {
		return &HealthCheck{Status: "error", Message: "JWT not configured"}
	}
	return &HealthCheck{Status: "ok"}
}

