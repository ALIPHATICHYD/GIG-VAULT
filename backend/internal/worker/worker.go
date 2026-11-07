// Package worker runs background jobs from the Postgres "jobs" table.
//
// Claiming uses SELECT ... FOR UPDATE SKIP LOCKED, so several API instances can
// run workers safely. Failed jobs retry with backoff up to MaxAttempts.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxAttempts = 8

type Handler func(ctx context.Context, payload json.RawMessage) error

type Worker struct {
	DB       *pgxpool.Pool
	Log      *slog.Logger
	Handlers map[string]Handler
	Interval time.Duration
}

func (w *Worker) Run(ctx context.Context) {
	if w.Interval == 0 {
		w.Interval = time.Second
	}
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		// Drain everything that is due, then wait for the next tick.
		for {
			ran, err := w.runOne(ctx)
			if err != nil {
				w.Log.Error("worker", "err", err)
			}
			if !ran || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *Worker) runOne(ctx context.Context) (bool, error) {
	var id int64
	var kind string
	var payload json.RawMessage
	var attempts int
	err := w.DB.QueryRow(ctx, `UPDATE jobs SET locked_at = now(), attempts = attempts + 1
		WHERE id = (
			SELECT id FROM jobs
			WHERE done_at IS NULL AND run_at <= now() AND attempts < $1
			  AND (locked_at IS NULL OR locked_at < now() - interval '2 minutes')
			ORDER BY run_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, kind, payload, attempts`, MaxAttempts).Scan(&id, &kind, &payload, &attempts)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	h, ok := w.Handlers[kind]
	var jobErr error
	if !ok {
		jobErr = fmt.Errorf("no handler for job kind %q", kind)
	} else {
		jobErr = safeRun(ctx, h, payload)
	}
	if jobErr == nil {
		_, err = w.DB.Exec(ctx, `UPDATE jobs SET done_at = now(), locked_at = NULL, last_error = NULL WHERE id = $1`, id)
		w.Log.Info("job done", "id", id, "kind", kind)
		return true, err
	}
	backoff := time.Duration(attempts*attempts) * 2 * time.Second
	w.Log.Warn("job failed", "id", id, "kind", kind, "attempt", attempts, "retry_in", backoff, "err", jobErr)
	_, err = w.DB.Exec(ctx, `UPDATE jobs SET locked_at = NULL, last_error = $2, run_at = now() + $3::interval WHERE id = $1`,
		id, jobErr.Error(), fmt.Sprintf("%d seconds", int(backoff.Seconds())))
	return true, err
}

func safeRun(ctx context.Context, h Handler, payload json.RawMessage) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return h(ctx, payload)
}
