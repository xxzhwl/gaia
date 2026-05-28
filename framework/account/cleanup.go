package account

import (
	"context"
	"time"

	"github.com/xxzhwl/gaia"
)

const (
	defaultCleanupInterval = 10 * time.Minute
	cleanupLockTTL         = 60 * time.Second
	cleanupLockKey         = "cleanup:lock"
)

// StartCleanupTask 启动后台协程，定期清理过期数据。
// 清理内容包括：已过期的 refresh token、会话（标记为 expired）、验证挑战、黑名单条目、
// MFA 挑战和 outbox 事件。
// 使用分布式锁防止多实例并发执行。
// 清理间隔可通过 interval 参数设置，传入 0 则使用默认值（10 分钟）。
// 返回一个 cancel 函数，调用后停止清理协程。
func (m *Manager) StartCleanupTask(ctx context.Context, interval time.Duration) context.CancelFunc {
	if interval <= 0 {
		interval = defaultCleanupInterval
	}

	cleanupCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		gaia.InfoF("[account] cleanup task started, interval=%v", interval)

		for {
			select {
			case <-cleanupCtx.Done():
				gaia.InfoF("[account] cleanup task stopped")
				return
			case <-ticker.C:
				if err := m.cleanupAll(cleanupCtx); err != nil {
					gaia.WarnF("[account] cleanup error: %v", err)
				}
			}
		}
	}()
	return cancel
}

// cleanupAll 执行全部过期数据的清理，使用分布式锁防止并发。
func (m *Manager) cleanupAll(ctx context.Context) error {
	if !m.acquireCleanupLock(ctx) {
		gaia.WarnF("[account] cleanup skipped — another instance is already running")
		return nil
	}
	defer m.releaseCleanupLock(ctx)

	now := time.Now()

	if err := m.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&RefreshToken{}).Error; err != nil {
		recordDBError(ctx)
		return err
	}

	if err := m.db.WithContext(ctx).Model(&Session{}).
		Where("expires_at < ? AND status = ?", now, SessionActive).
		Update("status", SessionExpired).Error; err != nil {
		recordDBError(ctx)
		return err
	}

	if err := m.verification.CleanupExpired(ctx); err != nil {
		recordDBError(ctx)
		return err
	}

	if err := m.CleanupExpiredDenylist(ctx); err != nil {
		recordDBError(ctx)
		return err
	}

	// Clean expired MFA challenges
	if err := m.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&MFAChallenge{}).Error; err != nil {
		recordDBError(ctx)
		return err
	}

	// Clean already-sent / expired outbox events
	if err := m.db.WithContext(ctx).Where("status IN ? AND created_at < ?",
		[]string{outboxSent, outboxIgnored, outboxFailed}, now.Add(-24*time.Hour)).Delete(&OutboxEvent{}).Error; err != nil {
		recordDBError(ctx)
		return err
	}

	// Archive audit logs before retention cleanup
	if archiveDays := m.cfg.Audit.ArchiveRetentionDays; archiveDays > 0 {
		archiveCutoff := now.AddDate(0, 0, -archiveDays)
		if n, err := m.auditSvc.ArchiveOldLogs(ctx, archiveCutoff, 500); err != nil {
			recordDBError(ctx)
			gaia.WarnF("[account] archive audit logs failed: %v", err)
		} else if n > 0 {
			gaia.InfoF("[account] archived %d audit logs older than %d days", n, archiveDays)
		}
	}

	// Clean audit logs beyond retention period
	if retention := m.cfg.Audit.RetentionDays; retention > 0 {
		cutoff := now.AddDate(0, 0, -retention)
		if err := m.db.WithContext(ctx).Where("created_at < ?", cutoff).Delete(&AuditLog{}).Error; err != nil {
			recordDBError(ctx)
			return err
		}
	}

	return nil
}

// acquireCleanupLock 尝试获取分布式清理锁。
// 使用 Redis INCR 实现：只有第一个递增到 1 的实例获得锁。
func (m *Manager) acquireCleanupLock(ctx context.Context) bool {
	if m.cache == nil {
		return true // 无缓存时允许执行（单实例模式）
	}
	val, err := m.cache.Increment(ctx, cleanupLockKey, cleanupLockTTL)
	if err != nil {
		gaia.WarnF("[account] cleanup lock check failed: %v", err)
		return true // 锁检查失败时允许执行，避免清理完全停滞
	}
	return val == 1
}

// releaseCleanupLock 释放分布式清理锁。
func (m *Manager) releaseCleanupLock(ctx context.Context) {
	if m.cache == nil {
		return
	}
	_ = m.cache.Del(ctx, cleanupLockKey)
}
