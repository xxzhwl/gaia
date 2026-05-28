package account

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// UserConsent 记录用户对隐私协议、服务条款等文档版本的签署。
type UserConsent struct {
	ID           string    `json:"id" gorm:"size:36;primaryKey"`
	TenantID     string    `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_user_consents,priority:1"`
	UserID       string    `json:"user_id" gorm:"size:36;not null;uniqueIndex:uniq_acct_user_consents,priority:2;index"`
	DocumentType string    `json:"document_type" gorm:"size:64;not null;uniqueIndex:uniq_acct_user_consents,priority:3"`
	Version      string    `json:"version" gorm:"size:64;not null;uniqueIndex:uniq_acct_user_consents,priority:4"`
	Locale       string    `json:"locale" gorm:"size:32"`
	Source       string    `json:"source" gorm:"size:64"`
	IP           string    `json:"ip" gorm:"size:64"`
	UserAgent    string    `json:"user_agent" gorm:"size:512"`
	ConsentedAt  time.Time `json:"consented_at" gorm:"not null;index"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (UserConsent) TableName() string { return "acct_user_consents" }

type ConsentService struct {
	m *Manager
}

type RecordConsentRequest struct {
	TenantID     string
	UserID       string
	DocumentType string
	Version      string
	Locale       string
	Source       string
	IP           string
	UserAgent    string
}

func (s *ConsentService) Record(ctx context.Context, req RecordConsentRequest) (*UserConsent, error) {
	if strings.TrimSpace(req.UserID) == "" {
		return nil, accountError(ErrInvalidArgument, "user_id 不能为空")
	}
	if strings.TrimSpace(req.DocumentType) == "" {
		return nil, accountError(ErrInvalidArgument, "document_type 不能为空")
	}
	if strings.TrimSpace(req.Version) == "" {
		return nil, accountError(ErrInvalidArgument, "version 不能为空")
	}
	now := time.Now()
	row := &UserConsent{
		ID:           newID(),
		TenantID:     s.m.tenantID(req.TenantID),
		UserID:       req.UserID,
		DocumentType: strings.TrimSpace(req.DocumentType),
		Version:      strings.TrimSpace(req.Version),
		Locale:       strings.TrimSpace(req.Locale),
		Source:       strings.TrimSpace(req.Source),
		IP:           req.IP,
		UserAgent:    req.UserAgent,
		ConsentedAt:  now,
	}
	err := s.m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"},
			{Name: "user_id"},
			{Name: "document_type"},
			{Name: "version"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"locale", "source", "ip", "user_agent", "consented_at", "updated_at"}),
	}).Create(row).Error
	if err != nil {
		return nil, err
	}
	s.m.audit(ctx, row.TenantID, row.UserID, "consent_record", "success", row.DocumentType+":"+row.Version, req.IP, req.UserAgent)
	return row, nil
}

func (s *ConsentService) List(ctx context.Context, tenantID, userID string) ([]UserConsent, error) {
	var rows []UserConsent
	if err := s.m.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", s.m.tenantID(tenantID), userID).
		Order("consented_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []UserConsent{}
	}
	return rows, nil
}
