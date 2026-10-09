// Package dbtest gives a test its own database with an owner and a writer role. It needs
// OPERATOR_AUDIT_TEST_DATABASE_URL, a superuser connection; without it, tests skip.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

const EnvVar = "OPERATOR_AUDIT_TEST_DATABASE_URL"

// DB is one test's database. The roles and the database are dropped when the test ends.
type DB struct {
	OwnerURL   string
	WriterURL  string
	WriterRole string
}

func New(t *testing.T) DB {
	t.Helper()
	admin := os.Getenv(EnvVar)
	if admin == "" {
		t.Skip(EnvVar + " is not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })

	var b [6]byte
	_, _ = rand.Read(b[:])
	suffix := hex.EncodeToString(b[:])
	name, owner, writer := "audit_test_"+suffix, "audit_owner_"+suffix, "audit_writer_"+suffix
	for _, stmt := range []string{
		"CREATE ROLE " + owner + " LOGIN PASSWORD 'owner'",
		"CREATE ROLE " + writer + " LOGIN PASSWORD 'writer'",
		"CREATE DATABASE " + name + " OWNER " + owner,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, stmt := range []string{
			"DROP DATABASE IF EXISTS " + name + " WITH (FORCE)",
			"DROP ROLE IF EXISTS " + writer,
			"DROP ROLE IF EXISTS " + owner,
		} {
			if _, err := conn.Exec(ctx, stmt); err != nil {
				t.Errorf("%s: %v", stmt, err)
			}
		}
	})

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	as := func(user, password string) string {
		c := *u
		c.User = url.UserPassword(user, password)
		c.Path = "/" + name
		return c.String()
	}
	return DB{OwnerURL: as(owner, "owner"), WriterURL: as(writer, "writer"), WriterRole: writer}
}
