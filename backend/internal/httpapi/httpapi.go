// Package httpapi exposes the GigVault REST API. Responses mirror Busha's
// envelope: {"status": "success"|"error", "message": "...", "data": ...}.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/gigs"
)

type API struct {
	Svc *gigs.Service
	Log *slog.Logger
}

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, a.cors, a.logRequests)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		ok(w, http.StatusOK, "ok", map[string]string{"busha_mode": a.Svc.Busha.Mode()})
	})

	r.Route("/v1", func(r chi.Router) {
		r.Get("/workers", a.listWorkers)
		r.Post("/workers", a.createWorker)
		r.Get("/workers/{id}/wallet", a.wallet)
		r.Post("/workers/{id}/cashout", a.cashout)

		r.Get("/gigs", a.listGigs)
		r.Post("/gigs", a.createGig)
		r.Get("/gigs/{id}", a.getGig)
		r.Post("/gigs/{id}/complete", a.completeGig)
		r.Post("/gigs/{id}/complaints", a.complain)
		r.Post("/gigs/{id}/complaints/resolve", a.resolve)
		r.Post("/gigs/{id}/cancel", a.cancel)

		r.Get("/banks", a.banks)
		r.Get("/admin/reconciliation", a.reconcile)

		r.Post("/webhooks/busha", a.bushaWebhook)

		if a.Svc.Cfg.AllowDevRoutes {
			// Dev only: fire a correctly signed Busha webhook at ourselves, so the
			// full webhook -> queue -> ledger path runs even without a public URL.
			r.Post("/dev/gigs/{id}/simulate-payment", a.simulatePayment)
		}
	})
	return r
}

// ---------- handlers ----------

func (a *API) listWorkers(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.ListWorkers(r.Context())
	a.respond(w, r, http.StatusOK, "workers retrieved", out, err)
}

func (a *API) createWorker(w http.ResponseWriter, r *http.Request) {
	var in gigs.CreateWorkerInput
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.CreateWorker(r.Context(), in)
	a.respond(w, r, http.StatusCreated, "worker created", out, err)
}

func (a *API) wallet(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.Wallet(r.Context(), chi.URLParam(r, "id"))
	a.respond(w, r, http.StatusOK, "wallet retrieved", out, err)
}

func (a *API) cashout(w http.ResponseWriter, r *http.Request) {
	var in gigs.CashoutInput
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.Cashout(r.Context(), chi.URLParam(r, "id"), in)
	a.respond(w, r, http.StatusCreated, "cash-out started", out, err)
}

func (a *API) listGigs(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.ListGigs(r.Context(), r.URL.Query().Get("worker_id"))
	a.respond(w, r, http.StatusOK, "gigs retrieved", out, err)
}

func (a *API) createGig(w http.ResponseWriter, r *http.Request) {
	var in gigs.CreateGigInput
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.CreateGig(r.Context(), in)
	a.respond(w, r, http.StatusCreated, "gig created; waiting for payment", out, err)
}

func (a *API) getGig(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.GetGig(r.Context(), chi.URLParam(r, "id"))
	a.respond(w, r, http.StatusOK, "gig retrieved", out, err)
}

func (a *API) completeGig(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.Complete(r.Context(), chi.URLParam(r, "id"))
	a.respond(w, r, http.StatusOK, "gig marked done; complaint window started", out, err)
}

func (a *API) complain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.Complain(r.Context(), chi.URLParam(r, "id"), in.Reason)
	a.respond(w, r, http.StatusOK, "complaint opened; part of the payment is frozen", out, err)
}

func (a *API) resolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Outcome string `json:"outcome"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := a.Svc.ResolveComplaint(r.Context(), chi.URLParam(r, "id"), in.Outcome)
	a.respond(w, r, http.StatusOK, "complaint resolved", out, err)
}

func (a *API) cancel(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.Cancel(r.Context(), chi.URLParam(r, "id"))
	a.respond(w, r, http.StatusOK, "gig cancelled", out, err)
}

func (a *API) banks(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.Busha.ListBanks(r.Context())
	a.respond(w, r, http.StatusOK, "banks retrieved", out, err)
}

func (a *API) reconcile(w http.ResponseWriter, r *http.Request) {
	out, err := a.Svc.Reconcile(r.Context())
	a.respond(w, r, http.StatusOK, "reconciliation", out, err)
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
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
