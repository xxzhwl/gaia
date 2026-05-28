package account

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// RiskDecision 风险评估后采取的行动。
type RiskDecision string

const (
	RiskAllow     RiskDecision = "allow"
	RiskChallenge  RiskDecision = "challenge"
	RiskDelay      RiskDecision = "delay"
	RiskBlock      RiskDecision = "block"
	RiskLock      RiskDecision = "lock"
)

// RiskAssessment 登录尝试的评估结果。
type RiskAssessment struct {
	Decision      RiskDecision
	Reason        string
	DelayDuration time.Duration
}

// RiskConfig 风险控制阈值配置。
type RiskConfig struct {
	MaxLoginFailuresPerUser int
	MaxLoginFailuresPerIP   int
	FailureWindow           time.Duration
	LockoutDuration         time.Duration
	BlockDuration           time.Duration
	EnableIPReputation      bool
}

func defaultRiskConfig() RiskConfig {
	return RiskConfig{
		MaxLoginFailuresPerUser: 10,
		MaxLoginFailuresPerIP:   50,
		FailureWindow:           15 * time.Minute,
		LockoutDuration:         30 * time.Minute,
		BlockDuration:           5 * time.Minute,
		EnableIPReputation:      false,
	}
}

type RiskService struct {
	m *Manager
}

// Assess 在验证凭证之前评估登录尝试的风险。
func (s *RiskService) Assess(ctx context.Context, tenantID, userID, ip string) (*RiskAssessment, error) {
	// 1. Check if account is already locked in DB
	if userID != "" {
		var user User
		if err := s.m.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", userID, tenantID).First(&user).Error; err == nil {
			if user.Status == UserStatusLocked {
				if user.LockedUntil != nil && time.Now().After(*user.LockedUntil) {
					_ = s.m.db.WithContext(ctx).Model(&User{}).Where("id = ?", user.ID).Updates(map[string]any{
						"status":       UserStatusNormal,
						"locked_until": nil,
					}).Error
					return &RiskAssessment{Decision: RiskAllow}, nil
				}
				return &RiskAssessment{Decision: RiskLock, Reason: "账号已锁定"}, nil
			}
		}
	}

	if s.m.cache == nil {
		return &RiskAssessment{Decision: RiskAllow}, nil
	}

	// 2. Check user failure count (only if we have a userID)
	if userID != "" {
		userKey := s.failureKey(tenantID, "user", userID)
		count, _, _ := s.m.cache.Get(ctx, userKey)
		c, _ := strconv.Atoi(count)
		rc := s.m.cfg.Risk
		if c >= rc.MaxLoginFailuresPerUser {
			return &RiskAssessment{
				Decision: RiskBlock,
				Reason:   fmt.Sprintf("登录失败次数过多（%d次），请稍后再试", c),
			}, nil
		}
		// Graded risk response at intermediate thresholds
		halfThreshold := rc.MaxLoginFailuresPerUser / 2
		if c >= halfThreshold+3 {
			return &RiskAssessment{
				Decision: RiskChallenge,
				Reason:   "登录异常，需要额外验证",
			}, nil
		}
		if c >= halfThreshold {
			return &RiskAssessment{
				Decision:      RiskDelay,
				Reason:        "登录请求过于频繁，请稍后再试",
				DelayDuration: time.Duration(c-halfThreshold+1) * time.Second,
			}, nil
		}
	}

	// 3. Check IP failure count
	if ip != "" {
		ipKey := s.failureKey(tenantID, "ip", ip)
		count, _, _ := s.m.cache.Get(ctx, ipKey)
		c, _ := strconv.Atoi(count)
		rc := s.m.cfg.Risk
		if c >= rc.MaxLoginFailuresPerIP {
			return &RiskAssessment{
				Decision: RiskBlock,
				Reason:   "来自该IP的登录请求过于频繁",
			}, nil
		}
	}

	return &RiskAssessment{Decision: RiskAllow}, nil
}

// RecordFailure 递增用户和/或 IP 的失败计数器。
// 使用原子的 Increment 操作避免并发竞争。
func (s *RiskService) RecordFailure(ctx context.Context, tenantID, userID, ip string) {
	if s.m.cache == nil {
		return
	}
	rc := s.m.cfg.Risk
	window := rc.FailureWindow

	if userID != "" {
		key := s.failureKey(tenantID, "user", userID)
		count, _ := s.m.cache.Increment(ctx, key, window)
		if count >= int64(rc.MaxLoginFailuresPerUser+3) {
			lockedUntil := time.Now().Add(rc.LockoutDuration)
			_ = s.m.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
				"status":       UserStatusLocked,
				"locked_until": &lockedUntil,
			}).Error
			_ = emitOutbox(s.m.db.WithContext(ctx), EventUserLocked, userID, map[string]any{
				"user_id": userID,
				"tenant_id": tenantID,
				"reason": "exceeded max login failures",
				"locked_until": lockedUntil,
			})
		}
	}
	if ip != "" {
		key := s.failureKey(tenantID, "ip", ip)
		_, _ = s.m.cache.Increment(ctx, key, window)
	}
}

// RecordSuccess 登录成功后重置失败计数器。
func (s *RiskService) RecordSuccess(ctx context.Context, tenantID, userID, ip string) {
	if s.m.cache == nil {
		return
	}
	if userID != "" {
		_ = s.m.cache.Del(ctx, s.failureKey(tenantID, "user", userID))
	}
	if ip != "" {
		_ = s.m.cache.Del(ctx, s.failureKey(tenantID, "ip", ip))
	}
}

func (s *RiskService) failureKey(tenantID, dimension, value string) string {
	return fmt.Sprintf("risk:fail:%s:%s:%s", tenantID, dimension, value)
}
