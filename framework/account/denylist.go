package account

import (
	"context"
	"fmt"
	"time"
)

// AccessTokenDenylist 存储用于立即撤销高风险访问令牌的 JTI 条目。
// 不会在每个请求上都检查此列表——仅在配置启用时检查，因为访问令牌设计上具有较短的 TTL。
type AccessTokenDenylist struct {
	JTI       string    `gorm:"size:64;primaryKey"`
	UserID    string    `gorm:"size:36;not null;index"`
	Reason    string    `gorm:"size:64"`
	ExpiresAt time.Time `gorm:"not null;index:idx_acct_denylist_expires"`
	CreatedAt time.Time
}

func (AccessTokenDenylist) TableName() string { return "acct_access_token_denylist" }

// RevokeAccessToken 将 JTI 添加到黑名单，使 Validate 会拒绝该令牌。
func (s *AuthService) RevokeAccessToken(ctx context.Context, jti, userID, reason string) error {
	if s.m.cache != nil {
		_ = s.m.cache.Set(ctx, denylistCacheKey(jti), "1", s.m.cfg.AccessTokenTTL)
	}
	return s.m.db.WithContext(ctx).Create(&AccessTokenDenylist{
		JTI:       jti,
		UserID:    userID,
		Reason:    reason,
		ExpiresAt: time.Now().Add(s.m.cfg.AccessTokenTTL),
	}).Error
}

// isDenied checks whether a JTI has been revoked.
func (s *AuthService) isDenied(ctx context.Context, jti string) (bool, error) {
	if s.m.cache != nil {
		if v, ok, err := s.m.cache.Get(ctx, denylistCacheKey(jti)); err == nil && ok {
			return v == "1", nil
		}
	}
	var count int64
	err := s.m.db.WithContext(ctx).Model(&AccessTokenDenylist{}).
		Where("jti = ?", jti).Count(&count).Error
	denied := count > 0
	if err == nil && s.m.cache != nil {
		value := "0"
		ttl := s.m.cfg.DenylistCacheTTL
		if denied {
			value = "1"
			ttl = s.m.cfg.AccessTokenTTL
		}
		_ = s.m.cache.Set(ctx, denylistCacheKey(jti), value, ttl)
	}
	return denied, err
}

func denylistCacheKey(jti string) string {
	return fmt.Sprintf("deny:jti:%s", jti)
}

// CleanupExpiredDenylist 清理黑名单中已过期的条目。
func (m *Manager) CleanupExpiredDenylist(ctx context.Context) error {
	return m.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&AccessTokenDenylist{}).Error
}
