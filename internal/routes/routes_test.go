package routes

import (
	"os"
	"strings"
	"testing"
)

const (
	nodeID   = "01jbxa5m7n9p1q3r5s7t9v1w3x"
	memberID = "01jbyb7g8h9j0k1m2n3p4q5r6s"
	userID   = "01jbzc0r1s2t3v4w5x6y7z8a9b"
)

const validConfig = `
control_plane:
  url: http://control-plane:5003

services:
  identity:
    url: http://identity:3000
  audit:
    url: http://audit:3002
  operator-audit:
    url: http://operator-audit:5005
  billing:
    url: https://billing.internal
    timeout: 15s
    routes:
      - path: /nodes/{node_id}/invoices
        node: node_id
        methods: [GET, POST]

authorizer:
  mode: authzen
  url: http://topaz:8383
  timeout: 3s
  token_env: TOPAZ_TOKEN

policy_claims: [groups, amr]
`

func TestParseValid(t *testing.T) {
	cfg, err := Parse([]byte(validConfig), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ControlPlaneURL != "http://control-plane:5003" || cfg.IdentityURL() != "http://identity:3000" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Authorizer.Mode != AuthorizerModeAuthZEN || cfg.Authorizer.URL != "http://topaz:8383" ||
		cfg.Authorizer.TokenEnv != "TOPAZ_TOKEN" {
		t.Fatalf("authorizer = %+v", cfg.Authorizer)
	}
	if strings.Join(cfg.PolicyClaims, ",") != "groups,amr" {
		t.Fatalf("policy_claims = %v", cfg.PolicyClaims)
	}
	if len(cfg.Services) != 4 {
		t.Fatalf("services = %d", len(cfg.Services))
	}
}

func TestParseOverrides(t *testing.T) {
	raw := `
services:
  identity:
    url: http://identity:3000
authorizer:
  mode: authzen
`
	cfg, err := Parse([]byte(raw), Overrides{
		ControlPlaneURL: "http://cp:5003/",
		AuthorizerMode:  "disabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ControlPlaneURL != "http://cp:5003" || cfg.Authorizer.Mode != AuthorizerModeDisabled {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"empty":                    `{}`,
		"authzen missing url":      "services:\n  x: { url: http://x }\n",
		"bad authz mode":           "services:\n  x: { url: http://x }\nauthorizer: { mode: magic }\n",
		"bad authz timeout":        "services:\n  x: { url: http://x }\nauthorizer: { mode: disabled, timeout: 0s }\n",
		"bad service name":         "services:\n  Identity: { url: http://x }\nauthorizer: { mode: disabled }\n",
		"no url without cp":        "services:\n  x: {}\nauthorizer: { mode: disabled }\n",
		"url path":                 "services:\n  x: { url: http://x/v1 }\nauthorizer: { mode: disabled }\n",
		"bad timeout":              "services:\n  x: { url: http://x, timeout: -1s }\nauthorizer: { mode: disabled }\n",
		"unknown field":            "unknown: 1\nservices:\n  x: { url: http://x }\nauthorizer: { mode: disabled }\n",
		"bad policy claim":         "services:\n  x: { url: http://x }\nauthorizer: { mode: disabled }\npolicy_claims: ['bad..claim']\n",
		"node not a param":         "services:\n  x:\n    url: http://x\n    routes:\n      - path: /a/{id}\n        node: other\n        methods: [GET]\nauthorizer: { mode: disabled }\n",
		"dotted param":             "services:\n  x:\n    url: http://x\n    routes:\n      - path: /projects/{project.id}\n        methods: [GET]\nauthorizer: { mode: disabled }\n",
		"overlapping routes":       "services:\n  x:\n    url: http://x\n    routes:\n      - path: /a/new\n        methods: [GET]\n      - path: /a/{id}\n        methods: [GET]\nauthorizer: { mode: disabled }\n",
		"options method":           "services:\n  x:\n    url: http://x\n    routes:\n      - path: /a\n        methods: [OPTIONS]\nauthorizer: { mode: disabled }\n",
		"service name too long":    "services:\n  " + strings.Repeat("a", 129) + ": { url: http://x }\nauthorizer: { mode: disabled }\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in), Overrides{}); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestShippedFiles(t *testing.T) {
	shippedRaw, err := os.ReadFile("../../operator.yml")
	if err != nil {
		t.Fatal(err)
	}
	// Shipped operator.yml defaults to mode: authzen and requires AUTHORIZER_URL (or AUTHORIZER_MODE=disabled).
	if _, err := Parse(shippedRaw, Overrides{}); err == nil {
		t.Fatal("shipped operator.yml must fail closed without authorizer.url")
	}
	cfg, err := Parse(shippedRaw, Overrides{AuthorizerURL: "http://topaz:8383"})
	if err != nil {
		t.Fatalf("shipped operator.yml with url: %v", err)
	}
	reg := NewRegistry(cfg)
	m, err := reg.Resolve("GET", "/services/audit/nodes/"+nodeID+"/audit-events")
	if err != nil || m == nil || m.Service != "audit" || m.Route != "/nodes/{node_id}/audit-events" || m.NodeID != nodeID {
		t.Fatalf("audit resolve: %+v %v", m, err)
	}

	devRaw, err := os.ReadFile("../../compose/operator.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(devRaw, Overrides{}); err != nil {
		t.Fatalf("compose/operator.yml: %v", err)
	}
}

func TestRegistryResolveAndControlPlaneMerge(t *testing.T) {
	cfg, err := Parse([]byte(validConfig), Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(cfg)

	// Built-in identity template works with zero route YAML.
	m, err := reg.Resolve("POST", "/services/identity/nodes/"+nodeID+"/children/project")
	if err != nil || m == nil || m.Service != "identity" || m.UpstreamPath != "/nodes/"+nodeID+"/children/project" ||
		m.Route != "/nodes/{node_id}/children/{kind}" || m.NodeID != nodeID || m.Params["kind"] != "project" {
		t.Fatalf("identity resolve: %+v %v", m, err)
	}

	// Unlisted path on known service resolves unscoped (no magic ULID scanning).
	m, err = reg.Resolve("GET", "/services/billing/internal/debug/"+nodeID)
	if err != nil || m == nil || m.Service != "billing" || m.UpstreamPath != "/internal/debug/"+nodeID ||
		m.Route != "" || m.NodeID != "" {
		t.Fatalf("unlisted resolve: %+v %v", m, err)
	}

	// Non-/services path or unknown service returns nil (404).
	if m, err := reg.Resolve("GET", "/nodes/"+nodeID); err != nil || m != nil {
		t.Fatalf("flat path: %+v %v", m, err)
	}
	if m, err := reg.Resolve("GET", "/services/unknown/ping"); err != nil || m != nil {
		t.Fatalf("unknown service: %+v %v", m, err)
	}

	// Feed a control-plane snapshot: discovers "api" service and its upstream templates.
	feed := `{
		"revision": 7,
		"services": {
			"api": {
				"url": "http://api:3000",
				"timeouts": { "response": "12s" },
				"routes": [
					{
						"method": "POST",
						"scope": "project",
						"upstream": "/projects/{project.id}/tasks/{task_id}",
						"permissions": ["task:write"]
					},
					{
						"method": "GET",
						"scope": "user",
						"upstream": "/users/{user.id}/prefs",
						"permissions": null
					}
				]
			}
		}
	}`
	snap, err := ParseGatewayFeed([]byte(feed))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetControlPlane(snap)

	m, err = reg.Resolve("POST", "/services/api/projects/"+nodeID+"/tasks/t_1")
	if err != nil || m == nil || m.Service != "api" || m.Target.String() != "http://api:3000" ||
		m.Route != "/projects/{project_id}/tasks/{task_id}" || m.NodeID != nodeID ||
		m.Params["task_id"] != "t_1" || len(m.Permissions) != 1 || m.Permissions[0] != "task:write" {
		t.Fatalf("api resolve: %+v %v", m, err)
	}
	m, err = reg.Resolve("GET", "/services/api/users/"+userID+"/prefs")
	if err != nil || m == nil || m.Route != "/users/{user_id}/prefs" || m.NodeID != "" {
		t.Fatalf("user scope resolve: %+v %v", m, err)
	}
}

func TestSegments(t *testing.T) {
	bad := []string{
		"", "organizations", "//", "/a//b", "/a/", "/a/./b", "/a/../b",
		"/a/%2e%2e/b", "/a/%2E", "/a%2Fb", "/a%2fb", "/a%5Cb", `/a\b`, "/a/%zz",
	}
	for _, p := range bad {
		if _, err := Segments(p); err == nil {
			t.Errorf("%q: want ErrBadPath", p)
		}
	}
	segs, err := Segments("/services/identity/members/m%201/nodes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(segs, "|") != "services|identity|members|m 1|nodes" {
		t.Fatalf("segs = %v", segs)
	}
}
