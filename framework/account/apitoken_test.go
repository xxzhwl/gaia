package account

import (
	"reflect"
	"testing"
	"time"
)

func TestAPITokenScopesNormalizeAndSplit(t *testing.T) {
	got := joinScopes([]string{"read:user", " ", "write:user", "read:user"})
	if got != "read:user,write:user" {
		t.Fatalf("unexpected joined scopes: %q", got)
	}
	if scopes := splitScopes(got); !reflect.DeepEqual(scopes, []string{"read:user", "write:user"}) {
		t.Fatalf("unexpected split scopes: %#v", scopes)
	}
	if scopes := splitScopes(""); len(scopes) != 0 {
		t.Fatalf("expected empty scopes, got %#v", scopes)
	}
}

func TestAPITokenInfoRedactsHash(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	lastUsedAt := time.Now()
	info := apiTokenInfo(PersonalAccessToken{
		ID:          "tok_1",
		TenantID:    "default",
		UserID:      "u1",
		Name:        "cli",
		TokenPrefix: personalAccessTokenPrefix + "abc",
		TokenHash:   "secret-hash",
		Scopes:      "read,write",
		Status:      "active",
		ExpiresAt:   &expiresAt,
		LastUsedAt:  &lastUsedAt,
	})
	if info.ID != "tok_1" || info.TokenPrefix == "" {
		t.Fatalf("unexpected token info: %#v", info)
	}
	if !reflect.DeepEqual(info.Scopes, []string{"read", "write"}) {
		t.Fatalf("unexpected scopes: %#v", info.Scopes)
	}
}

func TestAPITokenAllowsPermission(t *testing.T) {
	tests := []struct {
		name       string
		scopes     []string
		permission string
		want       bool
	}{
		{"exact", []string{"user:read"}, "user:read", true},
		{"resource wildcard", []string{"user:*"}, "user:delete", true},
		{"global wildcard", []string{"*"}, "admin:write", true},
		{"empty scopes", nil, "user:read", false},
		{"different permission", []string{"user:read"}, "user:write", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiTokenAllowsPermission(tt.scopes, tt.permission); got != tt.want {
				t.Fatalf("apiTokenAllowsPermission() = %v, want %v", got, tt.want)
			}
		})
	}
}
