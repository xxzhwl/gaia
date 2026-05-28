package account

import "testing"

func TestBuildPolicyEnv(t *testing.T) {
	req := AuthzRequest{
		Subject:    &Principal{UserID: "u1", TenantID: "t1", Roles: []string{"admin"}},
		ResourceType: "order",
		ResourceID:   "123",
		OwnerID:      "owner1",
		Permission:   "order:read",
	}
	env := buildPolicyEnv(req)
	if env.Subject["user_id"] != "u1" {
		t.Fatalf("expected u1, got %s", env.Subject["user_id"])
	}
	if env.Subject["tenant_id"] != "t1" {
		t.Fatalf("expected t1, got %s", env.Subject["tenant_id"])
	}
	if env.Resource["type"] != "order" {
		t.Fatalf("expected order, got %s", env.Resource["type"])
	}
	if env.Resource["id"] != "123" {
		t.Fatalf("expected 123, got %s", env.Resource["id"])
	}
	if env.Resource["owner_id"] != "owner1" {
		t.Fatalf("expected owner1, got %s", env.Resource["owner_id"])
	}
	if env.Subject["roles"] != "admin" {
		t.Fatalf("expected admin, got %s", env.Subject["roles"])
	}
}

func TestBuildPolicyEnvNoRoles(t *testing.T) {
	req := AuthzRequest{
		Subject: &Principal{UserID: "u1", TenantID: "t1"},
	}
	env := buildPolicyEnv(req)
	if _, ok := env.Subject["roles"]; ok {
		t.Fatal("expected no roles key when roles is empty")
	}
}

func TestSplitLogical(t *testing.T) {
	tests := []struct {
		expr string
		op   string
		want int
	}{
		{"a == b && c == d", "&&", 2},
		{"a == b || c == d", "||", 2},
		{"(a && b) || c", "||", 2},
		{"single", "&&", 1},
		{"", "&&", 0},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			parts := splitLogical(tt.expr, tt.op)
			if len(parts) != tt.want {
				t.Errorf("splitLogical(%q, %q) got %d parts, want %d: %v", tt.expr, tt.op, len(parts), tt.want, parts)
			}
		})
	}
}

func TestResolveAttr(t *testing.T) {
	env := PolicyEnv{
		Subject:  map[string]string{"user_id": "u1", "roles": "admin,user"},
		Resource: map[string]string{"type": "order", "id": "123", "owner_id": "owner1"},
	}
	tests := []struct {
		path    string
		want   string
		wantOK bool
	}{
		{"subject.user_id", "u1", true},
		{"subject.roles", "admin,user", true},
		{"resource.type", "order", true},
		{"resource.id", "123", true},
		{"resource.owner_id", "owner1", true},
		{"subject.nonexistent", "", false},
		{"resource.nonexistent", "", false},
		{"invalid", "", false},
		{"too.many.parts", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := resolveAttr(tt.path, env)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("resolveAttr(%q) = (%q, %v), want (%q, %v)", tt.path, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestResolveValue(t *testing.T) {
	env := PolicyEnv{
		Subject:  map[string]string{"user_id": "u1"},
		Resource: map[string]string{"owner_id": "owner1"},
	}
	tests := []struct {
		val  string
		want string
	}{
		{`"literal"`, "literal"},
		{`'single'`, "single"},
		{"subject.user_id", "u1"},
		{"resource.owner_id", "owner1"},
		{"42", "42"},
		{"nonexistent", "nonexistent"}, // unresolvable returns as-is
	}
	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			if got := resolveValue(tt.val, env); got != tt.want {
				t.Errorf("resolveValue(%q) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestEvalCompare(t *testing.T) {
	env := PolicyEnv{
		Subject:  map[string]string{"user_id": "u1"},
		Resource: map[string]string{"owner_id": "u1", "type": "order"},
	}
	tests := []struct {
		left    string
		op      string
		right   string
		want    bool
		wantErr bool
	}{
		{"subject.user_id", "==", "resource.owner_id", true, false},
		{"subject.user_id", "!=", "resource.owner_id", false, false},
		{`"a"`, "==", `"a"`, true, false},
		{`"a"`, "==", `"b"`, false, false},
		{`"b"`, ">", `"a"`, true, false},
		{`"a"`, "<", `"b"`, true, false},
		{`"a"`, ">=", `"a"`, true, false},
		{`"a"`, "<=", `"a"`, true, false},
		{`"a"`, "^^", `"b"`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.left+" "+tt.op, func(t *testing.T) {
			got, err := evalCompare(tt.left, tt.op, tt.right, env)
			if (err != nil) != tt.wantErr {
				t.Errorf("evalCompare() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err == nil && got != tt.want {
				t.Errorf("evalCompare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalContains(t *testing.T) {
	env := PolicyEnv{
		Subject:  map[string]string{"roles": "admin,user,editor"},
		Resource: map[string]string{"type": "order"},
	}
	tests := []struct {
		expr    string
		want    bool
		wantErr bool
	}{
		{`contains(subject.roles, "admin")`, true, false},
		{`contains(subject.roles, "superadmin")`, false, false},
		{`contains(subject.roles, "editor")`, true, false},
		{`contains(resource.type, "order")`, true, false},
		{`contains(resource.type, "document")`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := evalContains(tt.expr, env)
			if (err != nil) != tt.wantErr {
				t.Errorf("evalContains(%q) error = %v, wantErr = %v", tt.expr, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("evalContains(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestEvalIn(t *testing.T) {
	env := PolicyEnv{
		Subject:  map[string]string{"user_id": "u1"},
		Resource: map[string]string{"id": "123"},
	}
	tests := []struct {
		left    string
		right   string
		want    bool
		wantErr bool
	}{
		{`subject.user_id`, `["u1", "u2"]`, true, false},
		{`subject.user_id`, `["u3", "u4"]`, false, false},
		{`resource.id`, `["123", "456"]`, true, false},
		{`resource.id`, `"not-a-list"`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.left+" in", func(t *testing.T) {
			got, err := evalIn(tt.left, tt.right, env)
			if (err != nil) != tt.wantErr {
				t.Errorf("evalIn() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("evalIn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalExpression(t *testing.T) {
	env := PolicyEnv{
		Subject:  map[string]string{"user_id": "u1", "roles": "admin,user"},
		Resource: map[string]string{"type": "order", "id": "123", "owner_id": "u1"},
	}
	tests := []struct {
		expr    string
		want    bool
		wantErr bool
	}{
		// Simple equality
		{`subject.user_id == resource.owner_id`, true, false},
		{`subject.user_id != resource.owner_id`, false, false},
		// Contains
		{`contains(subject.roles, "admin")`, true, false},
		{`contains(subject.roles, "superadmin")`, false, false},
		// In
		{`resource.type in ["order", "invoice"]`, true, false},
		{`resource.type in ["document", "note"]`, false, false},
		// AND
		{`subject.user_id == resource.owner_id && resource.type == "order"`, true, false},
		{`subject.user_id == resource.owner_id && resource.type == "document"`, false, false},
		// OR
		{`resource.type == "order" || resource.type == "document"`, true, false},
		{`resource.type == "invoice" || resource.type == "document"`, false, false},
		// NOT
		{`!contains(subject.roles, "guest")`, true, false},
		// Parenthesized
		{`(subject.user_id == resource.owner_id)`, true, false},
		// AND with parenthesized OR
		{`resource.type == "order" && (subject.user_id == resource.owner_id || contains(subject.roles, "admin"))`, true, false},
		// Empty expression (matches everything)
		{``, true, false},
		// Literal comparison
		{`"admin" == "admin"`, true, false},
		{`"admin" == "user"`, false, false},
		{`true`, false, true}, // unsupported
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := evalExpression(tt.expr, env)
			if (err != nil) != tt.wantErr {
				t.Errorf("evalExpression(%q) error = %v, wantErr = %v", tt.expr, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("evalExpression(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestMatchResourceAction(t *testing.T) {
	tests := []struct {
		name string
		p    Policy
		req  AuthzRequest
		want bool
	}{
		{"exact match", Policy{ResourceType: "order", Action: "read"}, AuthzRequest{ResourceType: "order", Permission: "order:read"}, true},
		{"wildcard resource", Policy{ResourceType: "*", Action: "read"}, AuthzRequest{ResourceType: "document", Permission: "document:read"}, true},
		{"wildcard action", Policy{ResourceType: "order", Action: "*"}, AuthzRequest{ResourceType: "order", Permission: "order:write"}, true},
		{"wildcard both", Policy{ResourceType: "*", Action: "*"}, AuthzRequest{ResourceType: "anything", Permission: "anything:do"}, true},
		{"no match resource", Policy{ResourceType: "order", Action: "read"}, AuthzRequest{ResourceType: "document", Permission: "document:read"}, false},
		{"no match action", Policy{ResourceType: "order", Action: "write"}, AuthzRequest{ResourceType: "order", Permission: "order:read"}, false},
		{"permission without prefix", Policy{ResourceType: "order", Action: "read"}, AuthzRequest{ResourceType: "order", Permission: "read"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchResourceAction(tt.p, tt.req); got != tt.want {
				t.Errorf("matchResourceAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitFuncArgs(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`subject.roles, "admin"`, 2},
		{`a, b, c`, 3},
		{`"hello, world", x`, 2},
		{`single`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			args := splitFuncArgs(tt.input)
			if len(args) != tt.want {
				t.Errorf("splitFuncArgs(%q) = %d args, want %d", tt.input, len(args), tt.want)
			}
		})
	}
}
