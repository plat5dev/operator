// Package routes loads this gateway's route file and matches requests. Contract:
// docs/reference/routes.md.
package routes

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Service is one service address and the routes this gateway forwards to it.
type Service struct {
	URL    string   `yaml:"url"`
	Routes []*Route `yaml:"routes"`
}

type Route struct {
	Path    string   `yaml:"path"`
	Methods []string `yaml:"methods"`
	// Node is the param holding the Plat5 node id the route addresses, or "".
	Node string `yaml:"node"`
	// AsMember also serves the route at /as/{member} + Path.
	AsMember bool `yaml:"as_member"`

	segments []segment
	methods  map[string]bool
	service  string
	target   *url.URL
}

// Service is the name of the service this route belongs to.
func (r *Route) Service() string { return r.service }

// Target is the parsed upstream URL: scheme, host, and port.
func (r *Route) Target() *url.URL { return r.target }

type segment struct {
	literal string
	param   string
}

type Table struct {
	routes []*Route
}

func (t *Table) Len() int { return len(t.routes) }

// Routes returns every route, grouped by service name.
func (t *Table) Routes() []*Route { return t.routes }

// HasNodes is whether any route names a node, so the gateway needs identity.
func (t *Table) HasNodes() bool {
	for _, r := range t.routes {
		if r.Node != "" {
			return true
		}
	}
	return false
}

// asPrefix is the reserved first segment of /as/{member}/…, and member its param.
const (
	asPrefix    = "as"
	memberParam = "member"
)

var (
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	paramRe = regexp.MustCompile(`^\{([a-z][a-z0-9_]*)\}$`)
	allowed = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
)

func Load(path string) (*Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

func Parse(data []byte) (*Table, error) {
	var file struct {
		Services map[string]*Service `yaml:"services"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return nil, err
	}
	if file.Services == nil {
		return nil, errors.New("services: missing")
	}
	names := make([]string, 0, len(file.Services))
	for name := range file.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	var all []*Route
	for _, name := range names {
		svc := file.Services[name]
		if !nameRe.MatchString(name) {
			return nil, fmt.Errorf("services.%s: name must match %s", name, nameRe)
		}
		if svc == nil {
			return nil, fmt.Errorf("services.%s: empty", name)
		}
		target, err := parseURL(svc.URL)
		if err != nil {
			return nil, fmt.Errorf("services.%s.url: %w", name, err)
		}
		if len(svc.Routes) == 0 {
			return nil, fmt.Errorf("services.%s.routes: missing", name)
		}
		for i, r := range svc.Routes {
			if r == nil {
				return nil, fmt.Errorf("services.%s.routes[%d]: empty", name, i)
			}
			r.service, r.target = name, target
			if err := r.compile(); err != nil {
				return nil, fmt.Errorf("services.%s.routes[%d] %s: %w", name, i, r.Path, err)
			}
			all = append(all, r)
		}
	}
	for i, a := range all {
		for _, b := range all[i+1:] {
			if overlaps(a, b) {
				return nil, fmt.Errorf("routes %s (%s) and %s (%s) can match the same request", a.Path, a.service, b.Path, b.service)
			}
		}
	}
	return &Table{routes: all}, nil
}

func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("must be scheme://host[:port] with no path")
	}
	return u, nil
}

func (r *Route) compile() error {
	if !strings.HasPrefix(r.Path, "/") || r.Path == "/" {
		return errors.New("path must start with / and have a segment")
	}
	seen := map[string]bool{}
	for i, s := range strings.Split(r.Path[1:], "/") {
		if i == 0 && s == asPrefix {
			return errors.New("/as/ is reserved for acting as a member")
		}
		if m := paramRe.FindStringSubmatch(s); m != nil {
			if m[1] == memberParam {
				return errors.New("parameter member is reserved for /as/{member}")
			}
			if seen[m[1]] {
				return fmt.Errorf("parameter %s repeated", m[1])
			}
			seen[m[1]] = true
			r.segments = append(r.segments, segment{param: m[1]})
			continue
		}
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "{}%\\") {
			return fmt.Errorf("bad segment %q", s)
		}
		r.segments = append(r.segments, segment{literal: s})
	}

	if len(r.Methods) == 0 {
		return errors.New("methods: missing")
	}
	r.methods = map[string]bool{}
	for _, m := range r.Methods {
		if !allowed[m] {
			return fmt.Errorf("method %q not allowed", m)
		}
		if r.methods[m] {
			return fmt.Errorf("method %s repeated", m)
		}
		r.methods[m] = true
	}

	if r.Node != "" && !seen[r.Node] {
		return fmt.Errorf("node %q is not a path parameter", r.Node)
	}
	if r.AsMember && r.Node == "" {
		return errors.New("as_member needs node: the member is checked in the node's tree")
	}
	return nil
}

func overlaps(a, b *Route) bool {
	if len(a.segments) != len(b.segments) {
		return false
	}
	shared := false
	for m := range a.methods {
		if b.methods[m] {
			shared = true
			break
		}
	}
	if !shared {
		return false
	}
	for i, s := range a.segments {
		t := b.segments[i]
		if s.param == "" && t.param == "" && s.literal != t.literal {
			return false
		}
	}
	return true
}

// Match is a matched request.
type Match struct {
	Route *Route
	// Params are the path params by name, member included when acting as one.
	Params map[string]string
	// Member is the member from /as/{member}, or "".
	Member string
}

// Template is the matched path template, /as/{member} included when acting as one.
func (m *Match) Template() string {
	if m.Member != "" {
		return "/" + asPrefix + "/{" + memberParam + "}" + m.Route.Path
	}
	return m.Route.Path
}

// NodeID is the route node's id, or "" when the route names none.
func (m *Match) NodeID() string {
	if m.Route.Node == "" {
		return ""
	}
	return m.Params[m.Route.Node]
}

// Match returns the route for this method and decoded path segments, or nil. A path
// starting /as/{member}/ matches only routes with as_member, against the rest.
func (t *Table) Match(method string, segs []string) *Match {
	member := ""
	if len(segs) > 2 && segs[0] == asPrefix {
		member, segs = segs[1], segs[2:]
	}
	for _, r := range t.routes {
		if len(r.segments) != len(segs) || !r.methods[method] || (member != "" && !r.AsMember) {
			continue
		}
		params := map[string]string{}
		ok := true
		for i, s := range r.segments {
			if s.param != "" {
				params[s.param] = segs[i]
				continue
			}
			if s.literal != segs[i] {
				ok = false
				break
			}
		}
		if ok {
			if member != "" {
				params[memberParam] = member
			}
			return &Match{Route: r, Params: params, Member: member}
		}
	}
	return nil
}

// ErrBadPath is a path this gateway could read differently from the service.
var ErrBadPath = errors.New("bad path")

// Segments splits an escaped request path into decoded segments. "/" has none.
// Dot segments, empty segments, backslashes, and encoded / or \ are ErrBadPath.
func Segments(escaped string) ([]string, error) {
	if !strings.HasPrefix(escaped, "/") {
		return nil, ErrBadPath
	}
	if escaped == "/" {
		return nil, nil
	}
	parts := strings.Split(escaped[1:], "/")
	out := make([]string, len(parts))
	for i, s := range parts {
		if s == "" || strings.ContainsRune(s, '\\') {
			return nil, ErrBadPath
		}
		lower := strings.ToLower(s)
		if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
			return nil, ErrBadPath
		}
		d, err := url.PathUnescape(s)
		if err != nil || d == "." || d == ".." {
			return nil, ErrBadPath
		}
		out[i] = d
	}
	return out, nil
}
