// Command operator-audit stores the operator audit log and serves it back through the gateway.
// Contract: docs/reference/api/operator-audit.md.
//
//	operator-audit migrate   as the schema owner: migrations, then the writer's grants
//	operator-audit serve     as the writer: the internal write/export API and the read route
//	operator-audit prune     as the schema owner: drop monthly partitions older than cutoff
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/plat5dev/operator/internal/apierr"
	"github.com/plat5dev/operator/internal/db"
	"github.com/plat5dev/operator/internal/events"
	"github.com/plat5dev/operator/internal/httpx"
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
	var args []string
	if len(os.Args) > 1 {
		cmd = os.Args[1]
		args = os.Args[2:]
	}
	if err := run(cmd, args, log); err != nil {
		log.Error("exit", "cmd", cmd, "err", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string, log *slog.Logger) error {
	cfg, err := loadConfig(cmd, os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "migrate":
		if len(args) > 0 {
			return fmt.Errorf("unexpected arguments: %v", args)
		}
		pool, err := db.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		if err := db.Migrate(ctx, pool, cfg.WriterRole); err != nil {
			return err
		}
		log.Info("migrated", "version", db.LatestVersion(), "writer_role", cfg.WriterRole)
		return nil

	case "prune":
		fs := flag.NewFlagSet("prune", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		before := fs.String("before", "", "drop monthly partitions strictly older than YYYY-MM (UTC)")
		olderThan := fs.String("older-than", "", "drop monthly partitions strictly older than <N>m (e.g. 12m)")
		dryRun := fs.Bool("dry-run", false, "list partitions that would be dropped without dropping them")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() > 0 {
			return fmt.Errorf("unexpected arguments: %v", fs.Args())
		}
		cutoff, err := db.ParsePruneCutoff(*before, *olderThan, time.Now())
		if err != nil {
			return err
		}
		pool, err := db.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		dropped, err := db.Prune(ctx, pool, cutoff, *dryRun)
		if err != nil {
			return err
		}
		log.Info("pruned", "cutoff", cutoff.Format("2006-01"), "dry_run", *dryRun, "count", len(dropped), "partitions", dropped)
		return nil

	default:
		if len(args) > 0 {
			return fmt.Errorf("unexpected arguments: %v", args)
		}
		pool, err := db.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		return serve(ctx, stop, cfg, pool, log)
	}
}

func serve(ctx context.Context, stop context.CancelFunc, cfg config, pool *pgxpool.Pool, log *slog.Logger) error {
	// The role that serves can change no history (docs/internals/audit-storage.md#roles).
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
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		store.MaintainPartitions(ctx, partitionInterval, func(err error) {
			if ctx.Err() == nil {
				log.Error("audit partition maintenance failed", "err", err)
			}
		})
	}()
	go func() {
		defer wg.Done()
		countStale(ctx, store, stale, log)
	}()

	handler := events.NewHandler(store, log)
	public := http.NewServeMux()
	handler.MountPublic(public)
	public.HandleFunc("/", apierr.NotFound)

	internal := http.NewServeMux()
	internal.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteStatus(w, http.StatusOK, "healthy")
	})
	internal.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ping, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := store.Ping(ping); err != nil {
			httpx.WriteStatus(w, http.StatusServiceUnavailable, "unhealthy")
			return
		}
		httpx.WriteStatus(w, http.StatusOK, "healthy")
	})
	internal.Handle("GET /metrics", reg)
	handler.MountInternal(internal)
	internal.HandleFunc("/", apierr.NotFound)

	servers := []*http.Server{
		{Addr: cfg.Addr, Handler: withRequest(public, log), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second},
		{Addr: net.JoinHostPort("", cfg.InternalPort), Handler: withRequest(internal, log), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second},
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
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(shutdown)
	}
	wg.Wait()
	return err
}

// countStale sets the stale pending gauge now and every minute.
func countStale(ctx context.Context, store *events.Store, g *metrics.Gauge, log *slog.Logger) {
	t := time.NewTicker(staleInterval)
	defer t.Stop()
	for {
		n, err := store.StalePending(ctx, time.Now())
		if err != nil {
			if ctx.Err() == nil {
				log.Error("counting stale pending audit events failed", "err", err)
			}
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

// withRequest carries the gateway's request id into error envelopes and logs one line
// per request, health and metrics aside.
func withRequest(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-ID")
		if !httpx.ValidRequestID(id) {
			id = httpx.NewRequestID()
		}
		r = r.WithContext(apierr.WithRequestID(r.Context(), id))
		rec := &httpx.Recorder{ResponseWriter: w}
		defer func() {
			rv := recover()
			if rv != nil && rv != http.ErrAbortHandler {
				log.Error("panic", "request_id", id, "panic", rv)
				if rec.Status == 0 {
					apierr.Internal(rec, r)
				}
			}
			if r.Pattern != "GET /health/live" && r.Pattern != "GET /health/ready" && r.Pattern != "GET /metrics" {
				route := r.Pattern
				if route == "" || route == "/" {
					route = r.Method + " " + r.URL.EscapedPath()
				}
				log.Info("request", "request_id", id, "route", route, "status", rec.Status,
					"duration_ms", time.Since(start).Milliseconds())
			}
			if rv == http.ErrAbortHandler {
				panic(rv)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}
