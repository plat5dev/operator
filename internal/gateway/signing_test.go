package gateway

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

func testSeedB64() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, ed25519.SeedSize))
}

func TestCanonicalPayloadSortsAndFilters(t *testing.T) {
	h := http.Header{
		"X-Plat5-Project-Id":      {"01jbx"},
		"X-Plat5-Signature":       {"ignored"},
		"X-Plat5-Organization-Id": {"01jbw"},
		"X-Plat5-Operator-Id":     {"op-1"},
		"X-Plat5-Member-Id":       {"01jby"},
		"X-Plat5-Principal":       {"person"},
		"X-Other":                 {"ignored"},
	}
	got := CanonicalPayload("k-2026-11", 1700000000, "post", "/projects/01jbx/plans?limit=10", "req-1", h)
	want := "v1\n" +
		"k-2026-11\n" +
		"1700000000\n" +
		"POST\n" +
		"/projects/01jbx/plans?limit=10\n" +
		"req-1\n" +
		"x-plat5-member-id:01jby\n" +
		"x-plat5-operator-id:op-1\n" +
		"x-plat5-organization-id:01jbw\n" +
		"x-plat5-principal:person\n" +
		"x-plat5-project-id:01jbx\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSignerRoundTrip(t *testing.T) {
	signer, err := NewSignerFromSeed(testSeedB64(), "k-1")
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{
		"X-Plat5-Operator-Id": {"op-1"},
		"X-Plat5-Project-Id":  {"01jbx"},
	}
	header := signer.Sign("GET", "/projects/01jbx/items?active=true", "req-123", h, 1700000042)
	parts := strings.Split(header, ",")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "kid=k-1" || parts[2] != "t=1700000042" {
		t.Fatalf("unexpected header: %q", header)
	}
	sigB64, ok := strings.CutPrefix(parts[3], "sig=")
	if !ok || len(sigB64) != 86 {
		t.Fatalf("unexpected sig part: %q", parts[3])
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		t.Fatal(err)
	}
	pub := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	payload := CanonicalPayload("k-1", 1700000042, "GET", "/projects/01jbx/items?active=true", "req-123", h)
	if !ed25519.Verify(pub, []byte(payload), sig) {
		t.Fatal("signature did not verify")
	}
}

func TestSignerDefaultKIDAndValidation(t *testing.T) {
	s1, err := NewSignerFromSeed(testSeedB64(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s1.KID()) != 16 {
		t.Fatalf("kid = %q, want 16 hex chars", s1.KID())
	}
	s2, err := NewSignerFromSeed(testSeedB64(), "")
	if err != nil {
		t.Fatal(err)
	}
	if s1.KID() != s2.KID() {
		t.Fatalf("default kid not deterministic: %q vs %q", s1.KID(), s2.KID())
	}

	if _, err := NewSignerFromSeed("not-base64!!!", ""); err == nil {
		t.Fatal("want error for invalid base64")
	}
	short := base64.RawURLEncoding.EncodeToString(make([]byte, 16))
	if _, err := NewSignerFromSeed(short, ""); err == nil {
		t.Fatal("want error for short seed")
	}
	if _, err := NewSignerFromSeed(testSeedB64(), "bad kid"); err == nil {
		t.Fatal("want error for invalid kid")
	}
	if _, err := NewSignerFromSeed(testSeedB64(), strings.Repeat("a", 65)); err == nil {
		t.Fatal("want error for overlong kid")
	}
}
