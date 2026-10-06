package main

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func base() map[string]string {
	return map[string]string{
		"AUTH_ISSUER":    "http://localhost:5556/dex",
		"AUTH_JWKS_URI":  "http://dex:5556/dex/keys",
		"AUTH_AUDIENCES": "operator-cli, operator-console",
	}
}

func TestConfigDefaults(t *testing.T) {
	c, err := loadConfig(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":5004" || c.InternalPort != "8004" || c.RoutesFile != "routes.yml" || c.IDClaim != "sub" ||
		c.UpstreamTimeout != 30*time.Second || len(c.AllowedOrigins) != 0 {
		t.Fatalf("%+v", c)
	}
	if strings.Join(c.Audiences, "|") != "operator-cli|operator-console" {
		t.Fatalf("audiences = %v", c.Audiences)
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
		"no issuer":       {"AUTH_ISSUER": ""},
		"no jwks":         {"AUTH_JWKS_URI": ""},
		"bad jwks":        {"AUTH_JWKS_URI": "dex/keys"},
		"no audience":     {"AUTH_AUDIENCES": " , "},
		"wildcard origin": {"ALLOWED_ORIGINS": "*"},
		"origin path":     {"ALLOWED_ORIGINS": "https://a.test/app"},
		"bad port":        {"INTERNAL_PORT": "http"},
		"bad timeout":     {"UPSTREAM_TIMEOUT_MS": "0"},
		"authz set":       {"AUTHZ_URL": "http://pdp:8080"},
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
