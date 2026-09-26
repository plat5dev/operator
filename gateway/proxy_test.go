package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/plat5dev/operator/accounts"
	"github.com/plat5dev/operator/internal/apierr"
)

func TestProxy(t *testing.T) {
	var mu sync.Mutex
	var calls int
	var got http.Header
	var gotPath string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		got = r.Header.Clone()
		gotPath = r.URL.RequestURI()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN"}}`))
	}))
	defer up.Close()

	store := openStore(t)
	operatorID, err := store.Create("op@example.com", "secret")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.Login("op@example.com", "secret")
	if err != nil {
		t.Fatal(err)
	}

	routes := mustRoutes(t, up.URL)
	srv := httptest.NewServer(apierr.Middleware(Proxy(store, routes, slog.New(slog.NewJSONHandler(io.Discard, nil)))))
	defer srv.Close()

	t.Run("missing credential does not dial", func(t *testing.T) {
		before := callCount(&mu, &calls)
		res := do(t, srv, http.MethodGet, "/users/user-1/memberships", "")
		if res.StatusCode != http.StatusUnauthorized || callCount(&mu, &calls) != before {
			t.Fatalf("status %d calls %d", res.StatusCode, callCount(&mu, &calls))
		}
		assertCode(t, res, "UNAUTHORIZED")
	})

	t.Run("forwards the path and strips the operator credential", func(t *testing.T) {
		res := do(t, srv, http.MethodGet, "/users/user-1/memberships?limit=1", token, header{"X-Request-ID", "rid-1"}, header{"X-Api-Key", "plat5-sk-1-secret"})
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d", res.StatusCode)
		}
		body, _ := io.ReadAll(res.Body)
		if string(body) != `{"error":{"code":"FORBIDDEN"}}` {
			t.Fatalf("body %s", body)
		}
		if res.Header.Get("X-Request-ID") != "rid-1" {
			t.Fatalf("response request id %q", res.Header.Get("X-Request-ID"))
		}
		mu.Lock()
		defer mu.Unlock()
		if gotPath != "/users/user-1/memberships?limit=1" {
			t.Fatalf("path %s", gotPath)
		}
		if got.Get("Authorization") != "" || got.Get("X-Api-Key") != "" || got.Get("Cookie") != "" {
			t.Fatalf("forwarded credential: %v", got)
		}
		if got.Get("X-Request-ID") != "rid-1" {
			t.Fatalf("upstream request id %q", got.Get("X-Request-ID"))
		}
		if headerHas(got, operatorID) {
			t.Fatal("operator id was forwarded")
		}
	})

	t.Run("forwards an org path", func(t *testing.T) {
		res := do(t, srv, http.MethodGet, "/organizations/org-9", token)
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("status %d", res.StatusCode)
		}
		mu.Lock()
		defer mu.Unlock()
		if gotPath != "/organizations/org-9" {
			t.Fatalf("path %s", gotPath)
		}
		if headerHas(got, operatorID) {
			t.Fatal("operator id was forwarded")
		}
	})

	t.Run("unknown route", func(t *testing.T) {
		before := callCount(&mu, &calls)
		res := do(t, srv, http.MethodPost, "/users/user-1/memberships", token)
		if res.StatusCode != http.StatusNotFound || callCount(&mu, &calls) != before {
			t.Fatalf("status %d", res.StatusCode)
		}
		if !strings.Contains(string(readBody(t, res)), `"details":null`) {
			t.Fatal("details not null")
		}
	})

	t.Run("generates request id", func(t *testing.T) {
		res := do(t, srv, http.MethodGet, "/organizations", token)
		if res.Header.Get("X-Request-ID") == "" {
			t.Fatal("missing request id")
		}
	})
}

func TestCookieCredential(t *testing.T) {
	var got http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer up.Close()

	store := openStore(t)
	if _, err := store.Create("op@example.com", "secret"); err != nil {
		t.Fatal(err)
	}
	token, err := store.Login("op@example.com", "secret")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(apierr.Middleware(Proxy(store, mustRoutes(t, up.URL), slog.New(slog.NewJSONHandler(io.Discard, nil)))))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/organizations", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: accounts.SessionCookie, Value: token})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("cookie auth status %d", res.StatusCode)
	}
	if got.Get("Cookie") != "" || got.Get("Authorization") != "" {
		t.Fatalf("upstream headers %v", got)
	}

	req, err = http.NewRequest(http.MethodGet, srv.URL+"/organizations", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer nope")
	req.AddCookie(&http.Cookie{Name: accounts.SessionCookie, Value: token})
	res, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bearer should win: %d", res.StatusCode)
	}
}

func TestDialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	store := openStore(t)
	if _, err := store.Create("op@example.com", "secret"); err != nil {
		t.Fatal(err)
	}
	token, err := store.Login("op@example.com", "secret")
	if err != nil {
		t.Fatal(err)
	}
	routes := mustRoutes(t, "http://"+addr)
	srv := httptest.NewServer(apierr.Middleware(Proxy(store, routes, slog.New(slog.NewJSONHandler(io.Discard, nil)))))
	defer srv.Close()
	res := do(t, srv, http.MethodGet, "/organizations", token)
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", res.StatusCode)
	}
	assertCode(t, res, "SERVICE_UNAVAILABLE")
}

type header struct{ k, v string }

func do(t *testing.T, srv *httptest.Server, method, path, token string, extra ...header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, h := range extra {
		req.Header.Set(h.k, h.v)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func assertCode(t *testing.T, res *http.Response, code string) {
	t.Helper()
	var env struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	body := readBody(t, res)
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != code {
		t.Fatalf("code %s body %s", env.Error.Code, body)
	}
	if env.Error.RequestID == "" || env.Error.RequestID != res.Header.Get("X-Request-ID") {
		t.Fatalf("request id body %q header %q", env.Error.RequestID, res.Header.Get("X-Request-ID"))
	}
}

func readBody(t *testing.T, res *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func callCount(mu *sync.Mutex, calls *int) int {
	mu.Lock()
	defer mu.Unlock()
	return *calls
}

func headerHas(h http.Header, needle string) bool {
	for k, vals := range h {
		if strings.Contains(k, needle) {
			return true
		}
		for _, v := range vals {
			if v == needle {
				return true
			}
		}
	}
	return false
}

func openStore(t *testing.T) *accounts.Store {
	t.Helper()
	s, err := accounts.Open(filepath.Join(t.TempDir(), "op.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustRoutes(t *testing.T, upstream string) []Route {
	t.Helper()
	specs := []struct {
		path    string
		methods []string
	}{
		{"/users/{user_id}/memberships", []string{"GET"}},
		{"/organizations/{organization_id}", []string{"GET"}},
		{"/organizations", []string{"GET"}},
	}
	out := make([]Route, 0, len(specs))
	for _, spec := range specs {
		rt, err := compile(spec.path, spec.methods, upstream)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, rt)
	}
	return out
}
