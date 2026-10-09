package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	issuer   = "https://idp.test"
	audience = "operator"
)

type idp struct {
	mu     sync.Mutex
	keys   []jose.JSONWebKey
	status int
	hits   int
	srv    *httptest.Server
}

func newIDP(t *testing.T, keys ...jose.JSONWebKey) *idp {
	t.Helper()
	p := &idp{keys: keys, status: http.StatusOK}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.hits++
		w.WriteHeader(p.status)
		var pubs []jose.JSONWebKey
		for _, k := range p.keys {
			pubs = append(pubs, k.Public())
		}
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: pubs})
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) set(keys ...jose.JSONWebKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = keys
}

func (p *idp) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits
}

func rsaKey(t *testing.T, kid string) jose.JSONWebKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return jose.JSONWebKey{Key: k, KeyID: kid, Algorithm: string(jose.RS256), Use: "sig"}
}

func ecKey(t *testing.T, kid string) jose.JSONWebKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return jose.JSONWebKey{Key: k, KeyID: kid, Algorithm: string(jose.ES256), Use: "sig"}
}

func sign(t *testing.T, key jose.JSONWebKey, alg jose.SignatureAlgorithm, claims ...any) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType("JWT")
	if key.KeyID != "" {
		opts = opts.WithHeader("kid", key.KeyID)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key.Key}, opts)
	if err != nil {
		t.Fatal(err)
	}
	b := jwt.Signed(signer)
	for _, c := range claims {
		b = b.Claims(c)
	}
	raw, err := b.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func std(now time.Time) jwt.Claims {
	return jwt.Claims{
		Issuer:   issuer,
		Subject:  "op_1",
		Audience: jwt.Audience{audience},
		Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(now),
	}
}

func newVerifier(t *testing.T, p *idp, mut ...func(*Config)) *Verifier {
	t.Helper()
	cfg := Config{Issuer: issuer, JWKSURI: p.srv.URL, Audiences: []string{"other", audience}}
	for _, m := range mut {
		m(&cfg)
	}
	v := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := v.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVerify(t *testing.T) {
	key := rsaKey(t, "k1")
	ec := ecKey(t, "k2")
	stranger := rsaKey(t, "k1")
	p := newIDP(t, key, ec)
	v := newVerifier(t, p)
	now := time.Now()

	expired := std(now)
	expired.Expiry = jwt.NewNumericDate(now.Add(-2 * time.Minute))
	withinLeeway := std(now)
	withinLeeway.Expiry = jwt.NewNumericDate(now.Add(-30 * time.Second))
	noExp := std(now)
	noExp.Expiry = nil
	future := std(now)
	future.NotBefore = jwt.NewNumericDate(now.Add(5 * time.Minute))
	wrongIss := std(now)
	wrongIss.Issuer = "https://customer.test"
	wrongAud := std(now)
	wrongAud.Audience = jwt.Audience{"another-app"}
	noAud := std(now)
	noAud.Audience = nil
	noSub := std(now)
	noSub.Subject = ""
	noKid := key
	noKid.KeyID = ""
	hmac := jose.JSONWebKey{Key: []byte("0123456789abcdef0123456789abcdef"), KeyID: "k1"}

	cases := []struct {
		name   string
		header string
		want   error
	}{
		{"valid rsa", "Bearer " + sign(t, key, jose.RS256, std(now), map[string]any{"email": "a@x.test"}), nil},
		{"valid ec", "Bearer " + sign(t, ec, jose.ES256, std(now)), nil},
		{"scheme case", "bearer " + sign(t, key, jose.RS256, std(now)), nil},
		{"within leeway", "Bearer " + sign(t, key, jose.RS256, withinLeeway), nil},
		{"missing", "", ErrMissing},
		{"basic", "Basic abc", ErrInvalid},
		{"no token", "Bearer ", ErrInvalid},
		{"garbage", "Bearer abc.def.ghi", ErrInvalid},
		{"expired", "Bearer " + sign(t, key, jose.RS256, expired), ErrExpired},
		{"no exp", "Bearer " + sign(t, key, jose.RS256, noExp), ErrInvalid},
		{"nbf future", "Bearer " + sign(t, key, jose.RS256, future), ErrInvalid},
		{"wrong issuer", "Bearer " + sign(t, key, jose.RS256, wrongIss), ErrInvalid},
		{"wrong audience", "Bearer " + sign(t, key, jose.RS256, wrongAud), ErrInvalid},
		{"no audience", "Bearer " + sign(t, key, jose.RS256, noAud), ErrInvalid},
		{"no sub", "Bearer " + sign(t, key, jose.RS256, noSub), ErrInvalid},
		{"no kid", "Bearer " + sign(t, noKid, jose.RS256, std(now)), ErrInvalid},
		{"wrong key same kid", "Bearer " + sign(t, stranger, jose.RS256, std(now)), ErrInvalid},
		{"hmac", "Bearer " + sign(t, hmac, jose.HS256, std(now)), ErrInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op, err := v.Verify(context.Background(), c.header)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if c.want == nil && op.ID != "op_1" {
				t.Fatalf("id = %q", op.ID)
			}
		})
	}

	op, _ := v.Verify(context.Background(), cases[0].header)
	if op.Email != "a@x.test" || op.Issuer != issuer || op.ClientID != "" {
		t.Fatalf("email = %q, issuer = %q, client = %q", op.Email, op.Issuer, op.ClientID)
	}
}

func TestClientID(t *testing.T) {
	key := rsaKey(t, "k1")
	v := newVerifier(t, newIDP(t, key))
	now := time.Now()
	cases := []struct {
		extra map[string]any
		want  string
	}{
		{map[string]any{"azp": "operator-console", "client_id": "other"}, "operator-console"},
		{map[string]any{"client_id": "automation"}, "automation"},
		{map[string]any{"azp": 7}, ""},
	}
	for _, c := range cases {
		op, err := v.Verify(context.Background(), "Bearer "+sign(t, key, jose.RS256, std(now), c.extra))
		if err != nil {
			t.Fatal(err)
		}
		if op.ClientID != c.want {
			t.Fatalf("%v: client = %q", c.extra, op.ClientID)
		}
	}
}

func TestAlgNone(t *testing.T) {
	p := newIDP(t, rsaKey(t, "k1"))
	v := newVerifier(t, p)
	// {"alg":"none","kid":"k1"} . {"sub":"op_1",...} . (empty)
	none := "eyJhbGciOiJub25lIiwia2lkIjoiazEifQ.eyJzdWIiOiJvcF8xIiwiaXNzIjoiaHR0cHM6Ly9pZHAudGVzdCIsImF1ZCI6Im9wZXJhdG9yIiwiZXhwIjo5OTk5OTk5OTk5fQ."
	if _, err := v.Verify(context.Background(), "Bearer "+none); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestNestedClaim(t *testing.T) {
	key := rsaKey(t, "k1")
	p := newIDP(t, key)
	v := newVerifier(t, p, func(c *Config) { c.IDClaim = "properties.operator_id" })
	now := time.Now()
	tok := sign(t, key, jose.RS256, std(now), map[string]any{"properties": map[string]any{"operator_id": "nested_1"}})
	op, err := v.Verify(context.Background(), "Bearer "+tok)
	if err != nil {
		t.Fatal(err)
	}
	if op.ID != "nested_1" {
		t.Fatalf("id = %q", op.ID)
	}
	if _, err := v.Verify(context.Background(), "Bearer "+sign(t, key, jose.RS256, std(now))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing nested claim: err = %v", err)
	}
}

func TestNotReady(t *testing.T) {
	p := newIDP(t, rsaKey(t, "k1"))
	p.status = http.StatusInternalServerError
	v := New(Config{Issuer: issuer, JWKSURI: p.srv.URL, Audiences: []string{audience}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := v.Refresh(context.Background()); err == nil {
		t.Fatal("want fetch error")
	}
	if v.Ready() {
		t.Fatal("ready after failed fetch")
	}
	if _, err := v.Verify(context.Background(), "Bearer x.y.z"); !errors.Is(err, ErrNoKeys) {
		t.Fatalf("err = %v", err)
	}
	if _, err := v.Verify(context.Background(), ""); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing header before ready: err = %v", err)
	}
}

func TestUnknownKidRefetch(t *testing.T) {
	old := rsaKey(t, "old")
	rotated := rsaKey(t, "new")
	p := newIDP(t, old)
	v := newVerifier(t, p)
	clock := time.Now()
	v.now = func() time.Time { return clock }
	tok := "Bearer " + sign(t, rotated, jose.RS256, std(clock))

	// Within the refetch interval of the boot fetch: no refetch.
	p.set(old, rotated)
	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if p.count() != 1 {
		t.Fatalf("hits = %d", p.count())
	}

	clock = clock.Add(refetchInterval)
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("after refetch: %v", err)
	}
	if p.count() != 2 {
		t.Fatalf("hits = %d", p.count())
	}

	// A failed refetch keeps cached keys.
	p.status = http.StatusInternalServerError
	clock = clock.Add(refetchInterval)
	unknown := "Bearer " + sign(t, rsaKey(t, "other"), jose.RS256, std(clock))
	if _, err := v.Verify(context.Background(), unknown); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("cached key lost: %v", err)
	}
}
