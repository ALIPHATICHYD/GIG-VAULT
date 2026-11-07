// Package storetest gives tests a real, migrated Postgres to work against.
//
// The ledger's guarantees are enforced by Postgres itself — the append-only
// trigger, the unique idempotency key, CHECK constraints, transactions — so
// testing them against a fake would test nothing. Set TEST_DATABASE_URL to a
// throwaway database (a Neon branch is ideal) and these tests run; leave it
// unset and they skip, so `go test ./...` still passes on a machine with no
// database.
//
// Each call to New gets its own Postgres schema, created and migrated fresh and
// dropped afterwards. That keeps tests independent of each other and lets
// packages run in parallel without -p 1.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

// EnvVar names the connection string tests run against.
const EnvVar = "TEST_DATABASE_URL"

// New returns a pool bound to a private, freshly migrated schema, or skips the
// test when TEST_DATABASE_URL is unset. The schema is dropped on cleanup.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv(EnvVar)
	if url == "" {
		t.Skipf("%s is not set; skipping tests that need Postgres", EnvVar)
	}

	ctx := context.Background()
	schema := "gigvault_test_" + randomSuffix(t)

	// Always use the direct endpoint. Neon's pooled endpoint (PgBouncer)
	// refuses search_path as a startup parameter and does not keep a SET of it
	// across transactions, so isolating a test in its own schema is impossible
	// there — and a SET that does land leaks to whichever client is handed
	// that server connection next, breaking unrelated processes.
	url = direct(url)

	// Create the schema on a connection of its own, before the pool exists, so
	// every pooled connection can start with search_path already pointing at it.
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect to %s: %v", EnvVar, err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatalf("create schema %s: %v", schema, err)
	}
	admin.Close(ctx)

	// Drop the schema even if migration or the test itself fails.
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(dropCtx, url)
		if err != nil {
			t.Logf("warning: could not connect to drop schema %s: %v", schema, err)
			return
		}
		defer conn.Close(dropCtx)
		if _, err := conn.Exec(dropCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Logf("warning: could not drop schema %s: %v", schema, err)
		}
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse %s: %v", EnvVar, err)
	}
	// Neon ignores search_path passed as a startup parameter, so it has to be a
	// SET on each connection as it is established. That is safe here only
	// because the DSN above is the direct endpoint: the same SET on a pooled
	// connection would leak to other clients sharing that server connection.
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize())
		return err
	}
	cfg.MaxConns = 4

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Never migrate into the wrong schema: that is how a test suite quietly
	// starts writing to real tables.
	var current string
	if err := pool.QueryRow(ctx, "SELECT current_schema()").Scan(&current); err != nil {
		t.Fatalf("check current_schema: %v", err)
	}
	if current != schema {
		t.Fatalf("current_schema() is %q, want %q: this connection ignores search_path, "+
			"so tests would share tables. Point %s at a direct (non-pooled) connection string.",
			current, schema, EnvVar)
	}

	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate into %s: %v", schema, err)
	}
	return pool
}

// direct turns a Neon pooled connection string into the direct one by dropping
// "-pooler" from the host. Any other DSN is returned unchanged. Rewriting the
// string rather than the parsed host means the host fallbacks pgx derives from
// it point at the direct endpoint too.
func direct(dsn string) string {
	return strings.Replace(dsn, "-pooler.", ".", 1)
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
