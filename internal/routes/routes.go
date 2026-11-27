// Package routes loads operator.yml, merges Plat5 control-plane service/route discovery
// with built-in core templates, and resolves /services/{service}/{path...} requests.
// Contract: docs/reference/config.md.
package routes

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/plat5dev/operator/internal/audit"
)

const (
	AuthorizerModeAuthZEN  = "authzen"
	AuthorizerModeDisabled = "disabled"
	defaultAuthzTimeout    = 2 * time.Second
	maxPolicyClaims        = 32
)

// Overrides are optional environment overrides layered onto operator.yml.
type Overrides struct {
	ControlPlaneURL string
	AuthorizerMode  string
	AuthorizerURL   string
}

// Config is the validated operator.yml file.
type Config struct {
	ControlPlaneURL string
	Services        map[string]*Service
	Authorizer      AuthorizerConfig
	PolicyClaims    []string
}

// AuthorizerConfig is the validated authorizer section.
type AuthorizerConfig struct {
	Mode     string
	URL      string
	Timeout  time.Duration
	TokenEnv string
}

// Service is one explicit service entry in operator.yml.
type Service struct {
	Name    string
	Target  *url.URL
	Timeout time.Duration
	Routes  []*Route
}

// Route is a single compiled route template (for route normalization and node extraction).
type Route struct {
	Path        string   `yaml:"path"`
	Methods     []string `yaml:"methods"`
	Node        string   `yaml:"node"`
	Permissions []string `yaml:"-"`

	segments   []segment
	paramCount int
	verbs      map[string]bool
}

type segment struct {
	literal string
	param   string
}

var (
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	paramRe  = regexp.MustCompile(`^\{([a-z][a-z0-9_]*)\}$`)
	dottedRe = regexp.MustCompile(`^\{[^{}]*\.[^{}]*\}$`)
	claimRe  = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)
	allowed  = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
)

type rawFile struct {
	ControlPlane *rawControlPlane       `yaml:"control_plane"`
	Services     map[string]*rawService `yaml:"services"`
	Authorizer   *rawAuthorizer         `yaml:"authorizer"`
	PolicyClaims []string               `yaml:"policy_claims"`
}

type rawControlPlane struct {
	URL string `yaml:"url"`
}

type rawService struct {
	URL     string   `yaml:"url"`
	Timeout string   `yaml:"timeout"`
	Routes  []*Route `yaml:"routes"`
}

type rawAuthorizer struct {
	Mode     string `yaml:"mode"`
	URL      string `yaml:"url"`
	Timeout  string `yaml:"timeout"`
	TokenEnv string `yaml:"token_env"`
}

func Load(path string, ov Overrides) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(data, ov)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func Parse(data []byte, ov Overrides) (*Config, error) {
	var file rawFile
	if len(bytes.TrimSpace(data)) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
	}

	cfg := &Config{
		Services: map[string]*Service{},
		Authorizer: AuthorizerConfig{
			Mode:    AuthorizerModeAuthZEN,
			Timeout: defaultAuthzTimeout,
		},
	}

	if file.ControlPlane != nil {
		cfg.ControlPlaneURL = strings.TrimSpace(file.ControlPlane.URL)
	}
	if ov.ControlPlaneURL != "" {
		cfg.ControlPlaneURL = strings.TrimSpace(ov.ControlPlaneURL)
	}
	if cfg.ControlPlaneURL != "" {
		u, err := parseBaseURL(cfg.ControlPlaneURL)
		if err != nil {
			return nil, fmt.Errorf("control_plane.url: %w", err)
		}
		cfg.ControlPlaneURL = strings.TrimRight(u.String(), "/")
	}

	if file.Authorizer != nil {
		if m := strings.TrimSpace(file.Authorizer.Mode); m != "" {
			cfg.Authorizer.Mode = m
		}
		cfg.Authorizer.URL = strings.TrimSpace(file.Authorizer.URL)
		if t := strings.TrimSpace(file.Authorizer.Timeout); t != "" {
			d, err := time.ParseDuration(t)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("authorizer.timeout: must be a positive duration such as 2s")
			}
			cfg.Authorizer.Timeout = d
		}
		cfg.Authorizer.TokenEnv = strings.TrimSpace(file.Authorizer.TokenEnv)
	}
	if ov.AuthorizerMode != "" {
		cfg.Authorizer.Mode = strings.TrimSpace(ov.AuthorizerMode)
	}
	if ov.AuthorizerURL != "" {
		cfg.Authorizer.URL = strings.TrimSpace(ov.AuthorizerURL)
	}
	switch cfg.Authorizer.Mode {
	case AuthorizerModeAuthZEN:
		if cfg.Authorizer.URL == "" {
			return nil, errors.New("authorizer.url is required when authorizer.mode is authzen")
		}
		u, err := parseBaseURL(cfg.Authorizer.URL)
		if err != nil {
			return nil, fmt.Errorf("authorizer.url: %w", err)
		}
		cfg.Authorizer.URL = strings.TrimRight(u.String(), "/")
	case AuthorizerModeDisabled:
		if cfg.Authorizer.URL != "" {
			if _, err := parseBaseURL(cfg.Authorizer.URL); err != nil {
				return nil, fmt.Errorf("authorizer.url: %w", err)
			}
		}
	default:
		return nil, fmt.Errorf("authorizer.mode: must be %q or %q", AuthorizerModeAuthZEN, AuthorizerModeDisabled)
	}

	if len(file.PolicyClaims) > maxPolicyClaims {
		return nil, fmt.Errorf("policy_claims: at most %d claims", maxPolicyClaims)
	}
	seenClaim := map[string]bool{}
	for _, c := range file.PolicyClaims {
		c = strings.TrimSpace(c)
		if !claimRe.MatchString(c) {
			return nil, fmt.Errorf("policy_claims: invalid claim path %q", c)
		}
		if !seenClaim[c] {
			seenClaim[c] = true
			cfg.PolicyClaims = append(cfg.PolicyClaims, c)
		}
	}

	names := make([]string, 0, len(file.Services))
	for name := range file.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		raw := file.Services[name]
		if !nameRe.MatchString(name) || len(name) > audit.MaxServiceLen {
			return nil, fmt.Errorf("services.%s: name must match %s and be at most %d bytes", name, nameRe, audit.MaxServiceLen)
		}
		if raw == nil {
			return nil, fmt.Errorf("services.%s: empty", name)
		}
		svc := &Service{Name: name}
		if uStr := strings.TrimSpace(raw.URL); uStr != "" {
			u, err := parseURL(uStr)
			if err != nil {
				return nil, fmt.Errorf("services.%s.url: %w", name, err)
			}
			svc.Target = u
		} else if cfg.ControlPlaneURL == "" {
			return nil, fmt.Errorf("services.%s.url: required when control_plane.url is not configured", name)
		}
		if raw.Timeout != "" {
			d, err := time.ParseDuration(raw.Timeout)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("services.%s.timeout: must be a positive duration", name)
			}
			svc.Timeout = d
		}
		for i, r := range raw.Routes {
			if r == nil {
				return nil, fmt.Errorf("services.%s.routes[%d]: empty", name, i)
			}
			if err := r.compile(); err != nil {
				return nil, fmt.Errorf("services.%s.routes[%d] %s: %w", name, i, r.Path, err)
			}
			svc.Routes = append(svc.Routes, r)
		}
		for i, a := range svc.Routes {
			for _, b := range svc.Routes[i+1:] {
				if overlaps(a, b) {
					return nil, fmt.Errorf("services.%s: routes %s and %s can match the same request", name, a.Path, b.Path)
				}
			}
		}
		cfg.Services[name] = svc
	}

	if len(cfg.Services) == 0 && cfg.ControlPlaneURL == "" {
		return nil, errors.New("at least one service or control_plane.url is required")
	}

	return cfg, nil
}

// IdentityURL returns services.identity.url if explicitly configured, else "".
func (c *Config) IdentityURL() string {
	if s, ok := c.Services["identity"]; ok && s.Target != nil {
		return s.Target.String()
	}
	return ""
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

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("must be an http or https URL with no query or credentials")
	}
	return u, nil
}

func (r *Route) compile() error {
	if !strings.HasPrefix(r.Path, "/") || r.Path == "/" {
		return errors.New("path must start with / and have a segment")
	}
	if len(r.Path) > audit.MaxRouteLen {
		return fmt.Errorf("path exceeds %d bytes", audit.MaxRouteLen)
	}
	seen := map[string]bool{}
	for _, s := range strings.Split(r.Path[1:], "/") {
		if dottedRe.MatchString(s) {
			return fmt.Errorf("parameter %s has a dot: params are bare", s)
		}
		if m := paramRe.FindStringSubmatch(s); m != nil {
			if len(m[1]) > audit.MaxParamNameLen {
				return fmt.Errorf("parameter %s exceeds %d bytes", m[1], audit.MaxParamNameLen)
			}
			if seen[m[1]] {
				return fmt.Errorf("parameter %s repeated", m[1])
			}
			seen[m[1]] = true
			r.paramCount++
			if r.paramCount > audit.MaxParams {
				return fmt.Errorf("more than %d path parameters", audit.MaxParams)
			}
			r.segments = append(r.segments, segment{param: m[1]})
			continue
		}
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "{}%\\") {
			return fmt.Errorf("bad segment %q", s)
		}
		r.segments = append(r.segments, segment{literal: s})
	}

	if r.Node != "" && !seen[r.Node] {
		return fmt.Errorf("node %q is not a path parameter", r.Node)
	}

	if len(r.Methods) == 0 {
		return errors.New("methods: missing")
	}
	r.verbs = map[string]bool{}
	for _, verb := range r.Methods {
		if !allowed[verb] {
			return fmt.Errorf("method %q not allowed", verb)
		}
		if r.verbs[verb] {
			return fmt.Errorf("method %s repeated", verb)
		}
		r.verbs[verb] = true
	}
	return nil
}

func overlaps(a, b *Route) bool {
	if len(a.segments) != len(b.segments) {
		return false
	}
	shared := false
	for m := range a.verbs {
		if b.verbs[m] {
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

func (r *Route) match(method string, segs []string) (map[string]string, bool) {
	if len(r.segments) != len(segs) || !r.verbs[method] {
		return nil, false
	}
	for i, s := range r.segments {
		if s.param == "" && s.literal != segs[i] {
			return nil, false
		}
	}
	params := make(map[string]string, r.paramCount)
	for i, s := range r.segments {
		if s.param != "" {
			params[s.param] = segs[i]
		}
	}
	return params, true
}

// Resolution is the resolved target and optional matched route metadata for a
// /services/{service}/{upstream_path...} request.
type Resolution struct {
	Service      string
	Target       *url.URL
	Timeout      time.Duration
	UpstreamPath string
	// Route is the matched template (e.g. "/projects/{project_id}/tasks"), or "" for an unlisted path.
	Route       string
	Params      map[string]string
	NodeID      string
	Permissions []string
}

// Registry merges explicit operator.yml services, control-plane discovery, and built-in
// core templates. Safe for concurrent reads and atomic config/snapshot updates.
type Registry struct {
	mu   sync.RWMutex
	cfg  *Config
	snap *ControlPlaneSnapshot
}

func NewRegistry(cfg *Config) *Registry {
	return &Registry{cfg: cfg}
}

func (r *Registry) SetConfig(cfg *Config) {
	r.mu.Lock()
	r.cfg = cfg
	r.mu.Unlock()
}

func (r *Registry) SetControlPlane(snap *ControlPlaneSnapshot) {
	r.mu.Lock()
	r.snap = snap
	r.mu.Unlock()
}

func (r *Registry) Config() *Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

func (r *Registry) ServiceCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]bool{}
	for name, s := range r.cfg.Services {
		if s.Target != nil {
			seen[name] = true
		}
	}
	if r.snap != nil {
		for name, s := range r.snap.Services {
			if s.Target != nil {
				seen[name] = true
			}
		}
	}
	return len(seen)
}

// Resolve parses /services/{service}/{upstream_path...} and resolves the target service
// and optional route template. Returns (nil, nil) when the path does not address a known service.
func (r *Registry) Resolve(method, escapedPath string) (*Resolution, error) {
	segs, err := Segments(escapedPath)
	if err != nil {
		return nil, err
	}
	// Must be /services/{service}/{upstream_path...} (at least 3 segments).
	if len(segs) < 3 || segs[0] != "services" {
		return nil, nil
	}
	service := segs[1]
	if !nameRe.MatchString(service) {
		return nil, nil
	}
	// Preserve exact escaped upstream path by stripping "/services/{service}".
	prefix := "/services/" + service
	if !strings.HasPrefix(escapedPath, prefix+"/") {
		return nil, nil
	}
	upstreamPath := escapedPath[len(prefix):]
	upstreamSegs := segs[2:]

	r.mu.RLock()
	staticSvc := r.cfg.Services[service]
	var cpSvc *DiscoveredService
	if r.snap != nil {
		cpSvc = r.snap.Services[service]
	}
	r.mu.RUnlock()

	var target *url.URL
	var timeout time.Duration
	if staticSvc != nil && staticSvc.Target != nil {
		target = staticSvc.Target
	} else if cpSvc != nil && cpSvc.Target != nil {
		target = cpSvc.Target
	}
	if target == nil {
		return nil, nil
	}
	if staticSvc != nil && staticSvc.Timeout > 0 {
		timeout = staticSvc.Timeout
	} else if cpSvc != nil && cpSvc.Timeout > 0 {
		timeout = cpSvc.Timeout
	}

	res := &Resolution{
		Service:      service,
		Target:       target,
		Timeout:      timeout,
		UpstreamPath: upstreamPath,
		Params:       map[string]string{},
	}

	// Match templates in priority order: explicit operator.yml -> control-plane -> built-in core.
	var groups [][]*Route
	if staticSvc != nil && len(staticSvc.Routes) > 0 {
		groups = append(groups, staticSvc.Routes)
	}
	if cpSvc != nil && len(cpSvc.Routes) > 0 {
		groups = append(groups, cpSvc.Routes)
	}
	if bi, ok := builtinTemplates[service]; ok {
		groups = append(groups, bi)
	}
	for _, list := range groups {
		for _, rt := range list {
			if params, ok := rt.match(method, upstreamSegs); ok {
				res.Route = rt.Path
				res.Params = params
				if rt.Node != "" {
					res.NodeID = params[rt.Node]
				}
				res.Permissions = rt.Permissions
				return res, nil
			}
		}
	}
	return res, nil
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
