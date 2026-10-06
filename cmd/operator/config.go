package main

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// config is slice 1's environment. Contract: docs/v1.md, docs/idp.md.
type config struct {
	Addr            string
	InternalPort    string
	RoutesFile      string
	Issuer          string
	JWKSURI         string
	Audiences       []string
	IDClaim         string
	AllowedOrigins  []string
	UpstreamTimeout time.Duration
	AuthzURL        string
}

func loadConfig(getenv func(string) string) (config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	c := config{
		Addr:         get("ADDR", ":5004"),
		InternalPort: get("INTERNAL_PORT", "8004"),
		RoutesFile:   get("ROUTES_FILE", "routes.yml"),
		Issuer:       get("AUTH_ISSUER", ""),
		JWKSURI:      get("AUTH_JWKS_URI", ""),
		Audiences:    list(getenv("AUTH_AUDIENCES")),
		IDClaim:      get("AUTH_OPERATOR_ID_CLAIM", "sub"),
		AuthzURL:     get("AUTHZ_URL", ""),
	}
	var errs []error
	if c.Issuer == "" {
		errs = append(errs, errors.New("AUTH_ISSUER is required"))
	}
	if u, err := url.Parse(c.JWKSURI); c.JWKSURI == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, errors.New("AUTH_JWKS_URI must be an http or https URL"))
	}
	if len(c.Audiences) == 0 {
		errs = append(errs, errors.New("AUTH_AUDIENCES is required"))
	}
	if p, err := strconv.Atoi(c.InternalPort); err != nil || p < 1 || p > 65535 {
		errs = append(errs, errors.New("INTERNAL_PORT must be a port number"))
	}
	ms, err := strconv.Atoi(get("UPSTREAM_TIMEOUT_MS", "30000"))
	if err != nil || ms <= 0 {
		errs = append(errs, errors.New("UPSTREAM_TIMEOUT_MS must be a positive integer"))
	}
	c.UpstreamTimeout = time.Duration(ms) * time.Millisecond
	for _, o := range list(getenv("ALLOWED_ORIGINS")) {
		origin, err := parseOrigin(o)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.AllowedOrigins = append(c.AllowedOrigins, origin)
	}
	if c.AuthzURL != "" {
		errs = append(errs, errors.New("AUTHZ_URL is slice 2 and not supported yet; unset it"))
	}
	return c, errors.Join(errs...)
}

func list(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseOrigin accepts scheme://host[:port] only. No wildcard.
func parseOrigin(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil || strings.Contains(u.Host, "*") {
		return "", fmt.Errorf("ALLOWED_ORIGINS: %q is not scheme://host[:port]", s)
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}
