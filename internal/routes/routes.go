// Package routes loads this gateway's route file and matches requests. Contract: docs/routes.md.
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

// Resource names the authz resource for a route. Validated now, used in slice 2.
type Resource struct {
	Type string `yaml:"type"`
	ID   string `yaml:"id"`
}

// Upstream is one service address and the routes this gateway forwards to it.
type Upstream struct {
	URL    string   `yaml:"url"`
	Routes []*Route `yaml:"routes"`
}

type Route struct {
	Path     string    `yaml:"path"`
	Methods  []string  `yaml:"methods"`
	Action   string    `yaml:"action"`
	Resource *Resource `yaml:"resource"`

	segments []segment
	methods  map[string]bool
	upstream string
	target   *url.URL
}

// Upstream is the name of the upstream this route belongs to.
func (r *Route) Upstream() string { return r.upstream }

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

// Routes returns every route, grouped by upstream name.
func (t *Table) Routes() []*Route { return t.routes }

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
		Upstreams map[string]*Upstream `yaml:"upstreams"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return nil, err
	}
	if file.Upstreams == nil {
		return nil, errors.New("upstreams: missing")
	}
	names := make([]string, 0, len(file.Upstreams))
	for name := range file.Upstreams {
		names = append(names, name)
	}
	sort.Strings(names)

	var all []*Route
	for _, name := range names {
		up := file.Upstreams[name]
		if !nameRe.MatchString(name) {
			return nil, fmt.Errorf("upstreams.%s: name must match %s", name, nameRe)
		}
		if up == nil {
			return nil, fmt.Errorf("upstreams.%s: empty", name)
		}
		target, err := parseURL(up.URL)
		if err != nil {
			return nil, fmt.Errorf("upstreams.%s.url: %w", name, err)
		}
		if len(up.Routes) == 0 {
			return nil, fmt.Errorf("upstreams.%s.routes: missing", name)
		}
		for i, r := range up.Routes {
			if r == nil {
				return nil, fmt.Errorf("upstreams.%s.routes[%d]: empty", name, i)
			}
			r.upstream, r.target = name, target
			if err := r.compile(); err != nil {
				return nil, fmt.Errorf("upstreams.%s.routes[%d] %s: %w", name, i, r.Path, err)
			}
			all = append(all, r)
		}
	}
	for i, a := range all {
		for _, b := range all[i+1:] {
			if overlaps(a, b) {
				return nil, fmt.Errorf("routes %s (%s) and %s (%s) can match the same request", a.Path, a.upstream, b.Path, b.upstream)
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
	for _, s := range strings.Split(r.Path[1:], "/") {
		if m := paramRe.FindStringSubmatch(s); m != nil {
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

	if r.Action != "" && strings.TrimSpace(r.Action) == "" {
		return errors.New("action: blank")
	}
	if r.Resource != nil {
		if r.Resource.Type == "" {
			return errors.New("resource.type: missing")
		}
		if !seen[r.Resource.ID] {
			return fmt.Errorf("resource.id %q is not a path parameter", r.Resource.ID)
		}
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

// Match returns the route for this method and decoded path segments, and its parameters by name.
func (t *Table) Match(method string, segs []string) (*Route, map[string]string) {
	for _, r := range t.routes {
		if len(r.segments) != len(segs) || !r.methods[method] {
			continue
		}
		var params map[string]string
		ok := true
		for i, s := range r.segments {
			if s.param != "" {
				if params == nil {
					params = map[string]string{}
				}
				params[s.param] = segs[i]
				continue
			}
			if s.literal != segs[i] {
				ok = false
				break
			}
		}
		if ok {
			return r, params
		}
	}
	return nil, nil
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
