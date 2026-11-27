package db_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/plat5dev/operator/internal/db"
	"github.com/plat5dev/operator/internal/db/dbtest"
)

func connect(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	p, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestMigrateAndRoles(t *testing.T) {
	d := dbtest.New(t)
	ctx := context.Background()
	owner, writer := connect(t, d.OwnerURL), connect(t, d.WriterURL)

	if err := db.CheckWriter(ctx, writer); err == nil || !strings.Contains(err.Error(), "migrate first") {
		t.Fatalf("before migrate: %v", err)
	}

	// As in compose: the schema is made for the owner, who has no CREATE on the database.
	if _, err := owner.Exec(ctx, `CREATE SCHEMA audit`); err != nil {
		t.Fatal(err)
	}
	var dbName string
	_ = owner.QueryRow(ctx, "SELECT current_database()").Scan(&dbName)
	if _, err := owner.Exec(ctx, `REVOKE CREATE ON DATABASE `+dbName+` FROM PUBLIC, current_user`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, owner, "audit_owner_nope"); err == nil {
		t.Fatal("a writer role that does not exist must fail")
	}
	if err := db.Migrate(ctx, owner, "Bad-Role"); err == nil {
		t.Fatal("a role name that needs quoting must fail")
	}
	for range 2 {
		if err := db.Migrate(ctx, owner, d.WriterRole); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CheckWriter(ctx, writer); err != nil {
		t.Fatalf("writer: %v", err)
	}
	if err := db.CheckWriter(ctx, owner); !errors.Is(err, db.ErrNotWriter) {
		t.Fatalf("owner must be refused: %v", err)
	}

	var me string
	_ = owner.QueryRow(ctx, "SELECT current_user").Scan(&me)
	if err := db.Migrate(ctx, owner, me); err == nil {
		t.Fatal("WRITER_ROLE equal to the migrating role must fail")
	}

	// One event to try to rewrite.
	at := time.Now()
	if _, err := writer.Exec(ctx, `SELECT ensure_partition($1)`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(ctx, `INSERT INTO audit_events (id, occurred_at, request_id, actor_issuer,
		actor_operator_id, service, method, route, ip) VALUES ('01ja2z6q3y8d5v2k9n4r7t1w0x', $1, 'r1', 'i', 'o',
		'identity', 'GET', '/nodes', '10.0.0.1')`, at); err != nil {
		t.Fatal(err)
	}

	// The writer's grants stop it.
	for _, stmt := range []string{
		`DELETE FROM audit_events`,
		`TRUNCATE audit_events`,
		`UPDATE audit_events SET actor_operator_id = 'someone_else'`,
		`ALTER TABLE audit_events DISABLE TRIGGER audit_events_guard`,
		`DROP TABLE audit_events`,
		`CREATE TABLE audit.other (id int)`,
		`DELETE FROM schema_migrations`,
	} {
		if _, err := writer.Exec(ctx, stmt); err == nil || !strings.Contains(err.Error(), "permission denied") &&
			!strings.Contains(err.Error(), "must be owner") {
			t.Errorf("writer %s: %v", stmt, err)
		}
	}
	// The trigger stops anyone who does not drop it, including the owner.
	if _, err := owner.Exec(ctx, `DELETE FROM audit_events`); err == nil || !strings.Contains(err.Error(), "not deleted") {
		t.Errorf("owner delete: %v", err)
	}
	if _, err := writer.Exec(ctx, `UPDATE audit_events SET outcome = 'responded', status = 200, response_bytes = 1, finalized_seq = nextval('export_seq')`); err != nil {
		t.Fatalf("pending -> final: %v", err)
	}
	if _, err := writer.Exec(ctx, `UPDATE audit_events SET outcome = 'rejected'`); err == nil || !strings.Contains(err.Error(), "already has an outcome") {
		t.Errorf("final -> other: %v", err)
	}
	// Prune must be refused for the writer role, and work for the owner role.
	if _, err := db.Prune(ctx, writer, time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC), true); err == nil {
		t.Fatal("writer must not be allowed to prune")
	}
	if _, err := writer.Exec(ctx, `SELECT ensure_partition($1)`, time.Date(2025, 10, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	dry, err := db.Prune(ctx, owner, time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC), true)
	if err != nil || len(dry) != 1 || dry[0] != "audit_events_y2025m10" {
		t.Fatalf("dry prune: %v %v", dry, err)
	}
	dropped, err := db.Prune(ctx, owner, time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC), false)
	if err != nil || len(dropped) != 1 || dropped[0] != "audit_events_y2025m10" {
		t.Fatalf("prune: %v %v", dropped, err)
	}
}

func TestUnavailable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic", errors.New("boom"), false},
		{"pg unique violation", &pgconn.PgError{Code: "23505"}, false},
		{"pg invalid syntax", &pgconn.PgError{Code: "22P02"}, false},
		{"deadline exceeded", fmt.Errorf("wrap: %w", context.DeadlineExceeded), true},
		{"canceled", context.Canceled, true},
		{"bad conn", fmt.Errorf("wrap: %w", driver.ErrBadConn), true},
		{"eof", io.EOF, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"net closed", net.ErrClosed, true},
		{"net error", &net.DNSError{IsTimeout: true}, true},
		{"connect error", &pgconn.ConnectError{}, true},
		{"sqlstate 08", &pgconn.PgError{Code: "08006"}, true},
		{"sqlstate 53", &pgconn.PgError{Code: "53300"}, true},
		{"sqlstate 57", &pgconn.PgError{Code: "57P03"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := db.Unavailable(c.err); got != c.want {
				t.Fatalf("Unavailable(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
