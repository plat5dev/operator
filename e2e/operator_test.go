// Package e2e drives the operator gateway against a running Plat5 stack: the tree by id,
// acting as a member, the chain sent to services, restore, and the operator audit log.
// Opt-in: OPERATOR_E2E=1. See README.md.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// operator is the gateway as an operator's client sees it.
type operator struct {
	t     *testing.T
	base  string
	token string
	http  *http.Client
}

type response struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func setup(t *testing.T) *operator {
	t.Helper()
	if os.Getenv("OPERATOR_E2E") != "1" {
		t.Skip("OPERATOR_E2E=1 runs the end-to-end tests against a running stack")
	}
	o := &operator{t: t, base: env("OPERATOR_URL", "http://localhost:5410"), http: &http.Client{Timeout: 10 * time.Second}}
	wait, err := time.ParseDuration(env("E2E_WAIT", "5m"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(wait)
	o.applyConfig(deadline)
	for {
		o.token, err = o.mint()
		if err == nil {
			// Ready once the gateway has JWKS and identity answers.
			if r := o.call("GET", "/nodes?limit=1", nil); r.status == 200 {
				return o
			} else {
				err = fmt.Errorf("GET /nodes: %d %s", r.status, r.raw)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("stack not ready after %s: %v", wait, err)
		}
		time.Sleep(2 * time.Second)
	}
}

// applyConfig applies Plat5's e2e config with operator's additions (config_test.go) through
// Plat5's control plane, as Plat5's own e2e does: a dry run for the plan's revision, then
// the apply with it as expect. Identity listens only once a config is applied. A plan with
// no changes applies nothing.
func (o *operator) applyConfig(deadline time.Time) {
	o.t.Helper()
	config, err := loadConfig(plat5Dir(), additionsConfig)
	if err != nil {
		o.t.Fatal(err)
	}
	var plan struct {
		Revision int   `json:"revision"`
		Changes  []any `json:"changes"`
	}
	for {
		status, body, err := o.admin("/apply?dry_run=true", config)
		if err == nil && status == http.StatusOK {
			if err := json.Unmarshal(body, &plan); err != nil {
				o.t.Fatalf("plan: %v: %s", err, body)
			}
			break
		}
		if err == nil && status != http.StatusServiceUnavailable {
			o.t.Fatalf("dry run: %d %s", status, body)
		}
		if time.Now().After(deadline) {
			o.t.Fatalf("control plane not ready: %v %d %s", err, status, body)
		}
		time.Sleep(2 * time.Second)
	}
	if len(plan.Changes) == 0 {
		return
	}
	config["expect"] = map[string]int{"revision": plan.Revision}
	if status, body, err := o.admin("/apply", config); err != nil || status != http.StatusOK {
		o.t.Fatalf("apply: %v %d %s", err, status, body)
	}
}

func (o *operator) admin(path string, body any) (int, []byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", env("CONTROL_PLANE_URL", "http://localhost:5402")+path, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env("ADMIN_TOKEN", "dev-admin-token"))
	res, err := o.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	return res.StatusCode, out, err
}

// mint gets an operator token from Plat5 Auth's dev mode.
func (o *operator) mint() (string, error) {
	body := `{"email":"operator@example.com","client_id":"operator-console"}`
	res, err := o.http.Post(env("OPERATOR_AUTH_URL", "http://localhost:5411")+"/dev/token", "application/json", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("dev token: %d %v", res.StatusCode, err)
	}
	return out.AccessToken, nil
}

func (o *operator) call(method, path string, body any) response {
	o.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			o.t.Fatal(err)
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, o.base+path, r)
	if err != nil {
		o.t.Fatal(err)
	}
	if o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}
	req.Header.Set("Content-Type", "application/json")
	// A client can't name a member, a node or an operator.
	req.Header.Set("X-Plat5-Member-Id", forgedMemberID)
	req.Header.Set("X-Plat5-Operator-Id", "forged")
	res, err := o.http.Do(req)
	if err != nil {
		o.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := response{status: res.StatusCode, header: res.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

// must calls and fails unless the status is want.
func (o *operator) must(want int, method, path string, body any) response {
	o.t.Helper()
	r := o.call(method, path, body)
	if r.status != want {
		o.t.Fatalf("%s %s: %d, want %d: %s", method, path, r.status, want, r.raw)
	}
	return r
}

// forgedMemberID is what a client sends as X-Plat5-Member-Id. It never reaches a service.
const forgedMemberID = "01jbyb5j6k7m8n9p0q1r2s3t4v"

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func list(r response, key string) []map[string]any {
	items, _ := r.body[key].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		out = append(out, m)
	}
	return out
}

func ids(items []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[str(it, "id")] = true
	}
	return out
}

func TestOperator(t *testing.T) {
	o := setup(t)
	suffix := fmt.Sprint(time.Now().UnixNano())

	// An org an operator makes has no member behind it.
	org := str(o.must(201, "POST", "/roots/organization", map[string]any{"display_name": "E2E " + suffix}).body, "id")
	danaIdP := map[string]any{"issuer": "http://localhost:5401", "subject": "e2e-dana-" + suffix}
	dana := str(o.must(201, "POST", "/nodes/"+org+"/members", map[string]any{"idp": danaIdP, "role": "member"}).body, "id")

	// The tree, unfiltered, and a node by id.
	if len(list(o.must(200, "GET", "/nodes?kind=organization", nil), "nodes")) == 0 {
		t.Fatal("no orgs in the tree")
	}
	if str(o.must(200, "GET", "/nodes/"+org, nil).body, "kind") != "organization" {
		t.Fatal("org not found by id")
	}

	t.Run("acting as a member authors the write", func(t *testing.T) {
		project := str(o.must(201, "POST", "/as/"+dana+"/nodes/"+org+"/children/project", map[string]any{"display_name": "Apps"}).body, "id")
		var found bool
		for _, b := range list(o.must(200, "GET", "/nodes/"+project+"/bindings?inherited=false", nil), "bindings") {
			if str(b, "member_id") == dana && str(b, "role") == "admin" {
				found = true
			}
		}
		if !found {
			t.Fatal("the member acted as didn't get creator_role on the new project")
		}
		// Her org's role and her project's, as identity resolves them.
		perms, _ := o.must(200, "GET", "/nodes/"+project+"/memberships/"+dana, nil).body["permissions"].([]any)
		if fmt.Sprint(perms) != "[* organization:read]" {
			t.Fatalf("Dana's permissions on her project: %v", perms)
		}

		// Created by the operator alone: no member, so no binding.
		bare := str(o.must(201, "POST", "/nodes/"+org+"/children/project", map[string]any{"display_name": "Bare"}).body, "id")
		if b := list(o.must(200, "GET", "/nodes/"+bare+"/bindings?inherited=false", nil), "bindings"); len(b) != 0 {
			t.Fatalf("bindings on a project the operator made: %v", b)
		}

		// A filtered list, as the member: what Dana can read. Without a member, the full list.
		if seen := ids(list(o.must(200, "GET", "/as/"+dana+"/nodes/"+org+"/children/project", nil), "nodes")); !seen[project] || seen[bare] {
			t.Fatalf("Dana sees %v", seen)
		}
		if seen := ids(list(o.must(200, "GET", "/nodes/"+org+"/children/project", nil), "nodes")); !seen[project] || !seen[bare] {
			t.Fatalf("the full list is %v", seen)
		}

		events := list(o.must(200, "GET", "/operator-audit-events?acting_as_member_id="+dana+"&method=POST", nil), "audit_events")
		if len(events) != 1 {
			t.Fatalf("%d events acting as Dana", len(events))
		}
		e := events[0]
		acting, _ := e["acting_as"].(map[string]any)
		if str(acting, "member_id") != dana || str(acting, "principal") != "person" || str(e, "root_id") != org ||
			str(e, "route") != "/as/{member}/nodes/{node_id}/children/{kind}" || str(e, "created_id") != project ||
			str(e, "outcome") != "responded" {
			t.Fatalf("event %v", e)
		}
	})

	t.Run("a person, from their member", func(t *testing.T) {
		user := str(o.must(200, "GET", "/members/"+dana+"/user", nil).body, "id")
		q := url.Values{"issuer": {str(danaIdP, "issuer")}, "subject": {str(danaIdP, "subject")}}
		if found := list(o.must(200, "GET", "/users?"+q.Encode(), nil), "users"); len(found) != 1 || str(found[0], "id") != user {
			t.Fatalf("by IdP link: %v", found)
		}
		if str(o.must(200, "GET", "/users/"+user, nil).body, "status") != "active" {
			t.Fatal("Dana isn't active")
		}
		if !ids(list(o.must(200, "GET", "/users/"+user+"/nodes?kind=organization", nil), "nodes"))[org] {
			t.Fatal("Dana's orgs lack hers")
		}
		if _, ok := o.must(200, "GET", "/users/"+user+"/audit-events", nil).body["audit_events"]; !ok {
			t.Fatal("no audit_events in a person's log")
		}

		// An operator sets the person's name, trimmed, then clears it.
		if got := o.must(200, "PATCH", "/users/"+user, map[string]any{"name": "  Dana Scully  "}).body["name"]; got != "Dana Scully" {
			t.Fatalf("name after set: %v", got)
		}
		if got := o.must(200, "GET", "/users/"+user, nil).body["name"]; got != "Dana Scully" {
			t.Fatalf("name read back: %v", got)
		}
		if body := o.must(200, "PATCH", "/users/"+user, map[string]any{"name": nil}).body; body["name"] != nil {
			t.Fatalf("name after clear: %v", body["name"])
		}
		if body := o.must(200, "GET", "/users/"+user, nil).body; body["name"] != nil {
			t.Fatalf("name read back after clear: %v", body["name"])
		}
		q = url.Values{"method": {"PATCH"}, "route": {"/users/{user_id}"}, "params[user_id]": {user}}
		if events := list(o.must(200, "GET", "/operator-audit-events?"+q.Encode(), nil), "audit_events"); len(events) != 2 {
			t.Fatalf("%d operator audit events for the name edits", len(events))
		}
	})

	t.Run("services get the chain, the member and the operator", func(t *testing.T) {
		project := str(o.must(201, "POST", "/as/"+dana+"/nodes/"+org+"/children/project", map[string]any{"display_name": "Echo"}).body, "id")
		r := o.must(200, "GET", "/as/"+dana+"/nodes/"+project+"/echo", nil)
		// whoami prints the request line and headers it received.
		for _, want := range []string{
			"GET /nodes/" + project + "/echo ",
			"X-Plat5-Organization-Id: " + org,
			"X-Plat5-Project-Id: " + project,
			"X-Plat5-Member-Id: " + dana,
			"X-Plat5-Principal: person",
		} {
			if !strings.Contains(r.raw, want) {
				t.Errorf("echo lacks %q:\n%s", want, r.raw)
			}
		}
		for _, never := range []string{"Authorization:", forgedMemberID, "X-Plat5-Operator-Id: forged"} {
			if strings.Contains(r.raw, never) {
				t.Errorf("echo has %q:\n%s", never, r.raw)
			}
		}
		if !strings.Contains(r.raw, "X-Plat5-Operator-Id: ") {
			t.Errorf("echo lacks the operator:\n%s", r.raw)
		}

		// Without /as/, the chain and the operator only.
		r = o.must(200, "GET", "/nodes/"+project+"/echo", nil)
		if strings.Contains(r.raw, "X-Plat5-Member-Id") || strings.Contains(r.raw, "X-Plat5-Principal") ||
			!strings.Contains(r.raw, "X-Plat5-Project-Id: "+project) {
			t.Fatalf("echo without a member:\n%s", r.raw)
		}
	})

	t.Run("a member must be active in the node's tree", func(t *testing.T) {
		other := str(o.must(201, "POST", "/roots/organization", map[string]any{"display_name": "Other " + suffix}).body, "id")
		o.must(400, "POST", "/as/"+dana+"/nodes/"+other+"/children/project", map[string]any{"display_name": "Nope"})

		gone := str(o.must(201, "POST", "/nodes/"+org+"/members", map[string]any{
			"idp":  map[string]any{"issuer": "http://localhost:5401", "subject": "e2e-gone-" + suffix},
			"role": "member",
		}).body, "id")
		o.must(204, "DELETE", "/nodes/"+org+"/members/"+gone, nil)
		o.must(400, "POST", "/as/"+gone+"/nodes/"+org+"/children/project", map[string]any{"display_name": "Nope"})

		events := list(o.must(200, "GET", "/operator-audit-events?params%5Bmember%5D="+gone, nil), "audit_events")
		if len(events) != 1 || str(events[0], "outcome") != "rejected" || events[0]["acting_as"] != nil ||
			events[0]["status"] != float64(400) {
			t.Fatalf("events %v", events)
		}
	})

	t.Run("delete and restore a project", func(t *testing.T) {
		project := str(o.must(201, "POST", "/as/"+dana+"/nodes/"+org+"/children/project", map[string]any{"display_name": "Doomed"}).body, "id")
		o.must(204, "DELETE", "/nodes/"+project, nil)
		deleted := o.must(200, "GET", "/nodes?parent_id="+org+"&status=deleted", nil)
		if !ids(list(deleted, "nodes"))[project] {
			t.Fatal("deleted project not listed")
		}
		if str(o.must(200, "POST", "/nodes/"+project+"/restore", nil).body, "status") != "active" {
			t.Fatal("not restored")
		}
	})

	t.Run("a node that isn't there", func(t *testing.T) {
		r := o.must(404, "GET", "/nodes/01aaaaaaaaaaaaaaaaaaaaaaaa", nil)
		details, _ := r.body["error"].(map[string]any)["details"].(map[string]any)
		if str(details, "resource") != "node" {
			t.Fatalf("%s", r.raw)
		}
	})

	t.Run("the operator log by node", func(t *testing.T) {
		// Everything on the org and below: the creates under it, the project reads, the echo.
		events := list(o.must(200, "GET", "/operator-audit-events?node_id="+org+"&limit=100", nil), "audit_events")
		routes := map[string]bool{}
		for _, e := range events {
			routes[str(e, "route")] = true
			if str(e, "root_id") != org {
				t.Errorf("event under the org with root %v", e["root_id"])
			}
		}
		for _, want := range []string{
			"/nodes/{root_id}/members", "/as/{member}/nodes/{node_id}/children/{kind}",
			"/nodes/{node_id}/bindings", "/as/{member}/nodes/{node_id}/echo", "/nodes/{node_id}/restore",
		} {
			if !routes[want] {
				t.Errorf("no event for %s under the org", want)
			}
		}
	})

	t.Run("a slug is released only from a purged root", func(t *testing.T) {
		o.must(409, "POST", "/nodes/"+org+"/release-slug", nil)
	})

	t.Run("Plat5's audit reads go through", func(t *testing.T) {
		// Operator requests aren't in Plat5's log; the read itself works by node.
		r := o.must(200, "GET", "/nodes/"+org+"/audit-events", nil)
		if _, ok := r.body["audit_events"]; !ok {
			t.Fatalf("%s", r.raw)
		}
	})

	t.Run("no token, no event", func(t *testing.T) {
		anon := &operator{t: t, base: o.base, http: o.http}
		anon.must(401, "GET", "/nodes/"+org, nil)
	})

	t.Run("security headers", func(t *testing.T) {
		r := o.must(200, "GET", "/nodes/"+url.PathEscape(org), nil)
		if r.header.Get("X-Content-Type-Options") != "nosniff" || r.header.Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%v", r.header)
		}
	})
}
