package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func base() map[string]string {
	return map[string]string{
		"AUTH_ISSUER":            "http://localhost:5556/dex",
		"AUTH_JWKS_URI":          "http://dex:5556/dex/keys",
		"AUTH_ALLOWED_AUDIENCES": "operator-cli, operator-console",
		"AUDIT_URL":              "http://operator-audit:8005/",
	}
}

func TestConfigDefaults(t *testing.T) {
	c, err := loadConfig(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":5004" || c.InternalPort != "8004" || c.ConfigFile != "operator.yml" || c.IDClaim != "sub" ||
		c.UpstreamTimeout != 30*time.Second || len(c.AllowedOrigins) != 0 || len(c.TrustedProxies) != 0 {
		t.Fatalf("%+v", c)
	}
	if strings.Join(c.Audiences, "|") != "operator-cli|operator-console" {
		t.Fatalf("audiences = %v", c.Audiences)
	}
	if c.AuditURL != "http://operator-audit:8005" {
		t.Fatalf("audit = %q", c.AuditURL)
	}
	if c.IdentityURL != "" {
		t.Fatalf("identity = %q", c.IdentityURL)
	}
	m := base()
	m["IDENTITY_URL"] = "http://identity:3000/"
	m["CONTROL_PLANE_URL"] = "http://control-plane:5003/"
	m["AUTHORIZER_MODE"] = "disabled"
	m["TRUSTED_PROXIES"] = "10.0.0.0/8, 127.0.0.1"
	c, err = loadConfig(env(m))
	if err != nil || c.IdentityURL != "http://identity:3000" || c.ControlPlaneURL != "http://control-plane:5003" ||
		c.AuthorizerMode != "disabled" || len(c.TrustedProxies) != 2 {
		t.Fatalf("overrides = %+v, %v", c, err)
	}
}

func TestConfigAuditOff(t *testing.T) {
	m := base()
	m["AUDIT_ENABLED"] = "false"
	m["AUDIT_URL"] = ""
	c, err := loadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.AuditURL != "" {
		t.Fatalf("audit off kept the URL %q", c.AuditURL)
	}
	m["AUDIT_URL"] = "http://operator-audit:8005"
	if c, _ := loadConfig(env(m)); c.AuditURL != "" {
		t.Fatal("audit off must ignore AUDIT_URL")
	}
}

func TestConfigOrigins(t *testing.T) {
	m := base()
	m["ALLOWED_ORIGINS"] = "https://Console.Example.com, http://localhost:5173/"
	c, err := loadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.AllowedOrigins, "|") != "https://console.example.com|http://localhost:5173" {
		t.Fatalf("origins = %v", c.AllowedOrigins)
	}
}

func TestConfigErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"no issuer":           {"AUTH_ISSUER": ""},
		"no jwks":             {"AUTH_JWKS_URI": ""},
		"bad jwks":            {"AUTH_JWKS_URI": "dex/keys"},
		"no audience":         {"AUTH_ALLOWED_AUDIENCES": " , "},
		"wildcard origin":     {"ALLOWED_ORIGINS": "*"},
		"origin path":         {"ALLOWED_ORIGINS": "https://a.test/app"},
		"bad trusted proxies": {"TRUSTED_PROXIES": "not-an-ip"},
		"bad port":            {"INTERNAL_PORT": "http"},
		"bad timeout":         {"UPSTREAM_TIMEOUT": "0s"},
		"bad identity url":    {"IDENTITY_URL": "identity:3000"},
		"no audit url":        {"AUDIT_URL": ""},
		"bad audit url":       {"AUDIT_URL": "operator-audit:8005"},
		"audit enabled no":    {"AUDIT_ENABLED": "no"},
		"orphan kid":          {"UPSTREAM_SIGNING_KEY_ID": "k-1"},
		"bad signing key":     {"UPSTREAM_SIGNING_KEY": "not-valid"},
		"bad signing kid": {
			"UPSTREAM_SIGNING_KEY":    base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, ed25519.SeedSize)),
			"UPSTREAM_SIGNING_KEY_ID": "bad kid",
		},
	}
	for name, over := range cases {
		t.Run(name, func(t *testing.T) {
			m := base()
			for k, v := range over {
				m[k] = v
			}
			if _, err := loadConfig(env(m)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestConfigSigning(t *testing.T) {
	m := base()
	m["UPSTREAM_SIGNING_KEY"] = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{5}, ed25519.SeedSize))
	c, err := loadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.Signer == nil || len(c.Signer.KID()) != 16 {
		t.Fatalf("unexpected signer: %+v", c.Signer)
	}
	m["UPSTREAM_SIGNING_KEY_ID"] = "op-2026"
	c, err = loadConfig(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if c.Signer == nil || c.Signer.KID() != "op-2026" {
		t.Fatalf("unexpected kid: %+v", c.Signer)
	}
}
