// Command operator-audit stores the staff audit log and serves it back through the gateway.
// Contract: docs/audit.md.
//
//	operator-audit migrate   as the schema owner: migrations, then the writer's grants
//	operator-audit serve     as the writer: the internal write API and the read route
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/db"
	"github.com/plat5dev/operator/internal/events"
	"github.com/plat5dev/operator/internal/metrics"
)

const (
	// Partitions run months ahead, so this only has to happen well inside a month.
	partitionInterval = time.Hour
	staleInterval     = time.Minute
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if err := run(cmd, log); err != nil {
		log.Error("exit", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

func run(cmd string, log *slog.Logger) error {
	cfg, err := loadConfig(cmd, os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cmd == "migrate" {
		if err := db.Migrate(ctx, pool, cfg.WriterRole); err != nil {
			return err
		}
		log.Info("migrated", "version", db.LatestVersion(), "writer_role", cfg.WriterRole)
		return nil
	}
	return serve(ctx, cfg, pool, log)
}

func serve(ctx context.Context, cfg config, pool *pgxpool.Pool, log *slog.Logger) error {
	// The role that serves can change no history (docs/audit.md#roles).
	if err := db.CheckWriter(ctx, pool); err != nil {
		return err
	}
	store := events.NewStore(pool)
	if err := store.EnsureAround(ctx, time.Now()); err != nil {
		return err
	}

	reg := metrics.NewRegistry()
	stale := reg.Gauge("operator_audit_stale_pending",
		"Events from the last 24 hours still pending after 10 minutes. Checked every minute.")
	go store.MaintainPartitions(ctx, partitionInterval, func(err error) {
		log.Error("audit partition maintenance failed", "err", err)
	})
	go countStale(ctx, store, stale, log)

	handler := events.NewHandler(store, log)
	public := http.NewServeMux()
	handler.MountPublic(public)
	public.HandleFunc("/", apierr.NotFound)

	internal := http.NewServeMux()
	internal.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		status(w, http.StatusOK, "healthy")
	})
	internal.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ping, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := store.Ping(ping); err != nil {
			status(w, http.StatusServiceUnavailable, "unhealthy")
			return
		}
		status(w, http.StatusOK, "healthy")
	})
	internal.Handle("GET /metrics", reg)
	handler.MountInternal(internal, cfg.InternalToken)
	internal.HandleFunc("/", apierr.NotFound)

	servers := []*http.Server{
		{Addr: cfg.Addr, Handler: withRequest(public, log), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second},
		{Addr: net.JoinHostPort("", cfg.InternalPort), Handler: withRequest(internal, log), ReadHeaderTimeout: 5 * time.Second},
	}
	log.Info("starting", "addr", cfg.Addr, "internal_port", cfg.InternalPort, "version", db.LatestVersion())

	errc := make(chan error, len(servers))
	for _, srv := range servers {
		go func() {
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(shutdown)
	}
	return err
}

// countStale sets the stale pending gauge now and every minute.
func countStale(ctx context.Context, store *events.Store, g *metrics.Gauge, log *slog.Logger) {
	t := time.NewTicker(staleInterval)
	defer t.Stop()
	for {
		n, err := store.StalePending(ctx, time.Now())
		if err != nil {
			log.Error("counting stale pending audit events failed", "err", err)
		} else {
			g.Set(float64(n))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// withRequest carries the gateway's request id into error envelopes and logs one line
// per request, health and metrics aside.
func withRequest(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-ID")
		if !requestIDRe.MatchString(id) {
			var b [16]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		r = r.WithContext(apierr.WithRequestID(r.Context(), id))
		rec := &recorder{ResponseWriter: w}
		defer func() {
			if rv := recover(); rv != nil {
				log.Error("panic", "request_id", id, "panic", rv)
				if rec.status == 0 {
					apierr.Internal(rec, r)
				}
			}
			if r.Pattern == "GET /health/live" || r.Pattern == "GET /health/ready" || r.Pattern == "GET /metrics" {
				return
			}
			route := r.Pattern
			if route == "" || route == "/" {
				route = r.Method + " " + r.URL.EscapedPath()
			}
			log.Info("request", "request_id", id, "route", route, "status", rec.status,
				"duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func status(w http.ResponseWriter, code int, s string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": s})
}
