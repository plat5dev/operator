package routes

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Ids as Plat5 has them: lowercase ULIDs.
const (
	nodeID   = "01jbxa5m7n9p1q3r5s7t9v1w3x"
	memberID = "01jbyb7g8h9j0k1m2n3p4q5r6s"
	userID   = "01jbzc0r1s2t3v4w5x6y7z8a9b"
)

const valid = `
services:
  identity:
    url: http://identity:3000
    routes:
      - path: /nodes
        methods: [GET]
      - path: /nodes/{node_id}/children/{kind}
        node: node_id
        as_member: true
        methods: [GET, POST]
      - path: /nodes/search
        methods: [POST]
  billing:
    url: https://billing.internal
    routes:
      - path: /nodes/{node_id}
        node: node_id
        methods: [GET]
`

// one wraps route YAML lines in a single service.
func one(routes string) string {
	return "services:\n  x:\n    url: http://x\n    routes:\n" + routes
}

func TestParseValid(t *testing.T) {
	tab, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if tab.Len() != 4 || !tab.HasNodes() {
		t.Fatalf("len = %d, nodes = %v", tab.Len(), tab.HasNodes())
	}
	for _, r := range tab.Routes() {
		want := "http://identity:3000"
		if r.Service() == "billing" {
			want = "https://billing.internal"
		} else if r.Service() != "identity" {
			t.Fatalf("%s: service %q", r.Path, r.Service())
		}
		if r.Target().String() != want {
			t.Fatalf("%s: target %s, want %s", r.Path, r.Target(), want)
		}
	}
	plain, err := Parse([]byte(one("      - path: /a\n        methods: [GET]\n")))
	if err != nil || plain.HasNodes() {
		t.Fatalf("plain: %v, nodes = %v", err, plain.HasNodes())
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"missing services":        `{}`,
		"upstreams key":           "upstreams:\n  x:\n    url: http://x\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"bad service name":        "services:\n  Identity:\n    url: http://x\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"empty service":           "services:\n  x:\n",
		"duplicate service":       "services:\n  x:\n    url: http://x\n    routes:\n      - path: /a\n        methods: [GET]\n  x:\n    url: http://y\n    routes:\n      - path: /b\n        methods: [GET]\n",
		"no routes":               "services:\n  x:\n    url: http://x\n",
		"no url":                  "services:\n  x:\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url path":                "services:\n  x:\n    url: http://x/v1\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url slash":               "services:\n  x:\n    url: http://x/\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url scheme":              "services:\n  x:\n    url: ftp://x\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"url no host":             "services:\n  x:\n    url: identity:3000\n    routes:\n      - path: /a\n        methods: [GET]\n",
		"route upstream":          one("      - path: /a\n        methods: [GET]\n        upstream: http://x\n"),
		"unknown field":           one("      - path: /a\n        methods: [GET]\n        scope: user\n"),
		"authz field":             one("      - path: /a\n        methods: [GET]\n        action: a.read\n"),
		"root path":               one("      - path: /\n        methods: [GET]\n"),
		"no leading slash":        one("      - path: a\n        methods: [GET]\n"),
		"trailing slash":          one("      - path: /a/\n        methods: [GET]\n"),
		"empty segment":           one("      - path: /a//b\n        methods: [GET]\n"),
		"dot segment":             one("      - path: /a/../b\n        methods: [GET]\n"),
		"encoded literal":         one("      - path: /a%2Fb\n        methods: [GET]\n"),
		"bad param name":          one("      - path: /a/{Org}\n        methods: [GET]\n"),
		"partial param":           one("      - path: /a/x{id}\n        methods: [GET]\n"),
		"repeated param":          one("      - path: /a/{id}/b/{id}\n        methods: [GET]\n"),
		"no methods":              one("      - path: /a\n"),
		"options":                 one("      - path: /a\n        methods: [OPTIONS]\n"),
		"lowercase method":        one("      - path: /a\n        methods: [get]\n"),
		"repeated method":         one("      - path: /a\n        methods: [GET, GET]\n"),
		"node not a param":        one("      - path: /a/{id}\n        node: other\n        methods: [GET]\n"),
		"node a literal":          one("      - path: /a/{id}\n        node: a\n        methods: [GET]\n"),
		"as_member without node":  one("      - path: /a/{id}\n        as_member: true\n        methods: [GET]\n"),
		"verb as_member, no node": one("      - path: /a/{id}\n        methods:\n          POST: { as_member: true }\n"),
		"dotted param":            one("      - path: /projects/{project.id}\n        methods: [GET]\n"),
		"methods a scalar":        one("      - path: /a\n        methods: GET\n"),
		"methods an empty map":    one("      - path: /a\n        methods: {}\n"),
		"verb settings a list":    one("      - path: /a\n        methods:\n          GET: [as_member]\n"),
		"verb settings a scalar":  one("      - path: /a\n        methods:\n          GET: true\n"),
		"unknown verb setting":    one("      - path: /a/{id}\n        node: id\n        methods:\n          GET: { audit: false }\n"),
		"verb setting repeated":   one("      - path: /a/{id}\n        node: id\n        methods:\n          GET: { as_member: true, as_member: false }\n"),
		"verb as_member not bool": one("      - path: /a/{id}\n        node: id\n        methods:\n          GET: { as_member: yes please }\n"),
		"lowercase verb in a map": one("      - path: /a\n        methods:\n          get:\n"),
		"verb repeated in a map":  one("      - path: /a\n        methods:\n          GET:\n          GET:\n"),
		"literal vs param":        one("      - path: /a/new\n        methods: [GET]\n      - path: /a/{id}\n        methods: [GET, POST]\n"),
		"duplicate route":         one("      - path: /a\n        methods: [GET]\n      - path: /a\n        methods: [GET]\n"),
		"overlap across services": "services:\n  x:\n    url: http://x\n    routes:\n      - path: /a/{x}\n        methods: [GET]\n  y:\n    url: http://y\n    routes:\n      - path: /a/{y}\n        methods: [GET]\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(in)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

// A verb's settings override the route's: one path, a verb allowing X-Plat5-As-Member beside one that doesn't.
func TestMethodsMap(t *testing.T) {
	tab, err := Parse([]byte(one(`      - path: /projects/{project_id}/plans
        node: project_id
        methods:
          GET:
          POST: { as_member: true }
      - path: /projects/{project_id}/plans/{plan_id}
        node: project_id
        as_member: true
        methods:
          GET: { as_member: false }
          PATCH: {}
          DELETE:
`)))
	if err != nil {
		t.Fatal(err)
	}
	plans, plan := "/projects/"+nodeID+"/plans", "/projects/"+nodeID+"/plans/pro"
	for _, c := range []struct {
		method, path   string
		want, asMember bool
	}{
		{"GET", plans, true, false}, {"POST", plans, true, true},
		{"GET", plan, true, false}, {"PATCH", plan, true, true}, {"DELETE", plan, true, true},
		{"PUT", plan, false, false},
	} {
		m := match(t, tab, c.method, c.path)
		if (m != nil) != c.want {
			t.Errorf("%s %s: matched %v, want %v", c.method, c.path, m != nil, c.want)
			continue
		}
		if m != nil && m.AsMember != c.asMember {
			t.Errorf("%s %s: as_member %v, want %v", c.method, c.path, m.AsMember, c.asMember)
		}
	}
}

func TestAsAndMemberNotReserved(t *testing.T) {
	in := one("      - path: /as/{member}/items\n        methods: [GET]\n")
	tab, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	m := match(t, tab, "GET", "/as/"+memberID+"/items")
	if m == nil || m.Params["member"] != memberID {
		t.Fatalf("match = %+v", m)
	}
}

func TestParseDisjointMethodsAllowed(t *testing.T) {
	in := one("      - path: /a/new\n        methods: [POST]\n      - path: /a/{id}\n        methods: [GET]\n")
	if _, err := Parse([]byte(in)); err != nil {
		t.Fatal(err)
	}
}

// The shipped file and compose's dev file parse; compose's is the shipped one plus
// the echo service, so the dev stack forwards what a deployment would.
func TestShippedFiles(t *testing.T) {
	shipped := load(t, "../../routes.yml")
	dev := load(t, "../../compose/routes.yml")

	key := func(r *Route) string {
		return fmt.Sprintf("%s %s %v node=%s %s", r.Service(), r.Path, r.verbs, r.Node, r.Target())
	}
	want := map[string]bool{}
	for _, r := range shipped.Routes() {
		want[key(r)] = true
	}
	for _, r := range dev.Routes() {
		if r.Service() == "echo" {
			continue
		}
		if !want[key(r)] {
			t.Errorf("compose/routes.yml has %s, not in routes.yml", key(r))
		}
		delete(want, key(r))
	}
	for k := range want {
		t.Errorf("compose/routes.yml lacks %s", k)
	}

	m := match(t, shipped, "GET", "/nodes/"+nodeID+"/audit-events")
	if m == nil || m.Route.Service() != "audit" || m.Route.Target().String() != "http://audit:3002" || m.NodeID() != nodeID {
		t.Fatalf("audit route: %+v", m)
	}
	if match(t, shipped, "POST", "/nodes/"+nodeID+"/audit-events") != nil {
		t.Fatal("Plat5 audit read must be GET only")
	}
	if m := match(t, shipped, "GET", "/users/"+userID+"/audit-events"); m == nil || m.Route.Service() != "audit" || m.NodeID() != "" {
		t.Fatalf("a person's audit route: %+v", m)
	}
	// Creating keys mints customer credentials: not routed. Listing and revoking are.
	for _, p := range []string{"/users/" + userID + "/keys", "/nodes/" + nodeID + "/service-accounts/" + memberID + "/keys"} {
		if match(t, shipped, "POST", p) != nil {
			t.Errorf("POST %s is routed", p)
		}
		if match(t, shipped, "GET", p) == nil {
			t.Errorf("GET %s is not routed", p)
		}
	}
	if m := match(t, shipped, "POST", "/nodes/"+nodeID+"/children/project"); m == nil || !m.AsMember {
		t.Fatalf("children as a member: %+v", m)
	}
}

func load(t *testing.T, path string) *Table {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := Parse(b)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return tab
}

func match(t *testing.T, tab *Table, method, path string) *Match {
	t.Helper()
	segs, err := Segments(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return tab.Match(method, segs)
}

func TestMatch(t *testing.T) {
	tab, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path string
		template     string
		params       map[string]string
		asMember     bool
		node         string
	}{
		{"GET", "/nodes", "/nodes", nil, false, ""},
		{"GET", "/nodes/" + nodeID + "/children/project", "/nodes/{node_id}/children/{kind}",
			map[string]string{"node_id": nodeID, "kind": "project"}, true, nodeID},
		{"POST", "/nodes/" + nodeID + "/children/project", "/nodes/{node_id}/children/{kind}",
			map[string]string{"node_id": nodeID, "kind": "project"}, true, nodeID},
		{"POST", "/nodes/search", "/nodes/search", nil, false, ""},
		{"GET", "/nodes/search", "/nodes/{node_id}", map[string]string{"node_id": "search"}, false, "search"},
		{"GET", "/nodes/n%201", "/nodes/{node_id}", map[string]string{"node_id": "n 1"}, false, "n 1"},
		{"GET", "/as/" + memberID + "/nodes/" + nodeID, "", nil, false, ""},
		{"DELETE", "/nodes/" + nodeID, "", nil, false, ""},
		{"GET", "/nodes/" + nodeID + "/children/project/x", "", nil, false, ""},
		{"GET", "/users", "", nil, false, ""},
		{"GET", "/", "", nil, false, ""},
	}
	for _, c := range cases {
		m := match(t, tab, c.method, c.path)
		got := ""
		if m != nil {
			got = m.Template()
		}
		if got != c.template {
			t.Errorf("%s %s: route %q, want %q", c.method, c.path, got, c.template)
			continue
		}
		if m == nil {
			continue
		}
		if m.AsMember != c.asMember || m.NodeID() != c.node {
			t.Errorf("%s %s: asMember %v node %q, want %v %q", c.method, c.path, m.AsMember, m.NodeID(), c.asMember, c.node)
		}
		if len(m.Params) != len(c.params) {
			t.Errorf("%s %s: params %v, want %v", c.method, c.path, m.Params, c.params)
		}
		for k, v := range c.params {
			if m.Params[k] != v {
				t.Errorf("%s %s: %s = %q, want %q", c.method, c.path, k, m.Params[k], v)
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
	segs, err := Segments("/members/m%201/nodes")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(segs, "|") != "members|m 1|nodes" {
		t.Fatalf("segs = %v", segs)
	}
}
