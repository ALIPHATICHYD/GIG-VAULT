package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aliphatichyd/gig-vault/backend/internal/auth"
	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/config"
	"github.com/aliphatichyd/gig-vault/backend/internal/gigs"
	"github.com/aliphatichyd/gig-vault/backend/internal/store/storetest"
)

// Covers the wiring the ratelimit package's own tests can't: that the limiter
// is actually attached to /v1/auth/login, that it answers with the status and
// error code the frontend expects, and that it rejects before checking the
// password.

const testPassword = "demo1234"

func newTestAPI(t *testing.T) (http.Handler, *gigs.Service) {
	t.Helper()
	pool := storetest.New(t)
	svc := &gigs.Service{
		DB:    pool,
		Busha: busha.NewMock(1600),
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg: config.Config{
			AuthSecret:      "test-secret",
			LoginRateLimit:  5,
			LoginRateWindow: time.Minute,
			CORSOrigin:      "*",
		},
	}
	api := &API{Svc: svc, Log: svc.Log, Signer: auth.Signer{Secret: []byte("test-secret")}}
	return api.Router(), svc
}

func login(t *testing.T, h http.Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return e
}

// "the 6th wrong attempt inside a minute gets a 429"
func TestSixthWrongLoginIsRateLimited(t *testing.T) {
	h, svc := newTestAPI(t)
	if _, err := svc.Signup(context.Background(), gigs.SignupInput{
		Email: "tolu@test.local", Password: testPassword, Name: "Tolu Adeyemi", Role: "client",
	}); err != nil {
		t.Fatalf("signup: %v", err)
	}

	// Five wrong guesses are rejected on the merits, not throttled.
	for i := 1; i <= 5; i++ {
		rec := login(t, h, "tolu@test.local", "wrong-password")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401 (%s)", i, rec.Code, rec.Body.String())
		}
		if code := decodeEnvelope(t, rec).Code; code != "invalid_credentials" {
			t.Fatalf("attempt %d: code %q, want invalid_credentials", i, code)
		}
	}

	// The sixth is throttled.
	rec := login(t, h, "tolu@test.local", "wrong-password")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt: status %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	e := decodeEnvelope(t, rec)
	if e.Code != "too_many_attempts" {
		t.Errorf("code = %q, want too_many_attempts", e.Code)
	}
	if e.Status != "error" {
		t.Errorf("status = %q, want error", e.Status)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("no Retry-After header")
	}

	// Being throttled must not leak whether the password was right: even the
	// correct one is refused while the window is open.
	rec = login(t, h, "tolu@test.local", testPassword)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("correct password while throttled: status %d, want 429", rec.Code)
	}
}

// Throttling is per email as well as per IP, so guessing against one account
// must not lock out another. (Both requests share an IP here, so the limit is
// deliberately not reached on the IP key.)
func TestRateLimitIsPerEmail(t *testing.T) {
	h, svc := newTestAPI(t)
	ctx := context.Background()
	for _, email := range []string{"victim@test.local", "other@test.local"} {
		if _, err := svc.Signup(ctx, gigs.SignupInput{
			Email: email, Password: testPassword, Name: "Someone", Role: "client",
		}); err != nil {
			t.Fatalf("signup %s: %v", email, err)
		}
	}

	// Four wrong guesses at one account: under the limit on both keys.
	for i := 0; i < 4; i++ {
		login(t, h, "victim@test.local", "wrong-password")
	}

	rec := login(t, h, "other@test.local", testPassword)
	if rec.Code != http.StatusOK {
		t.Errorf("other account: status %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

// A successful login still counts against the limit, because "5 login tries a
// minute" is about tries, not failures. This pins the behaviour down so it
// can't drift silently.
func TestSuccessfulLoginsAlsoCount(t *testing.T) {
	h, svc := newTestAPI(t)
	if _, err := svc.Signup(context.Background(), gigs.SignupInput{
		Email: "tolu@test.local", Password: testPassword, Name: "Tolu Adeyemi", Role: "client",
	}); err != nil {
		t.Fatalf("signup: %v", err)
	}

	for i := 1; i <= 5; i++ {
		if rec := login(t, h, "tolu@test.local", testPassword); rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status %d, want 200 (%s)", i, rec.Code, rec.Body.String())
		}
	}
	if rec := login(t, h, "tolu@test.local", testPassword); rec.Code != http.StatusTooManyRequests {
		t.Errorf("6th successful login: status %d, want 429", rec.Code)
	}
}

func TestLoginRateLimitCanBeDisabled(t *testing.T) {
	pool := storetest.New(t)
	svc := &gigs.Service{
		DB:    pool,
		Busha: busha.NewMock(1600),
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Cfg:   config.Config{AuthSecret: "s", LoginRateLimit: 0, LoginRateWindow: time.Minute},
	}
	h := (&API{Svc: svc, Log: svc.Log, Signer: auth.Signer{Secret: []byte("s")}}).Router()

	for i := 0; i < 12; i++ {
		if rec := login(t, h, "nobody@test.local", "wrong"); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was throttled; a zero limit should disable throttling", i)
		}
	}
}
