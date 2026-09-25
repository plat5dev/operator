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
	email, ok, err := s.Email(id)
	if err != nil || !ok || email != "op@example.com" {
		t.Fatalf("email: %q %v %v", email, ok, err)
	}
	if _, ok, err := s.Email("missing"); err != nil || ok {
		t.Fatalf("missing email: %v %v", ok, err)
	}
	if err := s.Revoke(token); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Authenticate(token); err != nil || ok {
		t.Fatalf("revoked: %v %v", ok, err)
	}
}

func TestChangePassword(t *testing.T) {
	s := openTest(t)
	id, err := s.Create("op@example.com", "secret")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Login("op@example.com", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangePassword(id, "nope", "next"); !IsUnauthorized(err) {
		t.Fatalf("wrong current: %v", err)
	}
	if _, err := s.ChangePassword(id, "secret", ""); !IsInvalid(err) {
		t.Fatalf("empty next: %v", err)
	}
	if _, ok, err := s.Authenticate(old); err != nil || !ok {
		t.Fatal("failed attempt revoked the session")
	}
	next, err := s.ChangePassword(id, "secret", "next-secret")
	if err != nil || next == "" || next == old {
		t.Fatalf("change: %q %v", next, err)
	}
	if _, ok, err := s.Authenticate(old); err != nil || ok {
		t.Fatal("old token still valid")
	}
	got, ok, err := s.Authenticate(next)
	if err != nil || !ok || got != id {
		t.Fatalf("new token: %q %v %v", got, ok, err)
	}
	if _, err := s.Login("op@example.com", "secret"); !IsUnauthorized(err) {
		t.Fatal("old password still works")
	}
	if _, err := s.Login("op@example.com", "next-secret"); err != nil {
		t.Fatal(err)
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
