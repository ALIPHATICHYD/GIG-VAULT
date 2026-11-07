// Package httpapi exposes the GigVault REST API. Responses mirror Busha's
// envelope: {"status": "success"|"error", "message": "...", "data": ...}.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aliphatichyd/gig-vault/backend/internal/auth"
	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/gigs"
)

type API struct {
	Svc    *gigs.Service
	Log    *slog.Logger
	Signer auth.Signer
}

const sessionTTL = 7 * 24 * time.Hour

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, a.cors, a.logRequests)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ok(w, http.StatusOK, "ok", map[string]string{"busha_mode": a.Svc.Busha.Mode()})
	})

	r.Route("/v1", func(r chi.Router) {
		// Accounts: one sign-up, role = creator | client.
		r.Post("/auth/signup", a.signup)
		r.Post("/auth/login", a.login)
		r.With(a.Signer.Require("", fail)).Get("/me", a.me)

		// Public: anyone with the link can preview the gig before signing up.
		r.Get("/pay/{id}", a.previewGig)
		r.Get("/banks", a.banks)
		r.Post("/banks/resolve", a.resolveBank)

		// Creator screens.
		r.Group(func(r chi.Router) {
			r.Use(a.Signer.Require(auth.Creator, fail))
			r.Post("/creator/onboard", a.onboard)
			r.Get("/creator/wallet", a.creatorWallet)
			r.Get("/creator/activity", a.creatorActivity)
			r.Post("/creator/gigs", a.createGigLink)
			r.Get("/creator/gigs/{id}", a.creatorGig)
			r.Post("/creator/cashout", a.creatorCashout)
			r.Post("/creator/gigs/{id}/dispute/propose", a.creatorDispute("propose"))
			r.Post("/creator/gigs/{id}/dispute/accept", a.creatorDispute("accept"))
		})

		// Client screens.
		r.Group(func(r chi.Router) {
			r.Use(a.Signer.Require(auth.Client, fail))
			r.Post("/client/claim/{id}", a.claimGig)
			r.Get("/client/bookings", a.clientBookings)
			r.Get("/client/bookings/{id}", a.clientGig)
			r.Post("/client/bookings/{id}/checkout", a.clientCheckout)
			r.Post("/client/bookings/{id}/complete", a.clientAction("complete"))
			r.Post("/client/bookings/{id}/complaints", a.clientAction("complain"))
			r.Post("/client/bookings/{id}/cancel", a.clientAction("cancel"))
			r.Post("/client/bookings/{id}/dispute/propose", a.clientPropose)
			r.Post("/client/bookings/{id}/dispute/accept", a.clientAction("accept_split"))
		})

		// Platform admin (X-Admin-Key).
		r.Group(func(r chi.Router) {
			r.Use(a.requireAdmin)
			r.Post("/admin/gigs/{id}/dispute/resolve", a.resolve)
			r.Get("/admin/reconciliation", a.reconcile)
		})

		r.Post("/webhooks/busha", a.bushaWebhook)

		if a.Svc.Cfg.AllowDevRoutes {
			// Dev only: fire a correctly signed Busha webhook at ourselves, so the
			// full webhook -> queue -> ledger path runs even without a public URL.
			r.Post("/dev/gigs/{id}/simulate-payment", a.simulatePayment)
		}
	})
	return r
}

// ---------- accounts ----------

type sessionResp struct {
	Token string     `json:"token"`
	User  *gigs.User `json:"user"`
}

func (a *API) signup(w http.ResponseWriter, r *http.Request) {
	var in gigs.SignupInput
	if !decode(w, r, &in) {
		return
	}
	u, err := a.Svc.Signup(r.Context(), in)
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	ok(w, http.StatusCreated, "account created", sessionResp{a.Signer.Issue(u.ID, auth.Role(u.Role), sessionTTL), u})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := a.Svc.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	ok(w, http.StatusOK, "logged in", sessionResp{a.Signer.Issue(u.ID, auth.Role(u.Role), sessionTTL), u})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	u, err := a.Svc.GetUser(r.Context(), sess(r).UserID)
	a.respond(w, r, http.StatusOK, "me", u, err)
}

// ---------- creator ----------

func (a *API) onboard(w http.ResponseWriter, r *http.Request) {
	var in gigs.OnboardInput
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.OnboardCreator(r.Context(), sess(r).UserID, in)
	a.respond(w, r, http.StatusCreated, "creator profile ready", out, err)
}

func (a *API) creatorWallet(w http.ResponseWriter, r *http.Request) {
	wid, err := a.Svc.WorkerFor(r.Context(), sess(r).UserID)
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	out, err := a.Svc.Wallet(r.Context(), wid)
	a.respond(w, r, http.StatusOK, "wallet retrieved", out, err)
}

func (a *API) creatorActivity(w http.ResponseWriter, r *http.Request) {
	wid, err := a.Svc.WorkerFor(r.Context(), sess(r).UserID)
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	out, err := a.Svc.Activity(r.Context(), wid)
	a.respond(w, r, http.StatusOK, "activity", out, err)
}

func (a *API) createGigLink(w http.ResponseWriter, r *http.Request) {
	var in gigs.CreateGigLinkInput
	if !decode(w, r, &in) {
		return
	}
	wid, err := a.Svc.WorkerFor(r.Context(), sess(r).UserID)
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	out, err := a.Svc.CreateGigLink(r.Context(), wid, in)
	a.respond(w, r, http.StatusCreated, "gig link created; send it to your client", out, err)
}

func (a *API) creatorGig(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.CreatorGig(r.Context(), sess(r).UserID, chi.URLParam(r, "id"))
	a.respond(w, r, http.StatusOK, "gig retrieved", out, err)
}

func (a *API) creatorCashout(w http.ResponseWriter, r *http.Request) {
	var in gigs.CashoutInput
	if !decode(w, r, &in) {
		return
	}
	wid, err := a.Svc.WorkerFor(r.Context(), sess(r).UserID)
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	out, err := a.Svc.Cashout(r.Context(), wid, in)
	a.respond(w, r, http.StatusCreated, "cash-out started", out, err)
}

// ---------- public + client ----------

func (a *API) previewGig(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.PublicGig(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("t"))
	if err == nil {
		a.Svc.MarkOpened(r.Context(), out.ID)
	}
	a.respond(w, r, http.StatusOK, "gig", out, err)
}

func (a *API) claimGig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.ClaimGig(r.Context(), sess(r).UserID, chi.URLParam(r, "id"), in.Token)
	a.respond(w, r, http.StatusOK, "gig added to your bookings", out, err)
}

func (a *API) clientBookings(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.ClientBookings(r.Context(), sess(r).UserID)
	a.respond(w, r, http.StatusOK, "bookings", out, err)
}

func (a *API) clientGig(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.ClientGig(r.Context(), sess(r).UserID, chi.URLParam(r, "id"))
	a.respond(w, r, http.StatusOK, "booking", out, err)
}

func (a *API) clientCheckout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Currency string `json:"currency"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.ClientCheckout(r.Context(), sess(r).UserID, chi.URLParam(r, "id"), in.Currency)
	a.respond(w, r, http.StatusOK, "pay using the details below", out, err)
}

func (a *API) clientAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Reason string `json:"reason"`
		}
		if r.ContentLength != 0 && !decode(w, r, &in) {
			return
		}
		out, err := a.Svc.ClientAction(r.Context(), sess(r).UserID, chi.URLParam(r, "id"), action, in.Reason)
		a.respond(w, r, http.StatusOK, action+" done", out, err)
	}
}

func (a *API) clientPropose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CreatorPercent int `json:"creator_percent"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.ClientProposeSplit(r.Context(), sess(r).UserID, chi.URLParam(r, "id"), in.CreatorPercent)
	a.respond(w, r, http.StatusOK, "proposal sent", out, err)
}

func (a *API) creatorDispute(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			CreatorPercent int `json:"creator_percent"`
		}
		if r.ContentLength != 0 && !decode(w, r, &in) {
			return
		}
		out, err := a.Svc.CreatorDispute(r.Context(), sess(r).UserID, chi.URLParam(r, "id"), action, in.CreatorPercent)
		a.respond(w, r, http.StatusOK, "dispute "+action+" done", out, err)
	}
}

// ---------- admin + misc ----------

func (a *API) resolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CreatorPercent int `json:"creator_percent"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.ResolveDispute(r.Context(), chi.URLParam(r, "id"), in.CreatorPercent)
	a.respond(w, r, http.StatusOK, "dispute resolved by GigVault", out, err)
}

func (a *API) banks(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.Busha.ListBanks(r.Context())
	a.respond(w, r, http.StatusOK, "banks retrieved", out, err)
}

func (a *API) resolveBank(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BankCode      string `json:"bank_code"`
		AccountNumber string `json:"account_number"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.ResolveBank(r.Context(), in.BankCode, in.AccountNumber)
	a.respond(w, r, http.StatusOK, "account verified", out, err)
}

func (a *API) reconcile(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.Reconcile(r.Context())
	a.respond(w, r, http.StatusOK, "reconciliation", out, err)
}

func (a *API) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Admin-Key")), []byte(a.Svc.Cfg.AdminKey)) != 1 {
			fail(w, http.StatusUnauthorized, "unauthenticated", "admin key required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sess(r *http.Request) auth.Session {
	s, _ := auth.FromContext(r.Context())
	return s
}

// bushaWebhook verifies the signature, stores the raw event, queues it and
// returns 200 fast. All real work happens in the worker.
func (a *API) bushaWebhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_body", "could not read body")
		return
	}
	if !busha.VerifySignature(raw, r.Header.Get(busha.SignatureHeader), a.Svc.Cfg.BushaWebhookSecret) {
		a.Log.Warn("rejected webhook with bad signature", "ip", r.RemoteAddr)
		fail(w, http.StatusUnauthorized, "invalid_signature", "webhook signature verification failed")
		return
	}
	dup, err := a.Svc.IngestEvent(r.Context(), raw)
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	msg := "event queued"
	if dup {
		msg = "duplicate event ignored"
	}
	ok(w, http.StatusOK, msg, map[string]bool{"duplicate": dup})
}

func (a *API) simulatePayment(w http.ResponseWriter, r *http.Request) {
	d, err := a.Svc.GetGig(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.respond(w, r, 0, "", nil, err)
		return
	}
	target := ""
	if d.Gig.AmountUSDTMicro != nil {
		target = gigs.FormatMinor(*d.Gig.AmountUSDTMicro, 6)
	}
	prID := ""
	if d.Gig.BushaPaymentRequestID != nil {
		prID = *d.Gig.BushaPaymentRequestID
	}
	body, _ := json.Marshal(map[string]any{"business_id": "BUS_dev", "event": "payment_request.completed",
		"data": map[string]string{"id": prID, "reference": d.Gig.ID, "status": "completed", "target_amount": target}})
	dup, err := a.Svc.IngestEvent(r.Context(), body)
	a.respond(w, r, http.StatusOK, "simulated payment_request.completed webhook", map[string]any{"duplicate": dup,
		"signature": busha.Sign(body, a.Svc.Cfg.BushaWebhookSecret), "payload": json.RawMessage(body)}, err)
}

// ---------- helpers ----------

type envelope struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Code    string `json:"code,omitempty"`
}

func (a *API) respond(w http.ResponseWriter, r *http.Request, status int, msg string, data any, err error) {
	if err == nil {
		ok(w, status, msg, data)
		return
	}
	if ae, isApp := gigs.AsAppError(err); isApp {
		if ae.HTTP >= 500 {
			a.Log.Error("request failed", "path", r.URL.Path, "request_id", middleware.GetReqID(r.Context()), "err", err)
		}
		fail(w, ae.HTTP, ae.Code, ae.Msg)
		return
	}
	var be *busha.Error
	if errors.As(err, &be) {
		a.Log.Error("busha error", "path", r.URL.Path, "err", err)
		fail(w, http.StatusBadGateway, "busha_error", "Busha returned an error (request "+be.RequestID+")")
		return
	}
	a.Log.Error("internal error", "path", r.URL.Path, "request_id", middleware.GetReqID(r.Context()), "err", err)
	fail(w, http.StatusInternalServerError, "internal_error", "something went wrong; no money was moved")
}

func ok(w http.ResponseWriter, status int, msg string, data any) {
	writeJSON(w, status, envelope{Status: "success", Message: msg, Data: data})
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, envelope{Status: "error", Message: msg, Code: code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", a.Svc.Cfg.CORSOrigin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Key, ngrok-skip-browser-warning")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		a.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
			"ms", time.Since(start).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
	})
}
