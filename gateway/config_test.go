package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityRoutes(t *testing.T) {
	routes, err := Load("../routes.yml")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"/api/organizations":                                                         {"GET", "POST"},
		"/api/organizations/{organization_id}":                                       {"GET", "PATCH", "DELETE"},
		"/api/organizations/{organization_id}/members":                               {"GET", "POST"},
		"/api/organizations/{organization_id}/members/{member_id}":                   {"GET", "PATCH", "DELETE"},
		"/api/organizations/{organization_id}/members/{member_id}/api-keys":          {"GET", "POST"},
		"/api/organizations/{organization_id}/members/{member_id}/api-keys/{key_id}": {"DELETE"},
		"/api/organizations/{organization_id}/invites":                               {"GET", "POST"},
		"/api/organizations/{organization_id}/invites/{invite_id}":                   {"DELETE"},
		"/api/invites/redeem":                                                        {"POST"},
		"/api/organizations/{organization_id}/service-accounts":                      {"GET", "POST"},
		"/api/organizations/{organization_id}/service-accounts/{service_account_id}": {"GET", "PATCH", "DELETE"},
	}
	if len(routes) != len(want) {
		t.Fatalf("got %d routes", len(routes))
	}
	for _, rt := range routes {
		methods, ok := want[rt.Path]
		if !ok {
			t.Fatalf("unexpected %s", rt.Path)
		}
		if strings.Join(rt.Methods, ",") != strings.Join(methods, ",") {
			t.Fatalf("%s methods %v", rt.Path, rt.Methods)
		}
		if rt.Requires != RequiresUser || rt.Upstream.Host != "identity:3000" {
			t.Fatalf("%s requires %s upstream %s", rt.Path, rt.Requires, rt.Upstream)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.yml")
	raw := []byte("routes:\n  - path: /api/organizations\n    methods: [GET]\n    upstream: identity:3000\n    requires: user\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	routes, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Upstream.Host != "identity:3000" || routes[0].Requires != RequiresUser {
		t.Fatalf("%+v", routes[0])
	}
	ok, _ := routes[0].match("GET", "/api/organizations")
	if !ok {
		t.Fatal("expected match")
	}
	if ok, _ := routes[0].match("POST", "/api/organizations"); ok {
		t.Fatal("method should not match")
	}
}
