package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

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
