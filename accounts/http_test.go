package accounts

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plat5dev/operator/internal/apierr"
)

func TestSessionCookie(t *testing.T) {
	store := openTest(t)
	if _, err := store.Create("op@example.com", "secret"); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	mux := http.NewServeMux()
	mux.Handle("POST /login", LoginHandler(store, log))
	mux.Handle("POST /logout", LogoutHandler(store, log))
	mux.Handle("GET /session", SessionHandler(store, log))
	mux.Handle("POST /account/password", PasswordHandler(store, log))
	srv := httptest.NewServer(apierr.Middleware(mux))
	t.Cleanup(srv.Close)

	res := post(t, srv, "/login", `{"email":"op@example.com","password":"secret"}`, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", res.StatusCode)
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&loginBody); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if loginBody.Token == "" {
		t.Fatal("login omitted the token")
	}
	cookie := cookieFrom(t, res)
	if !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.Secure {
		t.Fatalf("cookie %+v", cookie)
	}

	res = get(t, srv, "/session", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("session status %d", res.StatusCode)
	}
	var session struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(res.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if session.Email != "op@example.com" || session.ID == "" {
		t.Fatalf("session %+v", session)
	}

	res = post(t, srv, "/account/password", `{"current_password":"nope","password":"next"}`, cookie)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad password status %d", res.StatusCode)
	}
	_ = res.Body.Close()
	res = get(t, srv, "/session", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatal("wrong password logged the browser out")
	}
	_ = res.Body.Close()

	res = post(t, srv, "/account/password", `{"current_password":"secret","password":"next-secret"}`, cookie)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("password status %d", res.StatusCode)
	}
	next := cookieFrom(t, res)
	_ = res.Body.Close()
	if next.Value == cookie.Value {
		t.Fatal("password change reused the token")
	}
	res = get(t, srv, "/session", cookie)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old cookie status %d", res.StatusCode)
	}
	_ = res.Body.Close()
	res = get(t, srv, "/session", next)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("new cookie status %d", res.StatusCode)
	}
	_ = res.Body.Close()

	noAuth := post(t, srv, "/login", `{"email":"op@example.com","password":"secret"}`, nil)
	bearer := bearerToken(t, noAuth)
	_ = noAuth.Body.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer nope")
	req.AddCookie(bearer)
	res, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bearer should win: %d", res.StatusCode)
	}
	_ = res.Body.Close()

	res = post(t, srv, "/logout", "", next)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status %d", res.StatusCode)
	}
	cleared := cookieFrom(t, res)
	if cleared.MaxAge >= 0 && cleared.Value != "" {
		t.Fatalf("cleared cookie %+v", cleared)
	}
	_ = res.Body.Close()
	res = get(t, srv, "/session", next)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked cookie status %d", res.StatusCode)
	}
	_ = res.Body.Close()
}

func post(t *testing.T, srv *httptest.Server, path, body string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func get(t *testing.T, srv *httptest.Server, path string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func cookieFrom(t *testing.T, res *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	t.Fatal("missing session cookie")
	return nil
}

func bearerToken(t *testing.T, res *http.Response) *http.Cookie {
	t.Helper()
	var body struct {
		Token string `json:"token"`
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: SessionCookie, Value: body.Token}
}

func TestLoginRejectsUnknownField(t *testing.T) {
	store := openTest(t)
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(`{"email":"a@b.c","password":"x","extra":1}`))
	rec := httptest.NewRecorder()
	LoginHandler(store, log).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}
