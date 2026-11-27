// Package db is operator-audit's Postgres: connecting, migrating as the owner, and
// checking that serve runs as the writer. Contract: docs/internals/audit-storage.md.
package db

import (
	"context"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema holds the operator audit log. Other operator-plane concerns get their own.
const Schema = "audit"

// migrateLockSpace namespaces the session advisory lock that serializes concurrent migrations.
const migrateLockSpace int32 = 0x6f706d67 // "opmg"

//go:embed migrations/*.sql
var migrationFS embed.FS

var roleRe = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// Connect opens a pool whose connections use the audit schema.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{Schema}.Sanitize())
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	ping, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(ping); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

type migration struct {
	version string
	sql     string
}

func migrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: strings.TrimSuffix(e.Name(), ".sql"), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// LatestVersion is the newest migration this binary carries.
func LatestVersion() string {
	ms, err := migrations()
	if err != nil || len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1].version
}

// Migrate applies migrations as the schema owner, then grants writerRole exactly the
// writer's rights (docs/internals/audit-storage.md#roles). Replicas serialize on an advisory lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool, writerRole string) error {
	if !roleRe.MatchString(writerRole) {
		return fmt.Errorf("WRITER_ROLE %q is not a plain lowercase role name", writerRole)
	}
	var me string
	var writerExists bool
	if err := pool.QueryRow(ctx, `SELECT current_user, EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`,
		writerRole).Scan(&me, &writerExists); err != nil {
		return err
	}
	if me == writerRole {
		return errors.New("WRITER_ROLE must not be the role migrate runs as")
	}
	if !writerExists {
		return fmt.Errorf("role %q does not exist; the deployment creates the writer role", writerRole)
	}

	ms, err := migrations()
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1, hashtext('audit.migrate'))`, migrateLockSpace); err != nil {
		return err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1, hashtext('audit.migrate'))`, migrateLockSpace); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()

	// A deployment may create the schema for the owner, who then needs no CREATE on the
	// database. IF NOT EXISTS would still ask for it, so look first.
	var schemaExists bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`,
		Schema).Scan(&schemaExists); err != nil {
		return err
	}
	if !schemaExists {
		if _, err := conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{Schema}.Sanitize()); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if err := refuseUnknown(ctx, conn, ms); err != nil {
		return err
	}
	for _, m := range ms {
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`,
			m.version).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.sql); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version)
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.version, err)
		}
	}

	writer := pgx.Identifier{writerRole}.Sanitize()
	grants := []string{
		"REVOKE ALL ON SCHEMA audit FROM " + writer,
		"REVOKE ALL ON ALL TABLES IN SCHEMA audit FROM " + writer,
		"REVOKE ALL ON ALL SEQUENCES IN SCHEMA audit FROM " + writer,
		"REVOKE ALL ON ALL FUNCTIONS IN SCHEMA audit FROM " + writer,
		"GRANT USAGE ON SCHEMA audit TO " + writer,
		"GRANT SELECT ON audit.schema_migrations TO " + writer,
		"GRANT SELECT, INSERT ON audit.audit_events TO " + writer,
		"GRANT UPDATE (outcome, status, response_bytes, details, created_id, finalized_seq) ON audit.audit_events TO " + writer,
		"GRANT USAGE, SELECT ON SEQUENCE audit.export_seq TO " + writer,
		"GRANT EXECUTE ON FUNCTION audit.ensure_partition(timestamptz) TO " + writer,
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		for _, g := range grants {
			if _, err := tx.Exec(ctx, g); err != nil {
				return fmt.Errorf("%s: %w", g, err)
			}
		}
		return nil
	})
}

// refuseUnknown stops migrate on a database that recorded migrations this build doesn't know.
func refuseUnknown(ctx context.Context, conn *pgxpool.Conn, ms []migration) error {
	known := make(map[string]bool, len(ms))
	for _, m := range ms {
		known[m.version] = true
	}
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	applied, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for _, v := range applied {
		if !known[v] {
			return fmt.Errorf("schema has migration %q that this build doesn't know; drop the schema or use a fresh database", v)
		}
	}
	return nil
}

// ErrNotWriter means serve's role could change or delete history, or cannot write.
var ErrNotWriter = errors.New("not the writer role")

// CheckWriter refuses a role that owns the schema or can delete or truncate events,
// and a schema this binary's migrations have not reached.
func CheckWriter(ctx context.Context, pool *pgxpool.Pool) error {
	var table *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('audit.audit_events')::text`).Scan(&table); err != nil {
		return err
	}
	if table == nil {
		return errors.New("audit.audit_events does not exist; run operator-audit migrate first")
	}
	var applied bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audit.schema_migrations WHERE version = $1)`,
		LatestVersion()).Scan(&applied); err != nil {
		return fmt.Errorf("%w: cannot read audit.schema_migrations; run operator-audit migrate with WRITER_ROLE set to this role: %v", ErrNotWriter, err)
	}
	if !applied {
		return fmt.Errorf("migration %s is not applied; run operator-audit migrate first", LatestVersion())
	}
	var me string
	var owns, canDelete, canTruncate, canInsert, canSeq, canPartition bool
	if err := pool.QueryRow(ctx, `
		SELECT current_user,
			pg_has_role(current_user, n.nspowner, 'MEMBER'),
			has_table_privilege('audit.audit_events', 'DELETE'),
			has_table_privilege('audit.audit_events', 'TRUNCATE'),
			has_table_privilege('audit.audit_events', 'INSERT'),
			has_sequence_privilege('audit.export_seq', 'USAGE'),
			has_function_privilege('audit.ensure_partition(timestamptz)', 'EXECUTE')
		FROM pg_namespace n WHERE n.nspname = 'audit'
	`).Scan(&me, &owns, &canDelete, &canTruncate, &canInsert, &canSeq, &canPartition); err != nil {
		return err
	}
	if owns || canDelete || canTruncate {
		return fmt.Errorf("%w: role %q can delete audit events; run serve as the writer role (docs/internals/audit-storage.md#roles)", ErrNotWriter, me)
	}
	if !canInsert || !canSeq || !canPartition {
		return fmt.Errorf("%w: role %q cannot write audit events; run operator-audit migrate with WRITER_ROLE=%s", ErrNotWriter, me, me)
	}
	return nil
}

// Unavailable reports whether err means Postgres couldn't be reached or didn't answer in time:
// a failed connect, a closed connection, a network error, cancellation or timeout, or Postgres
// refusing connections or out of resources (SQLSTATE classes 08, 53, 57).
func Unavailable(err error) bool {
	if err == nil {
		return false
	}
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &connectErr), errors.As(err, &netErr):
		return true
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled), pgconn.Timeout(err):
		return true
	case errors.Is(err, driver.ErrBadConn), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		return true
	case errors.As(err, &pgErr) && len(pgErr.Code) >= 2:
		switch pgErr.Code[:2] {
		case "08", "53", "57":
			return true
		}
	}
	return false
}
