package accounts

import (
	"path/filepath"
	"testing"
)

func TestLoginAndBootstrap(t *testing.T) {
	s := openTest(t)
	if _, err := s.Create("Op@Example.com", "secret"); err != nil {
		t.Fatal(err)
	}
	id, token, err := s.LoginOperator("op@example.com", "secret")
	if err != nil || token == "" || id == "" {
		t.Fatalf("login: %v", err)
	}
	got, ok, err := s.Authenticate(token)
	if err != nil || !ok || got != id {
		t.Fatalf("authenticate: %q %v %v", got, ok, err)
	}
	if _, err := s.Login("op@example.com", "nope"); !IsUnauthorized(err) {
		t.Fatalf("bad password: %v", err)
	}
	if _, ok, err := s.Authenticate("nope"); err != nil || ok {
		t.Fatalf("bad token: %v %v", ok, err)
	}
	created, err := s.Bootstrap("op@example.com", "other")
	if err != nil || created {
		t.Fatalf("bootstrap existing: %v %v", created, err)
	}
	if _, err := s.Login("op@example.com", "other"); !IsUnauthorized(err) {
		t.Fatal("bootstrap reset the password")
	}
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "op.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
