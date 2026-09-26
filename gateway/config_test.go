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
		"/users/{user_id}/memberships":                                               {"GET"},
		"/users/{user_id}/organizations":                                             {"POST"},
		"/users/{user_id}/invites/redeem":                                            {"POST"},
		"/users/{user_id}/api-keys":                                                  {"GET", "POST"},
		"/users/{user_id}/api-keys/{key_id}":                                         {"DELETE"},
		"/users/{user_id}/organizations/{organization_id}/session":                   {"POST"},
		"/organizations":                                                             {"GET"},
		"/organizations/{organization_id}":                                           {"GET", "PATCH", "DELETE"},
		"/organizations/{organization_id}/members":                                   {"GET", "POST"},
		"/organizations/{organization_id}/invites":                                   {"GET", "POST"},
		"/organizations/{organization_id}/invites/{invite_id}":                       {"DELETE"},
		"/organizations/{organization_id}/service-accounts":                          {"GET", "POST"},
		"/organizations/{organization_id}/service-accounts/{service_account_id}":     {"GET", "PATCH", "DELETE"},
		"/members/{member_id}":                                                       {"GET", "PATCH", "DELETE"},
		"/members/{member_id}/api-keys":                                              {"GET", "POST"},
		"/members/{member_id}/api-keys/{key_id}":                                     {"DELETE"},
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
		if rt.Upstream.Host != "identity:3000" {
			t.Fatalf("%s upstream %s", rt.Path, rt.Upstream)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.yml")
	raw := []byte("routes:\n  - path: /users/{user_id}/memberships\n    methods: [GET]\n    upstream: identity:3000\n")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	routes, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Upstream.Host != "identity:3000" {
		t.Fatalf("%+v", routes[0])
	}
	ok, params := routes[0].match("GET", "/users/user-1/memberships")
	if !ok || params["user_id"] != "user-1" {
		t.Fatalf("match %v %v", ok, params)
	}
	if ok, _ := routes[0].match("POST", "/users/user-1/memberships"); ok {
		t.Fatal("method should not match")
	}
}
