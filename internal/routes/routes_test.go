package routes

import (
	"os"
	"strings"
	"testing"
)

const valid = `
upstreams:
  identity:
    url: http://identity:3000
    routes:
      - path: /organizations
        methods: [GET]
      - path: /organizations/{organization_id}/members
        methods: [GET, POST]
        action: org.members
        resource: { type: organization, id: organization_id }
      - path: /organizations/new
        methods: [POST]
  billing:
    url: https://billing.internal
    routes:
      - path: /organizations/{organization_id}
        methods: [GET]
`

// one wraps route YAML lines in a single upstream.
func one(routes string) string {
	return "upstreams:\n  x:\n    url: http://x\n    routes:\n" + routes
}

func TestParseValid(t *testing.T) {
	tab, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if tab.Len() != 4 {
		t.Fatalf("len = %d", tab.Len())
	}
	for _, r := range tab.Routes() {
		want := "http://identity:3000"
		if r.Upstream() == "billing" {
			want = "https://billing.internal"
		} else if r.Upstream() != "identity" {
			t.Fatalf("%s: upstream %q", r.Path, r.Upstream())
		}
		if r.Target().String() != want {
			t.Fatalf("%s: target %s, want %s", r.Path, r.Target(), want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"missing upstreams":        `{}`,
		"old format":               "routes:\n  - path: /a\n    methods: [GET]\n    upstream: http://x\n",
		"bad upstream name":        "upstreams:\n  Identity:\n    url: http://x\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"empty upstream":           "upstreams:\n  x:\n",
		"duplicate upstream":       "upstreams:\n  x:\n    url: http://x\n    routes:\n      - path: /a\n        methods: [GET]\n  x:\n    url: http://y\n    routes:\n      - path: /b\n        methods: [GET]\n",
		"no routes":                "upstreams:\n  x:\n    url: http://x\n",
		"no url":                   "upstreams:\n  x:\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url path":                 "upstreams:\n  x:\n    url: http://x/v1\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url slash":                "upstreams:\n  x:\n    url: http://x/\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url scheme":               "upstreams:\n  x:\n    url: ftp://x\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url no host":              "upstreams:\n  x:\n    url: identity:3000\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"route upstream":           one("      - path: /a\n        methods: [GET]\n        upstream: http://x\n"),
		"unknown field":            one("      - path: /a\n        methods: [GET]\n        scope: user\n"),
		"root path":                one("      - path: /\n        methods: [GET]\n"),
		"no leading slash":         one("      - path: a\n        methods: [GET]\n"),
		"trailing slash":           one("      - path: /a/\n        methods: [GET]\n"),
		"empty segment":            one("      - path: /a//b\n        methods: [GET]\n"),
		"dot segment":              one("      - path: /a/../b\n        methods: [GET]\n"),
		"encoded literal":          one("      - path: /a%2Fb\n        methods: [GET]\n"),
		"bad param name":           one("      - path: /a/{Org}\n        methods: [GET]\n"),
		"partial param":            one("      - path: /a/x{id}\n        methods: [GET]\n"),
		"repeated param":           one("      - path: /a/{id}/b/{id}\n        methods: [GET]\n"),
		"no methods":               one("      - path: /a\n"),
		"options":                  one("      - path: /a\n        methods: [OPTIONS]\n"),
		"lowercase method":         one("      - path: /a\n        methods: [get]\n"),
		"repeated method":          one("      - path: /a\n        methods: [GET, GET]\n"),
		"resource no type":         one("      - path: /a/{id}\n        methods: [GET]\n        resource: { id: id }\n"),
		"resource bad id":          one("      - path: /a/{id}\n        methods: [GET]\n        resource: { type: a, id: other }\n"),
		"blank action":             one("      - path: /a\n        methods: [GET]\n        action: \" \"\n"),
		"literal vs param":         one("      - path: /a/new\n        methods: [GET]\n      - path: /a/{id}\n        methods: [GET, POST]\n"),
		"duplicate route":          one("      - path: /a\n        methods: [GET]\n      - path: /a\n        methods: [GET]\n"),
		"overlap across upstreams": "upstreams:\n  x:\n    url: http://x\n    routes:\n      - path: /a/{x}\n        methods: [GET]\n  y:\n    url: http://y\n    routes:\n      - path: /a/{y}\n        methods: [GET]\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestParseDisjointMethodsAllowed(t *testing.T) {
	in := one("      - path: /a/new\n        methods: [POST]\n      - path: /a/{id}\n        methods: [GET]\n")
	if _, err := Parse([]byte(in)); err != nil {
		t.Fatal(err)
	}
}

func TestShippedCatalog(t *testing.T) {
	b, err := os.ReadFile("../../routes.yml")
	if err != nil {
		t.Fatal(err)
	}
	tab, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	segs, err := Segments("/organizations/org_1/audit-events")
	if err != nil {
		t.Fatal(err)
	}
	r, params := tab.Match("GET", segs)
	if r == nil || r.Upstream() != "audit" || r.Target().String() != "http://audit:3002" {
		t.Fatalf("audit route: %+v", r)
	}
	if params["organization_id"] != "org_1" {
		t.Fatalf("params: %v", params)
	}
	if w, _ := tab.Match("POST", segs); w != nil {
		t.Fatal("audit log read must be GET only")
	}
}

func TestMatch(t *testing.T) {
	tab, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path string
		route        string
		params       map[string]string
	}{
		{"GET", "/organizations", "/organizations", nil},
		{"GET", "/organizations/org_1/members", "/organizations/{organization_id}/members", map[string]string{"organization_id": "org_1"}},
		{"POST", "/organizations/new", "/organizations/new", nil},
		{"GET", "/organizations/new", "/organizations/{organization_id}", map[string]string{"organization_id": "new"}},
		{"DELETE", "/organizations/org_1", "", nil},
		{"GET", "/organizations/org_1/members/x", "", nil},
		{"GET", "/users", "", nil},
		{"GET", "/", "", nil},
		{"GET", "/organizations/org%201", "/organizations/{organization_id}", map[string]string{"organization_id": "org 1"}},
	}
	for _, c := range cases {
		segs, err := Segments(c.path)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		r, params := tab.Match(c.method, segs)
		got := ""
		if r != nil {
			got = r.Path
		}
		if got != c.route {
			t.Errorf("%s %s: route %q, want %q", c.method, c.path, got, c.route)
			continue
		}
		if len(params) != len(c.params) {
			t.Errorf("%s %s: params %v, want %v", c.method, c.path, params, c.params)
		}
		for k, v := range c.params {
			if params[k] != v {
				t.Errorf("%s %s: %s = %q, want %q", c.method, c.path, k, params[k], v)
			}
		}
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
	segs, err := Segments("/users/u%201/memberships")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(segs, "|") != "users|u 1|memberships" {
		t.Fatalf("segs = %v", segs)
	}
}
