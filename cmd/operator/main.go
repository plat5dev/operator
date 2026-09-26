package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/plat5dev/operator/accounts"
	"github.com/plat5dev/operator/console"
	"github.com/plat5dev/operator/gateway"
	"github.com/plat5dev/operator/internal/apierr"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := env("ADDR", ":5004")
	dbPath := env("DB_PATH", "operator.db")
	routesFile := env("ROUTES_FILE", "routes.yml")

	store, err := accounts.Open(dbPath)
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	email := os.Getenv("OPERATOR_BOOTSTRAP_EMAIL")
	password := os.Getenv("OPERATOR_BOOTSTRAP_PASSWORD")
	if email != "" || password != "" {
		created, err := store.Bootstrap(email, password)
		if err != nil {
			log.Error("bootstrap", "err", err)
			os.Exit(1)
		}
		log.Info("bootstrap", "email", email, "created", created)
	}

	routes, err := gateway.Load(routesFile)
	if err != nil {
		log.Error("routes", "err", err)
		os.Exit(1)
	}
	assets := env("CONSOLE_ASSETS", "console/dist")
	if _, err := os.Stat(filepath.Join(assets, "index.html")); err != nil {
		log.Error("console", "dir", assets, "err", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           guard(apierr.Middleware(newHandler(store, routes, log, assets)), log),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	log.Info("listen", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}

func newHandler(store *accounts.Store, routes []gateway.Route, log *slog.Logger, assets string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /login", accounts.LoginHandler(store, log))
	mux.Handle("POST /logout", accounts.LogoutHandler(store, log))
	mux.Handle("GET /session", accounts.SessionHandler(store, log))
	mux.Handle("POST /account/password", accounts.PasswordHandler(store, log))
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	proxy := gateway.Proxy(store, routes, log)
	mux.Handle("/users/", proxy)
	mux.Handle("/organizations", proxy)
	mux.Handle("/organizations/", proxy)
	mux.Handle("/members/", proxy)
	mux.Handle("/", console.Handler(assets))
	return mux
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func guard(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error("panic", "request_id", apierr.From(r.Context()), "err", rec)
				apierr.Internal(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
