package account

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SessionService 会话管理服务，提供活跃会话查询和远程下线能力。
type SessionService struct {
	m *Manager
}

// SessionInfo 返回给客户端的会话摘要，隐藏内部字段。
type SessionInfo struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"device_id"`
	IP        string    `json:"ip"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IsCurrent bool      `json:"is_current"`
}

// List 返回用户的活跃会话列表。currentSessionID 会被标记为当前会话。
func (s *SessionService) List(ctx context.Context, tenantID, userID, currentSessionID string) ([]SessionInfo, error) {
	var sessions []Session
	if err := s.m.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND status = ?", s.m.tenantID(tenantID), userID, SessionActive).
		Order("created_at DESC").
		Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	infos := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		infos = append(infos, SessionInfo{
			ID:        s.ID,
			DeviceID:  s.DeviceID,
			IP:        s.IP,
			Status:    s.Status,
			CreatedAt: s.CreatedAt,
			ExpiresAt: s.ExpiresAt,
			IsCurrent: s.ID == currentSessionID,
		})
	}
	return infos, nil
}

// Revoke 撤销指定会话及其关联的刷新令牌。
// 如果会话不属于该用户，返回错误。
func (s *SessionService) Revoke(ctx context.Context, tenantID, userID, sessionID string) error {
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var session Session
		if err := tx.Where("id = ? AND tenant_id = ?", sessionID, s.m.tenantID(tenantID)).First(&session).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return accountError(ErrInvalidArgument, "会话不存在")
			}
			return fmt.Errorf("find session: %w", err)
		}
		if session.UserID != userID {
			return accountError(ErrPermissionDenied, "无权操作此会话")
		}
		now := time.Now()
		if err := tx.Model(&session).Updates(map[string]any{
			"status":     SessionRevoked,
			"revoked_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&RefreshToken{}).Where("session_id = ?", sessionID).
			Update("status", RefreshRevoked).Error; err != nil {
			return err
		}
		s.m.auth.invalidatePrincipalCache(ctx, sessionID)
		s.m.audit(ctx, tenantID, userID, "session_revoke", "success", "session: "+sessionID, "", "")
		return nil
	})
}

// RevokeOther 撤销用户除当前会话外的所有活跃会话。
func (s *SessionService) RevokeOther(ctx context.Context, tenantID, userID, currentSessionID string) (int64, error) {
	now := time.Now()
	tenantID = s.m.tenantID(tenantID)

	var revokedCount int64

	err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var targetIDs []string
		if err := tx.Model(&Session{}).
			Where("tenant_id = ? AND user_id = ? AND id != ? AND status = ?", tenantID, userID, currentSessionID, SessionActive).
			Pluck("id", &targetIDs).Error; err != nil {
			return err
		}
		if len(targetIDs) == 0 {
			return nil
		}
		if err := tx.Model(&Session{}).Where("id IN ?", targetIDs).Updates(map[string]any{
			"status":     SessionRevoked,
			"revoked_at": now,
		}).Error; err != nil {
			return err
		}
		res := tx.Model(&RefreshToken{}).Where("session_id IN ?", targetIDs).
			Update("status", RefreshRevoked)
		if res.Error != nil {
			return res.Error
		}
		revokedCount = res.RowsAffected
		tx.Model(&Session{}).Where("id IN ?", targetIDs).Count(&revokedCount)
		for _, sid := range targetIDs {
			s.m.auth.invalidatePrincipalCache(ctx, sid)
		}
		return nil
	})

	if err != nil {
		return 0, err
	}
	return revokedCount, nil
}

// RevokeByID 按 session ID 直接撤销会话（管理员操作，不校验 userID）。
func (s *SessionService) RevokeByID(ctx context.Context, tenantID, sessionID string) error {
	now := time.Now()
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Session{}).
			Where("id = ? AND tenant_id = ?", sessionID, s.m.tenantID(tenantID)).
			Updates(map[string]any{"status": SessionRevoked, "revoked_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return accountError(ErrInvalidArgument, "会话不存在")
		}
		if err := tx.Model(&RefreshToken{}).Where("session_id = ?", sessionID).
			Update("status", RefreshRevoked).Error; err != nil {
			return err
		}
		s.m.auth.invalidatePrincipalCache(ctx, sessionID)
		s.m.audit(ctx, tenantID, "", "session_revoke_admin", "success", "session: "+sessionID, "", "")
		return nil
	})
}
