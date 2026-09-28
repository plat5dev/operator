package console

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServesShell(t *testing.T) {
	dir := t.TempDir()
	index := "<!DOCTYPE html><script type=\"application/json\" id=\"operator-modules\">" + modulesPlaceholder + "</script>"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	mods := []Module{{ID: "identity", Title: "Identity", BasePath: "/identity", Entry: "/modules/identity/entry.js"}}
	h := Handler(dir, mods)

	res := get(t, h, "/")
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), modulesPlaceholder) || !strings.Contains(res.Body.String(), `"basePath":"/identity"`) {
		t.Fatalf("index %d %s", res.Code, res.Body.String())
	}
	if res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache %q", res.Header().Get("Cache-Control"))
	}

	res = get(t, h, "/account")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"id":"identity"`) {
		t.Fatalf("account %d %s", res.Code, res.Body.String())
	}

	res = get(t, h, "/assets/app.js")
	if res.Code != http.StatusOK || res.Body.String() != "console.log(1)" {
		t.Fatalf("asset %d %s", res.Code, res.Body.String())
	}

	res = get(t, h, "/assets/missing.js")
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing asset %d", res.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/account", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("post page %d", rec.Code)
	}
}

func TestMissingAssets(t *testing.T) {
	res := get(t, Handler(t.TempDir(), nil), "/")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "not built") {
		t.Fatalf("%d %s", res.Code, res.Body.String())
	}
}

func TestModuleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "identity"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "identity", "entry.js"), []byte("export default function(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := Files(dir)

	res := get(t, h, "/modules/identity/entry.js")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "export default") {
		t.Fatalf("entry %d %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("type %q", res.Header().Get("Content-Type"))
	}
	if res.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("nosniff %q", res.Header().Get("X-Content-Type-Options"))
	}

	res = get(t, h, "/modules/identity/missing.js")
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing %d", res.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/modules/identity/entry.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("post %d", rec.Code)
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
