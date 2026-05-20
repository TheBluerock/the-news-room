package casbinx

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	stringadapter "github.com/casbin/casbin/v2/persist/string-adapter"
)

const rbacModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && (r.obj == p.obj || p.obj == "*") && (r.act == p.act || p.act == "*")
`

// NewEnforcer builds a Casbin enforcer from rules loaded from PostgreSQL.
// rules is a slice of [ptype, v0, v1, ...] rows from the casbin_rule table.
func NewEnforcer(rules [][]string) (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(rbacModel)
	if err != nil {
		return nil, fmt.Errorf("casbin: model: %w", err)
	}

	var lines []string
	for _, rule := range rules {
		if len(rule) < 2 {
			continue
		}
		lines = append(lines, strings.Join(rule, ", "))
	}

	policy := strings.Join(lines, "\n")
	if policy == "" {
		slog.Warn("casbin: no policies loaded, using fail-closed dummy policy",
			"input_rows", len(rules))
		policy = "p, _no_user_, _no_obj_, _no_act_"
	}
	adapter := stringadapter.NewAdapter(policy)
	e, err := casbin.NewEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("casbin: enforcer: %w", err)
	}
	return e, nil
}
