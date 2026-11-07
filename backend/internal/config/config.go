// Package config loads runtime settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port        string
	DatabaseURL string

	// Busha
	BushaMode          string // "live" (sandbox/production API) or "mock" (offline fallback, clearly labelled)
	BushaBaseURL       string
	BushaSecretKey     string
	BushaPublicKey     string
	BushaProfileID     string
	BushaWebhookSecret string

	// Escrow rules (from our Scenario 3 pitch)
	DepositBps       int           // share released at booking, basis points (2500 = 25%)
	FreezeBps        int           // share of the remaining escrow a complaint can freeze
	ComplaintWindow  time.Duration // time after "gig done" before auto-release
	LateCancelWindow time.Duration // cancelling closer than this to the event is "late"
	MockRateNGN      int64         // NGN per 1 USDT used only in mock mode

	AllowDevRoutes bool
	CORSOrigin     string
}

func Load() (Config, error) {
	c := Config{
		Port:               env("PORT", "8080"),
		DatabaseURL:        env("DATABASE_URL", "postgres://gigvault:gigvault@localhost:5432/gigvault?sslmode=disable"),
		BushaMode:          env("BUSHA_MODE", "mock"),
		BushaBaseURL:       env("BUSHA_BASE_URL", "https://api.sandbox.busha.so"),
		BushaSecretKey:     os.Getenv("BUSHA_SECRET_KEY"),
		BushaPublicKey:     os.Getenv("BUSHA_PUBLIC_KEY"),
		BushaProfileID:     os.Getenv("BUSHA_PROFILE_ID"),
		BushaWebhookSecret: env("BUSHA_WEBHOOK_SECRET", "dev-webhook-secret"),
		DepositBps:         envInt("DEPOSIT_BPS", 2500),
		FreezeBps:          envInt("FREEZE_BPS", 5000),
		ComplaintWindow:    envDur("COMPLAINT_WINDOW", 60*time.Second),
		LateCancelWindow:   envDur("LATE_CANCEL_WINDOW", 24*time.Hour),
		MockRateNGN:        int64(envInt("MOCK_RATE_NGN", 1600)),
		AllowDevRoutes:     env("ALLOW_DEV_ROUTES", "true") == "true",
		CORSOrigin:         env("CORS_ORIGIN", "http://localhost:5173"),
	}
	if c.BushaMode != "mock" && c.BushaMode != "live" {
		return c, fmt.Errorf("BUSHA_MODE must be mock or live, got %q", c.BushaMode)
	}
	if c.BushaMode == "live" && c.BushaSecretKey == "" {
		return c, fmt.Errorf("BUSHA_SECRET_KEY is required when BUSHA_MODE=live")
	}
	if c.DepositBps < 0 || c.DepositBps > 10000 || c.FreezeBps < 0 || c.FreezeBps > 10000 {
		return c, fmt.Errorf("DEPOSIT_BPS and FREEZE_BPS must be between 0 and 10000")
	}
	return c, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
