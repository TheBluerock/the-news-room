package casbinx

import "testing"

// rules mirror the seed data in 001_init_schema.up.sql + 002_add_password_auth.up.sql.
func seedRules() [][]string {
	return [][]string{
		{"p", "admin", "*", "*"},
		{"p", "editor", "articles", "read"},
		{"p", "editor", "articles", "approve"},
		{"p", "editor", "articles", "correct"},
		{"p", "journalist", "articles", "read"},
		{"p", "viewer", "articles", "read"},
		{"g", "admin@newsroom.dev", "admin"},
		{"g", "editor.italy@newsroom.dev", "editor"},
		{"g", "viewer@newsroom.dev", "viewer"},
	}
}

func TestNewEnforcer_AdminWildcard(t *testing.T) {
	e, err := NewEnforcer(seedRules())
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	cases := []struct {
		obj, act string
	}{
		{"articles", "read"},
		{"articles", "approve"},
		{"articles", "delete"},
		{"users", "create"},
		{"audit", "read"},
	}
	for _, c := range cases {
		ok, err := e.Enforce("admin@newsroom.dev", c.obj, c.act)
		if err != nil {
			t.Fatalf("enforce err: %v", err)
		}
		if !ok {
			t.Errorf("admin denied %s:%s", c.obj, c.act)
		}
	}
}

func TestNewEnforcer_EditorAllowedActions(t *testing.T) {
	e, _ := NewEnforcer(seedRules())
	for _, act := range []string{"read", "approve", "correct"} {
		ok, _ := e.Enforce("editor.italy@newsroom.dev", "articles", act)
		if !ok {
			t.Errorf("editor denied articles:%s", act)
		}
	}
}

func TestNewEnforcer_EditorDeniedActions(t *testing.T) {
	e, _ := NewEnforcer(seedRules())
	for _, act := range []string{"delete", "publish-direct", "purge"} {
		ok, _ := e.Enforce("editor.italy@newsroom.dev", "articles", act)
		if ok {
			t.Errorf("editor allowed articles:%s (should deny)", act)
		}
	}
}

func TestNewEnforcer_EditorDeniedOtherResources(t *testing.T) {
	e, _ := NewEnforcer(seedRules())
	for _, obj := range []string{"users", "audit", "billing"} {
		ok, _ := e.Enforce("editor.italy@newsroom.dev", obj, "read")
		if ok {
			t.Errorf("editor allowed %s:read (should deny)", obj)
		}
	}
}

func TestNewEnforcer_ViewerReadOnly(t *testing.T) {
	e, _ := NewEnforcer(seedRules())
	ok, _ := e.Enforce("viewer@newsroom.dev", "articles", "read")
	if !ok {
		t.Error("viewer denied articles:read")
	}
	for _, act := range []string{"approve", "correct", "delete"} {
		ok, _ := e.Enforce("viewer@newsroom.dev", "articles", act)
		if ok {
			t.Errorf("viewer allowed articles:%s", act)
		}
	}
}

func TestNewEnforcer_UnknownUserDenied(t *testing.T) {
	e, _ := NewEnforcer(seedRules())
	ok, _ := e.Enforce("ghost@nowhere.dev", "articles", "read")
	if ok {
		t.Error("unknown user must be denied")
	}
}

func TestNewEnforcer_EmptyRules(t *testing.T) {
	e, err := NewEnforcer(nil)
	if err != nil {
		t.Fatalf("NewEnforcer with nil: %v", err)
	}
	ok, _ := e.Enforce("anyone", "anything", "any")
	if ok {
		t.Error("empty enforcer must deny all")
	}
}

func TestNewEnforcer_SkipsShortRows(t *testing.T) {
	// Rules with len < 2 should be silently dropped (defensive against bad data).
	rules := [][]string{
		{"p"},
		{"p", "admin", "*", "*"},
		{"g", "u@x.dev", "admin"},
	}
	e, err := NewEnforcer(rules)
	if err != nil {
		t.Fatalf("NewEnforcer: %v", err)
	}
	ok, _ := e.Enforce("u@x.dev", "anything", "any")
	if !ok {
		t.Error("admin grant should still work despite stub row")
	}
}

func TestResolveRoles_GetRolesForUser(t *testing.T) {
	e, _ := NewEnforcer(seedRules())
	roles, err := e.GetRolesForUser("admin@newsroom.dev")
	if err != nil {
		t.Fatalf("GetRolesForUser: %v", err)
	}
	if len(roles) != 1 || roles[0] != "admin" {
		t.Errorf("got %v, want [admin]", roles)
	}
}
