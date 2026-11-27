// Command operator is the headless operator gateway. Contract: docs/.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/authorizer"
	"github.com/plat5dev/operator/internal/gateway"
	"github.com/plat5dev/operator/internal/httpx"
	"github.com/plat5dev/operator/internal/identity"
	"github.com/plat5dev/operator/internal/metrics"
	"github.com/plat5dev/operator/internal/routes"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

func buildAuthorizer(cfg routes.AuthorizerConfig, reg *metrics.Registry, getenv func(string) string) (gateway.Authorizer, error) {
	if cfg.Mode == routes.AuthorizerModeDisabled {
		return nil, nil
	}
	var token string
	if cfg.TokenEnv != "" {
		token = strings.TrimSpace(getenv(cfg.TokenEnv))
		if token == "" {
			return nil, fmt.Errorf("authorizer.token_env: environment variable %s is unset or empty", cfg.TokenEnv)
		}
	}
	return authorizer.New(authorizer.Config{
		URL:     cfg.URL,
		Timeout: cfg.Timeout,
		Token:   token,
		Metrics: reg,
	}), nil
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	fileCfg, err := routes.Load(cfg.ConfigFile, cfg.overrides())
	if err != nil {
		return err
	}

	reg := metrics.NewRegistry()
	authzClient, err := buildAuthorizer(fileCfg.Authorizer, reg, os.Getenv)
	if err != nil {
		return err
	}

	identityURL := cfg.IdentityURL
	if identityURL == "" {
		identityURL = fileCfg.IdentityURL()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	verifier := auth.New(auth.Config{
		Issuer:       cfg.Issuer,
		JWKSURI:      cfg.JWKSURI,
		Audiences:    cfg.Audiences,
		IDClaim:      cfg.IDClaim,
		EmailClaim:   cfg.EmailClaim,
		PolicyClaims: fileCfg.PolicyClaims,
	}, log)
	go verifier.Run(ctx)

	registry := routes.NewRegistry(fileCfg)
	if fileCfg.ControlPlaneURL != "" {
		poller := routes.NewControlPlanePoller(fileCfg.ControlPlaneURL, nil, log, registry.SetControlPlane)
		go poller.Run(ctx)
	}

	gwCfg := gateway.Config{
		Registry:        registry,
		Auth:            verifier,
		Authorizer:      authzClient,
		AllowedOrigins:  cfg.AllowedOrigins,
		TrustedProxies:  cfg.TrustedProxies,
		UpstreamTimeout: cfg.UpstreamTimeout,
		Signer:          cfg.Signer,
		Metrics:         reg,
		Log:             log,
	}
	var auditWriter *audit.Writer
	if identityURL != "" {
		gwCfg.Identity = identity.New(identityURL, nil)
	}
	if cfg.AuditURL != "" {
		auditWriter = audit.New(audit.Config{URL: cfg.AuditURL, Log: log, Metrics: reg})
		gwCfg.Audit = auditWriter
	}
	gw := gateway.New(gwCfg)

	var reloadMu sync.Mutex
	reload := func() (int, error) {
		reloadMu.Lock()
		defer reloadMu.Unlock()
		next, err := routes.Load(cfg.ConfigFile, cfg.overrides())
		if err != nil {
			return 0, err
		}
		nextAuthz, err := buildAuthorizer(next.Authorizer, reg, os.Getenv)
		if err != nil {
			return 0, err
		}
		registry.SetConfig(next)
		verifier.SetPolicyClaims(next.PolicyClaims)
		gw.SetAuthorizer(nextAuthz)
		return registry.ServiceCount(), nil
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				n, err := reload()
				if err != nil {
					log.Error("config reload failed; keeping previous config", "file", cfg.ConfigFile, "err", err)
				} else {
					log.Info("config reloaded", "file", cfg.ConfigFile, "services", n)
				}
			}
		}
	}()

	api := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gw,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	internal := &http.Server{
		Addr:              net.JoinHostPort("", cfg.InternalPort),
		Handler:           internalRoutes(verifier, reg, reload, log),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Info("starting", "addr", cfg.Addr, "internal_port", cfg.InternalPort, "services", registry.ServiceCount(),
		"issuer", cfg.Issuer, "identity", identityURL, "control_plane", fileCfg.ControlPlaneURL)
	if authzClient != nil {
		log.Info("authorizer: authzen", "url", fileCfg.Authorizer.URL)
	} else {
		log.Warn("authorizer: disabled")
	}
	if cfg.Signer != nil {
		log.Info("upstream request signing: on", "kid", cfg.Signer.KID())
	}
	if auditWriter != nil {
		log.Info("audit: on", "url", cfg.AuditURL)
	} else {
		log.Warn("audit: off")
	}

	errc := make(chan error, 2)
	for _, srv := range []*http.Server{api, internal} {
		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}

	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	if auditWriter != nil {
		drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := auditWriter.Close(drain); err != nil {
			log.Error("audit outcomes lost at shutdown", "err", err)
		}
	}
	internalShutdown, internalCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer internalCancel()
	_ = internal.Shutdown(internalShutdown)
	return err
}

func internalRoutes(v *auth.Verifier, reg *metrics.Registry, reload func() (int, error), log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", reg)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteStatus(w, http.StatusOK, "healthy")
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		if !v.Ready() {
			httpx.WriteStatus(w, http.StatusServiceUnavailable, "unhealthy")
			return
		}
		httpx.WriteStatus(w, http.StatusOK, "healthy")
	})
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, _ *http.Request) {
		n, err := reload()
		if err != nil {
			log.Error("config reload failed; keeping previous config", "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		log.Info("config reloaded", "services", n)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "reloaded", "services": n})
	})
	return mux
}
