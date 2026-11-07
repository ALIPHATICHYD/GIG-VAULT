// Package store owns the Postgres pool, migrations and small shared helpers.
package store

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		sql, err := migrations.ReadFile("migrations/" + n)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", n, err)
		}
	}
	return nil
}

// MigrateURL connects to url (typically DATABASE_URL_UNPOOLED on Neon) and runs schema migrations.
func MigrateURL(ctx context.Context, url string) error {
	pool, err := Connect(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	return Migrate(ctx, pool)
}

// InTx runs fn in a transaction and commits only if fn returns nil.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const alphabet = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// NewID returns a prefixed random ID, e.g. GIG_8fK2...
func NewID(prefix string) string {
	b := make([]byte, 16)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[n.Int64()]
	}
	return prefix + "_" + string(b)
}

// Audit writes one audit_log row inside the caller's transaction.
func Audit(ctx context.Context, db DBTX, gigID, workerID, actor, action, from, to string, details any) error {
	var js []byte
	if details != nil {
		js, _ = json.Marshal(details)
	}
	_, err := db.Exec(ctx, `INSERT INTO audit_log (gig_id, worker_id, actor, action, from_status, to_status, details)
		VALUES (NULLIF($1,''), NULLIF($2,''), $3, $4, NULLIF($5,''), NULLIF($6,''), $7)`,
		gigID, workerID, actor, action, from, to, js)
	return err
}

// Enqueue adds a job. A non-empty dedupeKey makes the call idempotent.
func Enqueue(ctx context.Context, db DBTX, kind string, payload any, dedupeKey string, runAt any) error {
	js, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO jobs (kind, payload, dedupe_key, run_at)
		VALUES ($1, $2, NULLIF($3,''), COALESCE($4::timestamptz, now()))
		ON CONFLICT (dedupe_key) DO NOTHING`, kind, js, dedupeKey, runAt)
	return err
}
