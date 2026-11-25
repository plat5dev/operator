// Command operator is the headless operator gateway. Contract: docs/.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
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

func run(log *slog.Logger) error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	table, err := routes.Load(cfg.RoutesFile)
	if err != nil {
		return err
	}
	if table.HasNodes() && cfg.IdentityURL == "" {
		return errors.New("IDENTITY_URL is required: routes in " + cfg.RoutesFile + " name nodes")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	verifier := auth.New(auth.Config{
		Issuer:     cfg.Issuer,
		JWKSURI:    cfg.JWKSURI,
		Audiences:  cfg.Audiences,
		IDClaim:    cfg.IDClaim,
		EmailClaim: cfg.EmailClaim,
	}, log)
	go verifier.Run(ctx)

	reg := metrics.NewRegistry()
	gwCfg := gateway.Config{
		Routes:          table,
		Auth:            verifier,
		AllowedOrigins:  cfg.AllowedOrigins,
		UpstreamTimeout: cfg.UpstreamTimeout,
		Signer:          cfg.Signer,
		Metrics:         reg,
		Log:             log,
	}
	var auditWriter *audit.Writer
	if cfg.IdentityURL != "" {
		gwCfg.Identity = identity.New(cfg.IdentityURL, nil)
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
		next, err := routes.Load(cfg.RoutesFile)
		if err != nil {
			return 0, err
		}
		if err := gw.Reload(next); err != nil {
			return 0, err
		}
		return next.Len(), nil
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
					log.Error("routes reload failed; keeping previous routes", "file", cfg.RoutesFile, "err", err)
				} else {
					log.Info("routes reloaded", "file", cfg.RoutesFile, "routes", n)
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

	log.Info("starting", "addr", cfg.Addr, "internal_port", cfg.InternalPort, "routes", table.Len(),
		"issuer", cfg.Issuer, "identity", cfg.IdentityURL)
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
	// No more requests, so no more outcomes. Send what is queued (docs/concepts/audit.md#delivery).
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

// internalRoutes serves /health/live, /health/ready, /metrics, and POST /reload.
// Ready means routes loaded and JWKS fetched once. It does not probe operator-audit.
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
			log.Error("routes reload failed; keeping previous routes", "err", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		log.Info("routes reloaded", "routes", n)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "reloaded", "routes": n})
	})
	return mux
}
