package routes

// builtinTemplates holds the documented service API route templates for Plat5's core
// services (identity, audit) and operator-audit. These are strictly for route normalization
// and node param extraction; authorization belongs to AuthZEN.
var builtinTemplates = map[string][]*Route{
	"identity": mustCompileBuiltin("identity", []Route{
		{Path: "/nodes", Methods: []string{"GET"}},
		{Path: "/nodes/{node_id}", Node: "node_id", Methods: []string{"GET", "PATCH", "DELETE"}},
		{Path: "/nodes/{node_id}/restore", Node: "node_id", Methods: []string{"POST"}},
		{Path: "/nodes/{node_id}/release-slug", Node: "node_id", Methods: []string{"POST"}},
		{Path: "/nodes/{root_id}/rate-limit-tier", Node: "root_id", Methods: []string{"PATCH"}},
		{Path: "/nodes/{node_id}/children/{kind}", Node: "node_id", Methods: []string{"GET", "POST"}},
		{Path: "/roots/{kind}", Methods: []string{"POST"}},
		{Path: "/nodes/{node_id}/roles", Node: "node_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{root_id}/roles/{role_id}", Node: "root_id", Methods: []string{"GET", "PATCH", "DELETE"}},
		{Path: "/nodes/{root_id}/permissions", Node: "root_id", Methods: []string{"GET"}},
		{Path: "/nodes/{root_id}/security-policy", Node: "root_id", Methods: []string{"GET", "PATCH"}},
		{Path: "/nodes/{node_id}/bindings", Node: "node_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{node_id}/bindings/{binding_id}", Node: "node_id", Methods: []string{"PATCH", "DELETE"}},
		{Path: "/nodes/{node_id}/memberships/{member_id}", Node: "node_id", Methods: []string{"GET"}},
		{Path: "/nodes/{root_id}/members", Node: "root_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{root_id}/members/{member_id}", Node: "root_id", Methods: []string{"GET", "DELETE"}},
		{Path: "/nodes/{root_id}/members/{member_id}/status", Node: "root_id", Methods: []string{"PATCH"}},
		{Path: "/nodes/{root_id}/groups", Node: "root_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{root_id}/groups/{group_id}", Node: "root_id", Methods: []string{"GET", "PATCH", "DELETE"}},
		{Path: "/nodes/{root_id}/groups/{group_id}/members", Node: "root_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{root_id}/groups/{group_id}/members/{member_id}", Node: "root_id", Methods: []string{"DELETE"}},
		{Path: "/nodes/{node_id}/service-accounts", Node: "node_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{node_id}/service-accounts/{service_account_id}", Node: "node_id", Methods: []string{"GET", "PATCH", "DELETE"}},
		{Path: "/nodes/{node_id}/service-accounts/{service_account_id}/keys", Node: "node_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{node_id}/service-accounts/{service_account_id}/keys/{key_id}", Node: "node_id", Methods: []string{"PATCH", "DELETE"}},
		{Path: "/nodes/{node_id}/invites", Node: "node_id", Methods: []string{"GET", "POST"}},
		{Path: "/nodes/{node_id}/invites/{invite_id}", Node: "node_id", Methods: []string{"DELETE"}},
		{Path: "/users", Methods: []string{"GET"}},
		{Path: "/users/{user_id}", Methods: []string{"GET", "PATCH", "DELETE"}},
		{Path: "/users/{user_id}/status", Methods: []string{"PATCH"}},
		{Path: "/users/{user_id}/nodes", Methods: []string{"GET"}},
		{Path: "/users/{user_id}/keys", Methods: []string{"GET", "POST"}},
		{Path: "/users/{user_id}/keys/{key_id}", Methods: []string{"PATCH", "DELETE"}},
		{Path: "/users/{user_id}/invites/redeem", Methods: []string{"POST"}},
		{Path: "/members/{member_id}/user", Methods: []string{"GET"}},
	}),
	"audit": mustCompileBuiltin("audit", []Route{
		{Path: "/nodes/{node_id}/audit-events", Node: "node_id", Methods: []string{"GET"}},
		{Path: "/users/{user_id}/audit-events", Methods: []string{"GET"}},
	}),
	"operator-audit": mustCompileBuiltin("operator-audit", []Route{
		{Path: "/operator-audit-events", Methods: []string{"GET"}},
	}),
}

func mustCompileBuiltin(service string, specs []Route) []*Route {
	out := make([]*Route, 0, len(specs))
	for _, spec := range specs {
		r := spec
		if err := r.compile(); err != nil {
			panic("builtin route " + service + " " + r.Path + ": " + err.Error())
		}
		out = append(out, &r)
	}
	return out
}
