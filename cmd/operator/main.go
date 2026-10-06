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

	"github.com/plat5dev/operator/internal/auth"
	"github.com/plat5dev/operator/internal/gateway"
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

	api := &http.Server{
		Addr: cfg.Addr,
		Handler: gateway.New(gateway.Config{
			Routes:          table,
			Auth:            verifier,
			AllowedOrigins:  cfg.AllowedOrigins,
			UpstreamTimeout: cfg.UpstreamTimeout,
			Log:             log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	internal := &http.Server{
		Addr:              net.JoinHostPort("", cfg.InternalPort),
		Handler:           health(verifier),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info("starting", "addr", cfg.Addr, "internal_port", cfg.InternalPort, "routes", table.Len(), "issuer", cfg.Issuer)
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
	_ = internal.Shutdown(shutdown)
	return err
}

// health serves /health/live and /health/ready. Ready means routes loaded and JWKS fetched once.
func health(v *auth.Verifier) http.Handler {
	mux := http.NewServeMux()
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
