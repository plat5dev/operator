package gateway

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/plat5dev/operator/internal/routes"
)

func TestUpstreamSigningEndToEnd(t *testing.T) {
	var gotMethod, gotURI, gotReqID, gotSig string
	var gotHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURI = r.URL.RequestURI()
		gotReqID = r.Header.Get("X-Request-ID")
		gotSig = r.Header.Get(SignatureHeader)
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	table, err := routes.Parse([]byte(fmt.Sprintf(`
services:
  app:
    url: %s
    routes:
      - path: /projects/{project}/items
        methods: [POST]
        node: project
        as_member: true
`, upstream.URL)))
	if err != nil {
		t.Fatal(err)
	}

	seed := bytes.Repeat([]byte{11}, ed25519.SeedSize)
	signer, err := NewSignerFromSeed(base64.RawURLEncoding.EncodeToString(seed), "op-k1")
	if err != nil {
		t.Fatal(err)
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)

	gw := New(Config{
		Routes:          table,
		Auth:            fakeAuth{},
		Identity:        fakeIdentity{},
		UpstreamTimeout: 5 * time.Second,
		Signer:          signer,
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	req := httptest.NewRequest("POST", "/as/"+danaID+"/projects/"+projectID+"/items?dry_run=1", nil)
	req.Header.Set("Authorization", "Bearer good")
	req.Header.Set("X-Plat5-Signature", "v1,kid=forged,t=1,sig=forged")
	res := httptest.NewRecorder()
	gw.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}

	parts := strings.Split(gotSig, ",")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "kid=op-k1" {
		t.Fatalf("unexpected signature header: %q", gotSig)
	}
	tsStr, ok := strings.CutPrefix(parts[2], "t=")
	if !ok {
		t.Fatalf("missing t= in %q", gotSig)
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	sigB64, ok := strings.CutPrefix(parts[3], "sig=")
	if !ok {
		t.Fatalf("missing sig= in %q", gotSig)
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		t.Fatal(err)
	}

	if gotURI != "/projects/"+projectID+"/items?dry_run=1" {
		t.Fatalf("gotURI = %q", gotURI)
	}
	payload := CanonicalPayload("op-k1", ts, gotMethod, gotURI, gotReqID, gotHeaders)
	if !ed25519.Verify(pub, []byte(payload), sigBytes) {
		t.Fatalf("signature did not verify for payload:\n%s", payload)
	}
}

func TestReloadSwapsRoutesAndRefusesInvalidUpdate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	initial, err := routes.Parse([]byte(fmt.Sprintf(`
services:
  app:
    url: %s
    routes:
      - path: /v1/ping
        methods: [GET]
`, upstream.URL)))
	if err != nil {
		t.Fatal(err)
	}
	gw := New(Config{
		Routes:          initial,
		Auth:            fakeAuth{},
		UpstreamTimeout: 5 * time.Second,
		Log:             slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	call := func(path string) int {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer good")
		res := httptest.NewRecorder()
		gw.ServeHTTP(res, req)
		return res.Code
	}

	if code := call("/v1/ping"); code != http.StatusOK {
		t.Fatalf("/v1/ping = %d, want 200", code)
	}
	if code := call("/v2/ping"); code != http.StatusNotFound {
		t.Fatalf("/v2/ping before reload = %d, want 404", code)
	}

	// Reload adding a node route without IDENTITY_URL configured must fail and keep /v1/ping live.
	withNodes, err := routes.Parse([]byte(fmt.Sprintf(`
services:
  app:
    url: %s
    routes:
      - path: /projects/{project}
        methods: [GET]
        node: project
`, upstream.URL)))
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.Reload(withNodes); err == nil {
		t.Fatal("expected error reloading node routes without identity client")
	}
	if code := call("/v1/ping"); code != http.StatusOK {
		t.Fatalf("/v1/ping after failed reload = %d, want 200", code)
	}

	// Valid reload replaces the route table atomically.
	updated, err := routes.Parse([]byte(fmt.Sprintf(`
services:
  app:
    url: %s
    routes:
      - path: /v2/ping
        methods: [GET]
`, upstream.URL)))
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.Reload(updated); err != nil {
		t.Fatal(err)
	}
	if code := call("/v1/ping"); code != http.StatusNotFound {
		t.Fatalf("/v1/ping after reload = %d, want 404", code)
	}
	if code := call("/v2/ping"); code != http.StatusOK {
		t.Fatalf("/v2/ping after reload = %d, want 200", code)
	}
}
