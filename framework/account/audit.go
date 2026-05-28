package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AuditQueryRequest 审计日志查询参数。
type AuditQueryRequest struct {
	TenantID string
	UserID   string
	Event    string
	Status   string
	StartAt  *time.Time
	EndAt    *time.Time
	Keyword  string // 在 Reason 字段中模糊搜索
	Page     int
	PageSize int
}

func (r *AuditQueryRequest) defaults() {
	if r.Page < 1 {
		r.Page = 1
	}
	if r.PageSize < 1 || r.PageSize > 500 {
		r.PageSize = 50
	}
}

// AuditQueryResult 审计日志查询结果。
type AuditQueryResult struct {
	Items []AuditLog `json:"items"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
}

// AuditService 审计日志查询服务。
type AuditService struct {
	m *Manager
}

// Query 按条件查询审计日志，支持分页和时间范围过滤。
func (s *AuditService) Query(ctx context.Context, req AuditQueryRequest) (*AuditQueryResult, error) {
	req.defaults()
	q := s.m.db.WithContext(ctx).Model(&AuditLog{})

	if tenantID := s.m.tenantID(req.TenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if req.UserID != "" {
		q = q.Where("user_id = ?", req.UserID)
	}
	if req.Event != "" {
		q = q.Where("event = ?", req.Event)
	}
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	if req.StartAt != nil {
		q = q.Where("created_at >= ?", *req.StartAt)
	}
	if req.EndAt != nil {
		q = q.Where("created_at <= ?", *req.EndAt)
	}
	if req.Keyword != "" {
		q = q.Where("reason LIKE ?", "%"+req.Keyword+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count audit logs: %w", err)
	}

	var items []AuditLog
	offset := (req.Page - 1) * req.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("query audit logs: %w", err)
	}

	if items == nil {
		items = []AuditLog{}
	}
	return &AuditQueryResult{Items: items, Total: total, Page: req.Page}, nil
}

// GetByID 根据 ID 获取单条审计日志。
func (s *AuditService) GetByID(ctx context.Context, id string) (*AuditLog, error) {
	var log AuditLog
	if err := s.m.db.WithContext(ctx).Where("id = ?", id).First(&log).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, accountError(ErrInvalidArgument, "审计日志不存在")
		}
		return nil, fmt.Errorf("get audit log: %w", err)
	}
	return &log, nil
}

// ListEvents 返回指定时间范围内出现的事件名称列表（去重）。
func (s *AuditService) ListEvents(ctx context.Context, tenantID string, startAt, endAt time.Time) ([]string, error) {
	var events []string
	q := s.m.db.WithContext(ctx).Model(&AuditLog{}).
		Where("tenant_id = ? AND created_at >= ? AND created_at <= ?", s.m.tenantID(tenantID), startAt, endAt).
		Distinct().Pluck("event", &events)
	return events, q.Error
}

// ArchiveOldLogs 将指定时间之前的热数据归档到 audit_log_archives 表。
// 每个批次处理 batchSize 条记录，避免长事务。
func (s *AuditService) ArchiveOldLogs(ctx context.Context, before time.Time, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = 500
	}
	var totalArchived int64
	for {
		var batch []AuditLog
		if err := s.m.db.WithContext(ctx).Where("created_at < ?", before).Order("created_at ASC").Limit(batchSize).Find(&batch).Error; err != nil {
			return totalArchived, err
		}
		if len(batch) == 0 {
			break
		}
		now := time.Now()
		archives := make([]AuditLogArchive, len(batch))
		for i, log := range batch {
			archives[i] = AuditLogArchive{
				ID:         log.ID,
				TenantID:   log.TenantID,
				UserID:     log.UserID,
				Event:      log.Event,
				IP:         log.IP,
				UserAgent:  log.UserAgent,
				Status:     log.Status,
				Reason:     log.Reason,
				CreatedAt:  log.CreatedAt,
				ArchivedAt: now,
			}
		}
		if err := s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&archives).Error; err != nil {
				return err
			}
			ids := make([]string, len(batch))
			for i, log := range batch {
				ids[i] = log.ID
			}
			return tx.Where("id IN ?", ids).Delete(&AuditLog{}).Error
		}); err != nil {
			return totalArchived, fmt.Errorf("archive batch failed: %w", err)
		}
		totalArchived += int64(len(batch))
	}
	return totalArchived, nil
}

// QueryArchived 查询归档审计日志，支持分页和时间范围过滤。
func (s *AuditService) QueryArchived(ctx context.Context, req AuditQueryRequest) (*AuditQueryResult, error) {
	req.defaults()
	q := s.m.db.WithContext(ctx).Model(&AuditLogArchive{})

	if tenantID := s.m.tenantID(req.TenantID); tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if req.UserID != "" {
		q = q.Where("user_id = ?", req.UserID)
	}
	if req.Event != "" {
		q = q.Where("event = ?", req.Event)
	}
	if req.Status != "" {
		q = q.Where("status = ?", req.Status)
	}
	if req.StartAt != nil {
		q = q.Where("created_at >= ?", *req.StartAt)
	}
	if req.EndAt != nil {
		q = q.Where("created_at <= ?", *req.EndAt)
	}
	if req.Keyword != "" {
		q = q.Where("reason LIKE ?", "%"+req.Keyword+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("count archived audit logs: %w", err)
	}

	var items []AuditLogArchive
	offset := (req.Page - 1) * req.PageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(req.PageSize).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("query archived audit logs: %w", err)
	}

	result := &AuditQueryResult{Total: total, Page: req.Page}
	result.Items = make([]AuditLog, len(items))
	for i, item := range items {
		result.Items[i] = AuditLog{
			ID:        item.ID,
			TenantID:  item.TenantID,
			UserID:    item.UserID,
			Event:     item.Event,
			IP:        item.IP,
			UserAgent: item.UserAgent,
			Status:    item.Status,
			Reason:    item.Reason,
			CreatedAt: item.CreatedAt,
		}
	}
	return result, nil
}

// RestoreFromArchive 将单条归档审计日志恢复到主表。
func (s *AuditService) RestoreFromArchive(ctx context.Context, id string) error {
	var archived AuditLogArchive
	if err := s.m.db.WithContext(ctx).Where("id = ?", id).First(&archived).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return accountError(ErrInvalidArgument, "归档审计日志不存在")
		}
		return fmt.Errorf("find archived audit log: %w", err)
	}
	log := AuditLog{
		ID:        archived.ID,
		TenantID:  archived.TenantID,
		UserID:    archived.UserID,
		Event:     archived.Event,
		IP:        archived.IP,
		UserAgent: archived.UserAgent,
		Status:    archived.Status,
		Reason:    archived.Reason,
		CreatedAt: archived.CreatedAt,
	}
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&log).Error; err != nil {
			return err
		}
		return tx.Delete(&archived).Error
	})
}
