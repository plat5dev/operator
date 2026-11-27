package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/authorizer"
)

type fakeAuthorizer struct {
	lastReq authorizer.Request
	res     authorizer.Result
	err     error
	batch   func(items []authorizer.BatchItem) ([]authorizer.Result, error)
}

func (f *fakeAuthorizer) Evaluate(_ context.Context, req authorizer.Request) (authorizer.Result, error) {
	f.lastReq = req
	return f.res, f.err
}

func (f *fakeAuthorizer) EvaluateBatch(_ context.Context, _ authorizer.Subject, _ map[string]any, items []authorizer.BatchItem) ([]authorizer.Result, error) {
	if f.batch != nil {
		return f.batch(items)
	}
	out := make([]authorizer.Result, len(items))
	for i := range items {
		out[i] = f.res
	}
	return out, f.err
}

func TestAuthZENAllowDenyAndUnavailable(t *testing.T) {
	t.Run("allow forwards full payload", func(t *testing.T) {
		up := newUpstream(t, nil)
		rec := &recordingAudit{}
		az := &fakeAuthorizer{res: authorizer.Result{Allowed: true}}
		gw, _ := setupFull(t, up.srv.URL, fakeAuth{}, az, 0, rec, nil)
		w := do(gw, "POST", "/services/identity/nodes/"+projectID+"/children/env", map[string]string{
			"Authorization": "Bearer good",
			asMemberHeader:  danaID,
			reasonHeader:    "ZD-123",
		})
		if w.Code != http.StatusTeapot {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body)
		}
		if az.lastReq.Subject.Type != "operator" || az.lastReq.Subject.ID != "op_1" {
			t.Fatalf("subject = %+v", az.lastReq.Subject)
		}
		if az.lastReq.Action.Name != "POST" || az.lastReq.Action.Properties["reason"] != "ZD-123" ||
			az.lastReq.Action.Properties["acting_as"] != danaID || az.lastReq.Action.Properties["service"] != "identity" ||
			az.lastReq.Action.Properties["route"] != "/nodes/{node_id}/children/{kind}" {
			t.Fatalf("action = %+v", az.lastReq.Action)
		}
		if az.lastReq.Resource.Type != "project" || az.lastReq.Resource.ID != projectID ||
			az.lastReq.Resource.Properties["root_id"] != orgID {
			t.Fatalf("resource = %+v", az.lastReq.Resource)
		}
	})

	t.Run("deny returns 403 and records policy_reason in audit", func(t *testing.T) {
		up := newUpstream(t, nil)
		rec := &recordingAudit{}
		az := &fakeAuthorizer{res: authorizer.Result{Allowed: false, Reason: "ticket required"}}
		gw, _ := setupFull(t, up.srv.URL, fakeAuth{}, az, 0, rec, nil)
		w := do(gw, "PATCH", "/services/identity/nodes/"+projectID, map[string]string{"Authorization": "Bearer good"})
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body)
		}
		if up.seen() != nil {
			t.Fatal("denied request reached upstream")
		}
		env := decode(t, w)
		if env.Error.Code != "FORBIDDEN" || env.Error.Details["reason"] != "ticket required" {
			t.Fatalf("envelope = %+v", env)
		}
		_, out := rec.only(t, w)
		if out.Outcome != audit.Rejected || *out.Status != 403 || string(out.Details) != `{"policy_reason":"ticket required"}` {
			t.Fatalf("outcome = %+v, details = %s", out, out.Details)
		}
	})

	t.Run("authorizer unavailable fails closed 503", func(t *testing.T) {
		up := newUpstream(t, nil)
		rec := &recordingAudit{}
		az := &fakeAuthorizer{err: authorizer.ErrUnavailable}
		gw, _ := setupFull(t, up.srv.URL, fakeAuth{}, az, 0, rec, nil)
		w := do(gw, "GET", "/services/identity/nodes", map[string]string{"Authorization": "Bearer good"})
		if w.Code != http.StatusServiceUnavailable || up.seen() != nil {
			t.Fatalf("status = %d, seen = %v", w.Code, up.seen())
		}
		_, out := rec.only(t, w)
		if out.Outcome != audit.Rejected || *out.Status != 503 {
			t.Fatalf("outcome = %+v", out)
		}
	})
}

func TestAccessRoutesBatch(t *testing.T) {
	up := newUpstream(t, nil)
	rec := &recordingAudit{}
	az := &fakeAuthorizer{
		batch: func(items []authorizer.BatchItem) ([]authorizer.Result, error) {
			out := make([]authorizer.Result, len(items))
			for i, it := range items {
				if it.Action.Name == "GET" {
					out[i] = authorizer.Result{Allowed: true}
				} else {
					out[i] = authorizer.Result{Allowed: false, Reason: "read-only"}
				}
			}
			return out, nil
		},
	}
	gw, _ := setupFull(t, up.srv.URL, fakeAuth{}, az, 0, rec, nil)

	body := `{
		"checks": [
			{"method": "GET", "path": "/services/identity/nodes/` + projectID + `"},
			{"method": "DELETE", "path": "/services/identity/nodes/` + projectID + `"},
			{"method": "GET", "path": "/services/unknown/foo"},
			{"method": "GET", "path": "/services/identity/nodes/` + missingNodeID + `"}
		]
	}`
	r := httptest.NewRequest("POST", "/access/routes", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer good")
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	if rec.count() != 0 {
		t.Fatalf("/access/routes must not be audited, got %d intents", rec.count())
	}
	var resp struct {
		Checks []accessRouteResult `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Checks) != 4 || !resp.Checks[0].Allowed || resp.Checks[1].Allowed || resp.Checks[1].Reason != "read-only" ||
		resp.Checks[2].Allowed || resp.Checks[3].Allowed {
		t.Fatalf("checks = %+v", resp.Checks)
	}
}

func TestTrustedProxiesClientIP(t *testing.T) {
	prefixes, err := ParseTrustedProxies("10.0.0.0/8, 192.0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/services/identity/nodes", nil)
	r.RemoteAddr = "10.1.2.3:54321"
	r.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.7, 192.0.2.1")
	if got := clientIP(r, prefixes); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", got)
	}
	// Untrusted peer ignores X-Forwarded-For.
	r.RemoteAddr = "203.0.113.55:1234"
	if got := clientIP(r, prefixes); got != "203.0.113.55" {
		t.Fatalf("untrusted peer clientIP = %q", got)
	}
}
