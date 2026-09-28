package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadModules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "modules.yml")
	raw := []byte(`modules:
  - id: identity
    title: Identity
    base_path: /identity
    entry: /modules/identity/entry.js
  - id: billing
    title: Billing
    base_path: /billing/
    entry: /modules/billing/entry.js
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	mods, err := LoadModules(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 2 || mods[0].BasePath != "/identity" || mods[1].BasePath != "/billing" || mods[0].Entry != "/modules/identity/entry.js" {
		t.Fatalf("%+v", mods)
	}

	empty := filepath.Join(dir, "empty.yml")
	if err := os.WriteFile(empty, []byte("modules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mods, err = LoadModules(empty)
	if err != nil || len(mods) != 0 {
		t.Fatalf("%v %+v", err, mods)
	}
}

func TestLoadModulesRejects(t *testing.T) {
	cases := []string{
		"modules:\n  - id: identity\n    title: Identity\n    base_path: /organizations\n    entry: /modules/identity/entry.js\n",
		"modules:\n  - id: identity\n    title: Identity\n    base_path: /identity\n    entry: https://evil/entry.js\n",
		"modules:\n  - id: identity\n    title: Identity\n    base_path: /identity\n    entry: /modules/../entry.js\n",
		"modules:\n  - id: a\n    title: A\n    base_path: /a\n    entry: /modules/a/entry.js\n  - id: a\n    title: B\n    base_path: /b\n    entry: /modules/b/entry.js\n",
		"modules:\n  - id: a\n    title: A\n    base_path: /tools\n    entry: /modules/a/entry.js\n  - id: b\n    title: B\n    base_path: /tools/extra\n    entry: /modules/b/entry.js\n",
		"modules:\n  - id: a\n    title: A\n    base_path: /\n    entry: /modules/a/entry.js\n",
	}
	for _, raw := range cases {
		path := filepath.Join(t.TempDir(), "modules.yml")
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadModules(path); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestModulesJSONEscapes(t *testing.T) {
	raw := modulesJSON([]Module{{
		ID:       "identity",
		Title:    `</script>`,
		BasePath: "/identity",
		Entry:    "/modules/identity/entry.js",
	}})
	if strings.Contains(raw, "</script>") || !strings.Contains(raw, `\u003c`) {
		t.Fatal(raw)
	}
}
