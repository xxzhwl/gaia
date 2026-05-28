package account

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AuthorizedApp 记录用户已授权访问的 OIDC 客户端。
type AuthorizedApp struct {
	ID               string     `json:"id" gorm:"size:36;primaryKey"`
	TenantID         string     `json:"tenant_id" gorm:"size:64;not null;uniqueIndex:uniq_acct_authorized_apps,priority:1"`
	UserID           string     `json:"user_id" gorm:"size:36;not null;uniqueIndex:uniq_acct_authorized_apps,priority:2;index"`
	ClientID         string     `json:"client_id" gorm:"size:128;not null;uniqueIndex:uniq_acct_authorized_apps,priority:3;index"`
	Scopes           string     `json:"scopes" gorm:"size:512"`
	LastAuthorizedAt time.Time  `json:"last_authorized_at" gorm:"not null"`
	RevokedAt        *time.Time `json:"revoked_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (AuthorizedApp) TableName() string { return "acct_authorized_apps" }

type AuthorizedAppInfo struct {
	ID               string     `json:"id"`
	ClientID         string     `json:"client_id"`
	ClientName       string     `json:"client_name"`
	RedirectURIs     []string   `json:"redirect_uris"`
	Scopes           []string   `json:"scopes"`
	LastAuthorizedAt time.Time  `json:"last_authorized_at"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
}

func (s *IdpService) recordAuthorizedApp(ctx context.Context, tenantID, userID, clientID, scopes string) error {
	if userID == "" || clientID == "" {
		return nil
	}
	now := time.Now()
	row := &AuthorizedApp{
		ID:               newID(),
		TenantID:         s.m.tenantID(tenantID),
		UserID:           userID,
		ClientID:         clientID,
		Scopes:           joinScopes(splitScopes(scopes)),
		LastAuthorizedAt: now,
	}
	return s.m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"},
			{Name: "user_id"},
			{Name: "client_id"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"scopes":             row.Scopes,
			"last_authorized_at": now,
			"revoked_at":         nil,
			"updated_at":         now,
		}),
	}).Create(row).Error
}

// ListAuthorizedApps 返回当前用户已授权的 OIDC 客户端。
func (s *IdpService) ListAuthorizedApps(ctx context.Context, tenantID, userID string) ([]AuthorizedAppInfo, error) {
	var rows []AuthorizedApp
	if err := s.m.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND revoked_at IS NULL", s.m.tenantID(tenantID), userID).
		Order("last_authorized_at DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AuthorizedAppInfo, 0, len(rows))
	for _, row := range rows {
		var client IdpClient
		name := row.ClientID
		var redirectURIs []string
		if err := s.m.db.WithContext(ctx).Where("client_id = ?", row.ClientID).First(&client).Error; err == nil {
			name = client.Name
			_ = json.Unmarshal([]byte(client.RedirectURIs), &redirectURIs)
		}
		out = append(out, AuthorizedAppInfo{
			ID:               row.ID,
			ClientID:         row.ClientID,
			ClientName:       name,
			RedirectURIs:     redirectURIs,
			Scopes:           splitScopes(row.Scopes),
			LastAuthorizedAt: row.LastAuthorizedAt,
			RevokedAt:        row.RevokedAt,
		})
	}
	return out, nil
}

// RevokeAuthorizedApp 撤销用户对指定 OIDC 客户端的授权，并清理未使用授权码。
func (s *IdpService) RevokeAuthorizedApp(ctx context.Context, tenantID, userID, clientID string) error {
	if strings.TrimSpace(clientID) == "" {
		return accountError(ErrInvalidArgument, "client_id 不能为空")
	}
	tenantID = s.m.tenantID(tenantID)
	now := time.Now()
	return s.m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&AuthorizedApp{}).
			Where("tenant_id = ? AND user_id = ? AND client_id = ? AND revoked_at IS NULL", tenantID, userID, clientID).
			Updates(map[string]any{"revoked_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return accountError(ErrInvalidArgument, "授权应用不存在或已撤销")
		}
		if err := tx.Where("tenant_id = ? AND user_id = ? AND client_id = ?", tenantID, userID, clientID).
			Delete(&AuthorizationCode{}).Error; err != nil {
			return err
		}
		s.m.audit(ctx, tenantID, userID, "authorized_app_revoke", "success", "client: "+clientID, "", "")
		return nil
	})
}
