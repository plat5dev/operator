package main

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/plat5dev/operator/internal/gateway"
	"github.com/plat5dev/operator/internal/routes"
)

// config is the gateway's environment. Contract: docs/reference/env.md.
type config struct {
	Addr            string
	InternalPort    string
	ConfigFile      string
	ControlPlaneURL string
	AuthorizerMode  string
	AuthorizerURL   string
	// IdentityURL is Plat5 identity's public base URL. Falls back to services.identity.url.
	IdentityURL     string
	Issuer          string
	JWKSURI         string
	Audiences       []string
	IDClaim         string
	EmailClaim      string
	AllowedOrigins  []string
	TrustedProxies  []netip.Prefix
	UpstreamTimeout time.Duration
	// AuditURL is operator-audit's internal base URL. Empty when audit is off.
	AuditURL string
	// Signer signs outgoing upstream requests when UPSTREAM_SIGNING_KEY is set.
	Signer *gateway.Signer
}

func (c config) overrides() routes.Overrides {
	return routes.Overrides{
		ControlPlaneURL: c.ControlPlaneURL,
		AuthorizerMode:  c.AuthorizerMode,
		AuthorizerURL:   c.AuthorizerURL,
	}
}

func loadConfig(getenv func(string) string) (config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	c := config{
		Addr:            get("ADDR", ":5004"),
		InternalPort:    get("INTERNAL_PORT", "8004"),
		ConfigFile:      get("CONFIG_FILE", "operator.yml"),
		ControlPlaneURL: strings.TrimRight(get("CONTROL_PLANE_URL", ""), "/"),
		AuthorizerMode:  get("AUTHORIZER_MODE", ""),
		AuthorizerURL:   strings.TrimRight(get("AUTHORIZER_URL", ""), "/"),
		IdentityURL:     strings.TrimRight(get("IDENTITY_URL", ""), "/"),
		Issuer:          get("AUTH_ISSUER", ""),
		JWKSURI:         get("AUTH_JWKS_URI", ""),
		Audiences:       list(getenv("AUTH_ALLOWED_AUDIENCES")),
		IDClaim:         get("AUTH_OPERATOR_ID_CLAIM", "sub"),
		EmailClaim:      get("AUTH_OPERATOR_EMAIL_CLAIM", "email"),
	}
	var errs []error
	if c.Issuer == "" {
		errs = append(errs, errors.New("AUTH_ISSUER is required"))
	}
	if u, err := url.Parse(c.JWKSURI); c.JWKSURI == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, errors.New("AUTH_JWKS_URI must be an http or https URL"))
	}
	if len(c.Audiences) == 0 {
		errs = append(errs, errors.New("AUTH_ALLOWED_AUDIENCES is required"))
	}
	if p, err := strconv.Atoi(c.InternalPort); err != nil || p < 1 || p > 65535 {
		errs = append(errs, errors.New("INTERNAL_PORT must be a port number"))
	}
	timeout, err := time.ParseDuration(get("UPSTREAM_TIMEOUT", "30s"))
	if err != nil || timeout <= 0 {
		errs = append(errs, errors.New("UPSTREAM_TIMEOUT must be a positive duration such as 30s"))
	}
	c.UpstreamTimeout = timeout
	for _, o := range list(getenv("ALLOWED_ORIGINS")) {
		origin, err := parseOrigin(o)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.AllowedOrigins = append(c.AllowedOrigins, origin)
	}
	trusted, err := gateway.ParseTrustedProxies(getenv("TRUSTED_PROXIES"))
	if err != nil {
		errs = append(errs, err)
	} else {
		c.TrustedProxies = trusted
	}
	if c.IdentityURL != "" {
		if u, err := url.Parse(c.IdentityURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			errs = append(errs, errors.New("IDENTITY_URL must be an http or https URL"))
		}
	}
	switch get("AUDIT_ENABLED", "true") {
	case "true":
		c.AuditURL = strings.TrimRight(get("AUDIT_URL", ""), "/")
		if u, err := url.Parse(c.AuditURL); c.AuditURL == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			errs = append(errs, errors.New("AUDIT_URL must be an http or https URL unless AUDIT_ENABLED=false"))
		}
	case "false":
	default:
		errs = append(errs, errors.New("AUDIT_ENABLED must be true or false"))
	}
	signingKey := get("UPSTREAM_SIGNING_KEY", "")
	signingKID := get("UPSTREAM_SIGNING_KEY_ID", "")
	switch {
	case signingKey != "":
		s, err := gateway.NewSignerFromSeed(signingKey, signingKID)
		if err != nil {
			errs = append(errs, err)
		} else {
			c.Signer = s
		}
	case signingKID != "":
		errs = append(errs, errors.New("UPSTREAM_SIGNING_KEY_ID cannot be set without UPSTREAM_SIGNING_KEY"))
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
