// Command api runs the GigVault HTTP API and its background worker.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aliphatichyd/gig-vault/backend/internal/auth"
	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/config"
	"github.com/aliphatichyd/gig-vault/backend/internal/gigs"
	"github.com/aliphatichyd/gig-vault/backend/internal/httpapi"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
	"github.com/aliphatichyd/gig-vault/backend/internal/worker"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	loadDotEnv(".env")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}

	var api busha.API
	if cfg.BushaMode == "live" {
		api = busha.NewClient(cfg.BushaBaseURL, cfg.BushaSecretKey, cfg.BushaProfileID, log)
		log.Info("busha: live", "base_url", cfg.BushaBaseURL)
	} else {
		api = busha.NewMock(cfg.MockRateNGN)
		log.Warn("busha: MOCK mode (no real API calls). Set BUSHA_MODE=live with sandbox keys.")
	}

	svc := &gigs.Service{DB: pool, Busha: api, Cfg: cfg, Log: log}
	if err := svc.EnsureDemoWorker(ctx); err != nil {
		return err
	}

	wk := &worker.Worker{DB: pool, Log: log, Handlers: map[string]worker.Handler{
		gigs.JobBushaEvent: func(ctx context.Context, p json.RawMessage) error {
			var in struct {
				EventID int64 `json:"event_id"`
			}
			if err := json.Unmarshal(p, &in); err != nil {
				return err
			}
			return svc.ProcessEvent(ctx, in.EventID)
		},
		gigs.JobAutoRelease: func(ctx context.Context, p json.RawMessage) error {
			var in struct {
				GigID string `json:"gig_id"`
			}
			if err := json.Unmarshal(p, &in); err != nil {
				return err
			}
			return svc.AutoRelease(ctx, in.GigID)
		},
		gigs.JobMockSettlement: func(ctx context.Context, p json.RawMessage) error {
			var in struct {
				TransferID string `json:"transfer_id"`
			}
			if err := json.Unmarshal(p, &in); err != nil {
				return err
			}
			return svc.CompletePayoutByTransfer(ctx, in.TransferID)
		},
	}}
	go wk.Run(ctx)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: (&httpapi.API{Svc: svc, Log: log, Signer: auth.Signer{Secret: []byte(cfg.AuthSecret)}}).Router(),
		ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("listening", "port", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loadDotEnv reads KEY=VALUE lines without overriding real env vars.
func loadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range splitLines(string(b)) {
		if line == "" || line[0] == '#' {
			continue
		}
		for i := 0; i < len(line); i++ {
			if line[i] == '=' {
				k, v := line[:i], line[i+1:]
				if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
					v = v[1 : len(v)-1]
				}
				if _, set := os.LookupEnv(k); !set {
					os.Setenv(k, v)
				}
				break
			}
		}
	}
}

func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
