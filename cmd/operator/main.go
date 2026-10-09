// Command operator is the headless staff gateway. Contract: docs/.
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
	"syscall"
	"time"

	"github.com/plat5dev/operator/internal/audit"
	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/gateway"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	verifier := auth.New(auth.Config{
		Issuer:    cfg.Issuer,
		JWKSURI:   cfg.JWKSURI,
		Audiences: cfg.Audiences,
		IDClaim:   cfg.IDClaim,
	}, log)
	go verifier.Run(ctx)

	reg := metrics.NewRegistry()
	gw := gateway.Config{
		Routes:          table,
		Auth:            verifier,
		AllowedOrigins:  cfg.AllowedOrigins,
		UpstreamTimeout: cfg.UpstreamTimeout,
		Metrics:         reg,
		Log:             log,
	}
	var auditWriter *audit.Writer
	if cfg.AuditURL != "" {
		auditWriter = audit.New(audit.Config{URL: cfg.AuditURL, Token: cfg.AuditToken, Log: log, Metrics: reg})
		gw.Audit = auditWriter
	}

	api := &http.Server{
		Addr:              cfg.Addr,
		Handler:           gateway.New(gw),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	internal := &http.Server{
		Addr:              net.JoinHostPort("", cfg.InternalPort),
		Handler:           internalRoutes(verifier, reg),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info("starting", "addr", cfg.Addr, "internal_port", cfg.InternalPort, "routes", table.Len(), "issuer", cfg.Issuer)
	if auditWriter != nil {
		log.Info("audit: on", "url", cfg.AuditURL)
	} else {
		log.Warn("audit: off")
	}
	log.Warn("authz: none")

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
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = api.Shutdown(shutdown)
	// No more requests, so no more outcomes. Send what is queued (docs/audit.md#delivery).
	if auditWriter != nil {
		drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := auditWriter.Close(drain); err != nil {
			log.Error("audit outcomes lost at shutdown", "err", err)
		}
	}
	_ = internal.Shutdown(shutdown)
	return err
}

// internalRoutes serves /health/live, /health/ready, and /metrics. Ready means routes loaded
// and JWKS fetched once. It does not probe operator-audit.
func internalRoutes(v *auth.Verifier, reg *metrics.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", reg)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		status(w, http.StatusOK, "healthy")
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		if !v.Ready() {
			status(w, http.StatusServiceUnavailable, "unhealthy")
			return
		}
		status(w, http.StatusOK, "healthy")
	})
	return mux
}

func status(w http.ResponseWriter, code int, s string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": s})
}
