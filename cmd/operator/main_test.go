package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plat5dev/operator/accounts"
	"github.com/plat5dev/operator/internal/apierr"
)

func TestPagesAreNotTheGateway(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "op.db")
	store, err := accounts.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	assets := filepath.Join(dir, "assets")
	if err := os.Mkdir(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	index := "<!DOCTYPE html><script>" + "__MODULES__" + "</script>"
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h := apierr.Middleware(newHandler(store, nil, log, assets, nil, filepath.Join(dir, "modules")))

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/account", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "[]") {
		t.Fatalf("page %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/organizations", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api %d %s", rec.Code, rec.Body.String())
	}

	modDir := filepath.Join(dir, "modules")
	if err := os.MkdirAll(filepath.Join(modDir, "identity"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "identity", "entry.js"), []byte("export default function(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	h = apierr.Middleware(newHandler(store, nil, log, assets, nil, modDir))
	req = httptest.NewRequest(http.MethodGet, "/modules/identity/entry.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "export default") {
		t.Fatalf("module %d %s", rec.Code, rec.Body.String())
	}
}
