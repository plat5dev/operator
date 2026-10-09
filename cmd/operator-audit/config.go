package main

import (
	"errors"
	"strconv"
	"strings"
)

// config is operator-audit's environment. Contract: docs/audit.md#runtime.
type config struct {
	Addr          string
	InternalPort  string
	DatabaseURL   string
	InternalToken string
	WriterRole    string
}

// loadConfig reads what cmd needs: serve needs the token, migrate the writer role.
func loadConfig(cmd string, getenv func(string) string) (config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	c := config{
		Addr:          get("ADDR", ":5005"),
		InternalPort:  get("INTERNAL_PORT", "8005"),
		DatabaseURL:   get("DATABASE_URL", ""),
		InternalToken: get("INTERNAL_TOKEN", ""),
		WriterRole:    get("WRITER_ROLE", ""),
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	switch cmd {
	case "serve":
		if c.InternalToken == "" {
			errs = append(errs, errors.New("INTERNAL_TOKEN is required: only the gateway writes the staff log"))
		}
		if p, err := strconv.Atoi(c.InternalPort); err != nil || p < 1 || p > 65535 {
			errs = append(errs, errors.New("INTERNAL_PORT must be a port number"))
		}
	case "migrate":
		if c.WriterRole == "" {
			errs = append(errs, errors.New("WRITER_ROLE is required: migrate grants it the writer's rights"))
		}
	default:
		errs = append(errs, errors.New("usage: operator-audit migrate|serve"))
	}
	return c, errors.Join(errs...)
}
