package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func object(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// These run without a stack.

func TestMerge(t *testing.T) {
	for _, c := range []struct{ name, base, add, want string }{
		{"empty additions", `{"a":1,"b":{"c":2}}`, `{}`, `{"a":1,"b":{"c":2}}`},
		{"objects merge key by key", `{"k":{"p":{"x":1,"y":2}},"r":3}`, `{"k":{"p":{"y":9,"z":true}}}`, `{"k":{"p":{"x":1,"y":9,"z":true}},"r":3}`},
		{"new keys are added", `{"a":1}`, `{"b":{"c":2}}`, `{"a":1,"b":{"c":2}}`},
		{"arrays replace whole", `{"a":[1,2,3]}`, `{"a":[4]}`, `{"a":[4]}`},
		{"a scalar replaces an object", `{"a":{"b":1}}`, `{"a":false}`, `{"a":false}`},
		{"an object replaces a scalar", `{"a":"s"}`, `{"a":{"b":1}}`, `{"a":{"b":1}}`},
		{"null replaces", `{"a":{"b":1}}`, `{"a":null}`, `{"a":null}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			base, add := object(t, c.base), object(t, c.add)
			got := merge(base, add)
			if want := object(t, c.want); !reflect.DeepEqual(got, want) {
				t.Fatalf("merge(%s, %s) = %v, want %v", c.base, c.add, got, want)
			}
			if !reflect.DeepEqual(base, object(t, c.base)) || !reflect.DeepEqual(add, object(t, c.add)) {
				t.Fatal("merge changed its inputs")
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "e2e", "testdata"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, s string) {
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, plat5Config), `{"kinds":{"project":{"parent":"organization","bind_creator":false}},"creator_role":"admin"}`)
	add := filepath.Join(dir, "additions.json")
	write(add, `{"kinds":{"project":{"bind_creator":true}}}`)

	got, err := loadConfig(dir, add)
	if err != nil {
		t.Fatal(err)
	}
	if want := object(t, `{"kinds":{"project":{"parent":"organization","bind_creator":true}},"creator_role":"admin"}`); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}

	missing := filepath.Join(dir, "nope")
	_, err = loadConfig(missing, add)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(missing, plat5Config)) || !strings.Contains(err.Error(), "PLAT5_DIR") {
		t.Fatalf("missing plat5: %v", err)
	}
}

// The additions file parses, and is an object.
func TestAdditionsFile(t *testing.T) {
	if _, err := readObject(additionsConfig); err != nil {
		t.Fatal(err)
	}
}
