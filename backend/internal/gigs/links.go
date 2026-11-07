package gigs

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
	"github.com/aliphatichyd/gig-vault/backend/internal/store"
)

// ---------- creator onboarding ----------

type OnboardInput struct {
	Name          string `json:"name"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Craft         string `json:"craft"`   // e.g. "Singer · Nail tech"
	IDType        string `json:"id_type"` // bvn | nin
	IDNumber      string `json:"id_number"`
	BankCode      string `json:"bank_code"`
	BankName      string `json:"bank_name"`
	AccountNumber string `json:"account_number"`
	BirthDate     string `json:"birth_date"` // DD-MM-YYYY, needed by Busha customers
	City          string `json:"city"`
	State         string `json:"state"`
	Street        string `json:"street"`
}

type OnboardResult struct {
	Worker  *Worker  `json:"worker"`
	KYCLink *string  `json:"kyc_link"`
	Notes   []string `json:"notes"`
}

var elevenDigits = regexp.MustCompile(`^\d{11}$`)
var tenDigits = regexp.MustCompile(`^\d{10}$`)

// Onboard creates a creator:
//  1. Busha resolves the bank account so we show her the real account name
//  2. Busha recipient for that account (used by every future cash-out)
//  3. Busha customer + hosted KYC link (BVN/NIN checks happen at Busha)
//
// Steps 3 is best effort: a KYC hiccup must not stop her getting paid later.
func (s *Service) Onboard(ctx context.Context, in OnboardInput) (*OnboardResult, error) {
	in.IDType = strings.ToLower(strings.TrimSpace(in.IDType))
	switch {
	case strings.TrimSpace(in.Name) == "":
		return nil, badRequest("validation_error", "name is required")
	case strings.TrimSpace(in.Phone) == "":
		return nil, badRequest("validation_error", "phone is required")
	case in.IDType != "bvn" && in.IDType != "nin":
		return nil, badRequest("validation_error", "id_type must be bvn or nin")
	case !elevenDigits.MatchString(in.IDNumber):
		return nil, badRequest("validation_error", "BVN and NIN are 11 digits")
	case in.BankCode == "" || !tenDigits.MatchString(in.AccountNumber):
		return nil, badRequest("validation_error", "bank_code and a 10-digit account_number are required")
	}

	acct, err := s.Busha.ResolveBankAccount(ctx, in.BankCode, in.AccountNumber)
	if err != nil {
		return nil, &AppError{HTTP: http.StatusUnprocessableEntity, Code: "bank_account_invalid",
			Msg: "Busha couldn't verify that bank account", Err: err}
	}
	bankName := in.BankName
	if bankName == "" {
		bankName = acct.BankName
	}
	rcp, err := s.Busha.CreateRecipient(ctx, busha.RecipientInput{Type: "ngn_bank", Currency: "NGN", CountryCode: "NG",
		AccountName: acct.AccountName, AccountNumber: in.AccountNumber, BankCode: in.BankCode, BankName: bankName, Email: in.Email})
	if err != nil {
		return nil, upstream("could not save the bank account on Busha", err)
	}

	res := &OnboardResult{Notes: []string{}}
	kycStatus := "not_started"
	var customerID, kycLink *string
	first, last := splitName(in.Name)
	cus, err := s.Busha.CreateCustomer(ctx, busha.CustomerInput{Email: in.Email, HasAcceptedTerms: true, Type: "individual",
		CountryID: "NG", Phone: in.Phone, BirthDate: in.BirthDate, FirstName: first, LastName: last,
		Address: busha.Address{Street: def(in.Street, "N/A"), City: def(in.City, "Lagos"), State: def(in.State, "Lagos"), Country: "NG"}})
	if err != nil {
		s.Log.Warn("busha customer create failed", "err", err)
		res.Notes = append(res.Notes, "KYC not started on Busha yet: "+err.Error())
	} else {
		customerID = &cus.ID
		kycStatus = "pending"
		if link, err := s.Busha.KYCLink(ctx, cus.ID); err == nil {
			kycLink = &link
		} else {
			res.Notes = append(res.Notes, "KYC link not available: "+err.Error())
		}
	}

	id := store.NewID("WRK")
	last4 := in.IDNumber[len(in.IDNumber)-4:]
	err = store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO workers (id, name, email, phone, craft, id_type, id_last4, kyc_status, kyc_link,
			busha_customer_id, busha_recipient_id, bank_name, bank_code, account_number, account_name)
			VALUES ($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			id, in.Name, in.Email, in.Phone, in.Craft, in.IDType, last4, kycStatus, kycLink, customerID, rcp.ID,
			bankName, in.BankCode, in.AccountNumber, acct.AccountName); err != nil {
			return err
		}
		return store.Audit(ctx, tx, "", id, "worker", "worker.onboarded", "", kycStatus,
			map[string]any{"busha_recipient_id": rcp.ID, "busha_customer_id": customerID, "id_type": in.IDType})
	})
	if err != nil {
		return nil, err
	}
	if res.Worker, err = getWorker(ctx, s.DB, id); err != nil {
		return nil, err
	}
	res.KYCLink = kycLink
	return res, nil
}

func (s *Service) GetWorker(ctx context.Context, id string) (*Worker, error) {
	return getWorker(ctx, s.DB, id)
}

func (s *Service) ResolveBank(ctx context.Context, bankCode, accountNumber string) (*busha.ResolvedAccount, error) {
	if bankCode == "" || !tenDigits.MatchString(accountNumber) {
		return nil, badRequest("validation_error", "bank_code and a 10-digit account_number are required")
	}
	acct, err := s.Busha.ResolveBankAccount(ctx, bankCode, accountNumber)
	if err != nil {
		return nil, &AppError{HTTP: http.StatusUnprocessableEntity, Code: "bank_account_invalid", Msg: "Busha couldn't verify that bank account", Err: err}
	}
	return acct, nil
}

// ---------- creator: gig link ----------

type CreateGigLinkInput struct {
	Title      string    `json:"title"`
	EventAt    time.Time `json:"event_at"`
	AmountNGN  string    `json:"amount_ngn"`
	ClientName string    `json:"client_name"` // optional, pre-fills the pay page
	Note       string    `json:"note"`
}

type GigLink struct {
	Gig     *Gig   `json:"gig"`
	PayPath string `json:"pay_path"` // /pay/GIG_x?t=token (prefix with the web app origin)
}

// CreateGigLink is what the creator does after agreeing a price on WhatsApp.
// No money moves yet; the client opens the link and pays.
func (s *Service) CreateGigLink(ctx context.Context, workerID string, in CreateGigLinkInput) (*GigLink, error) {
	switch {
	case strings.TrimSpace(in.Title) == "":
		return nil, badRequest("validation_error", "title is required")
	case in.EventAt.IsZero():
		return nil, badRequest("validation_error", "event_at is required (RFC 3339)")
	}
	kobo, err := ParseDecimal(in.AmountNGN, 2)
	if err != nil || kobo < 100_00 {
		return nil, badRequest("validation_error", "amount_ngn must be at least ₦100")
	}
	if _, err := getWorker(ctx, s.DB, workerID); err != nil {
		return nil, err
	}
	id, token := store.NewID("GIG"), newToken()
	err = store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO gigs (id, worker_id, title, client_name, note, event_at, amount_ngn_kobo,
			deposit_bps, freeze_bps, complaint_window_seconds, status, payment_reference, client_token)
			VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,$8,$9,$10,$11,$1,$12)`,
			id, workerID, in.Title, in.ClientName, in.Note, in.EventAt, kobo, s.Cfg.DepositBps, s.Cfg.FreezeBps,
			int(s.Cfg.ComplaintWindow.Seconds()), StatusAwaitingPayment, token); err != nil {
			return err
		}
		return store.Audit(ctx, tx, id, workerID, "worker", "gig.link_created", "", StatusAwaitingPayment,
			map[string]any{"amount_ngn_kobo": kobo})
	})
	if err != nil {
		return nil, err
	}
	g, err := getGig(ctx, s.DB, id, false)
	if err != nil {
		return nil, err
	}
	return &GigLink{Gig: g, PayPath: "/pay/" + id + "?t=" + token}, nil
}

// ---------- client: public pay link ----------

// CheckToken guards every client action. The token in the link is the client's
// only credential, so compare in constant time and give nothing away on failure.
func (s *Service) CheckToken(ctx context.Context, gigID, token string) error {
	g, err := getGig(ctx, s.DB, gigID, false)
	if err != nil {
		return notFound("gig")
	}
	if token == "" || g.ClientToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(g.ClientToken)) != 1 {
		return notFound("gig")
	}
	return nil
}

type PublicGig struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Note            *string    `json:"note"`
	EventAt         time.Time  `json:"event_at"`
	AmountNGNKobo   int64      `json:"amount_ngn_kobo"`
	AmountUSDTMicro *int64     `json:"amount_usdt_micro"`
	Status          string     `json:"status"`
	ClientName      *string    `json:"client_name"`
	PayCurrency     *string    `json:"pay_currency"`
	PayInMethod     *string    `json:"pay_in_method"`
	PaymentDetails  any        `json:"payment_details"`
	ReleaseAt       *time.Time `json:"release_at"`
	DepositPercent  int        `json:"deposit_percent"`
	FreezePercent   int        `json:"freeze_percent"`
	WindowSeconds   int        `json:"complaint_window_seconds"`
	Creator         struct {
		Name     string  `json:"name"`
		Craft    *string `json:"craft"`
		Verified bool    `json:"verified"`
	} `json:"creator"`
	BushaMode string `json:"busha_mode"`
}

func (s *Service) PublicGig(ctx context.Context, gigID, token string) (*PublicGig, error) {
	if err := s.CheckToken(ctx, gigID, token); err != nil {
		return nil, err
	}
	g, err := getGig(ctx, s.DB, gigID, false)
	if err != nil {
		return nil, err
	}
	w, err := getWorker(ctx, s.DB, g.WorkerID)
	if err != nil {
		return nil, err
	}
	p := &PublicGig{ID: g.ID, Title: g.Title, Note: g.Note, EventAt: g.EventAt, AmountNGNKobo: g.AmountNGNKobo,
		AmountUSDTMicro: g.AmountUSDTMicro, Status: g.Status, ClientName: g.ClientName, PayCurrency: g.PayCurrency,
		PayInMethod: g.PayInMethod, ReleaseAt: g.ReleaseAt, DepositPercent: g.DepositBps / 100,
		FreezePercent: g.FreezeBps / 100, WindowSeconds: g.ComplaintWindowSeconds, BushaMode: s.Busha.Mode()}
	if len(g.PaymentDetails) > 0 {
		p.PaymentDetails = g.PaymentDetails
	}
	p.Creator.Name, p.Creator.Craft, p.Creator.Verified = w.Name, w.Craft, w.KYCStatus == "verified"
	return p, nil
}

type CheckoutInput struct {
	ClientName  string `json:"client_name"`
	ClientEmail string `json:"client_email"`
	Currency    string `json:"currency"` // NGN (bank transfer) or USDT (from abroad)
}

// Checkout creates the Busha payment request for a gig link. The price is
// always in naira; a client paying in USDT is quoted in NGN by Busha
// (quote_currency/quote_amount). Calling it again returns the same request.
func (s *Service) Checkout(ctx context.Context, gigID, token string, in CheckoutInput) (*PublicGig, error) {
	if err := s.CheckToken(ctx, gigID, token); err != nil {
		return nil, err
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "NGN"
	}
	switch {
	case strings.TrimSpace(in.ClientName) == "" || !strings.Contains(in.ClientEmail, "@"):
		return nil, badRequest("validation_error", "your name and a valid email are required")
	case in.Currency != "NGN" && in.Currency != "USDT":
		return nil, badRequest("validation_error", "currency must be NGN or USDT")
	}
	g, err := getGig(ctx, s.DB, gigID, false)
	if err != nil {
		return nil, err
	}
	if g.Status != StatusAwaitingPayment {
		return nil, conflict("invalid_state", "this gig is already paid or closed")
	}
	if g.BushaPaymentRequestID != nil && g.PayCurrency != nil && *g.PayCurrency == in.Currency {
		return s.PublicGig(ctx, gigID, token) // idempotent: same request again
	}

	naira := FormatMinor(g.AmountNGNKobo, 2)
	req := busha.PaymentRequestInput{TargetCurrency: "USDT", Reference: g.ID}
	req.AdditionalInfo.Email, req.AdditionalInfo.Name = in.ClientEmail, in.ClientName
	if in.Currency == "NGN" {
		req.SourceCurrency, req.SourceAmount = "NGN", naira
		req.PayIn = &busha.PayIn{Type: "temporary_bank_account"}
	} else {
		req.SourceCurrency, req.QuoteCurrency, req.QuoteAmount = "USDT", "NGN", naira
		req.PayIn = &busha.PayIn{Type: "address", Network: "TRX"}
	}
	pr, err := s.Busha.CreatePaymentRequest(ctx, req)
	if err != nil {
		return nil, upstream("could not create Busha payment request", err)
	}
	var usdtPtr *int64
	var ratePtr *float64
	if t, err := ParseDecimal(pr.TargetAmount, 6); err == nil && t > 0 {
		usdtPtr = &t
		r := float64(g.AmountNGNKobo) / 100 / (float64(t) / 1e6)
		ratePtr = &r
	} else if r, err := strconv.ParseFloat(string(pr.Rate), 64); err == nil && r > 0 {
		ratePtr = &r
	}
	err = store.InTx(ctx, s.DB, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE gigs SET client_name=$2, client_email=$3, pay_currency=$4, pay_in_method=$5,
			busha_payment_request_id=$6, payment_details=$7, amount_usdt_micro=$8, rate_ngn_per_usdt=$9, updated_at=now()
			WHERE id=$1 AND status=$10`, g.ID, in.ClientName, in.ClientEmail, in.Currency, req.PayIn.Type, pr.ID,
			[]byte(pr.PayIn), usdtPtr, ratePtr, StatusAwaitingPayment); err != nil {
			return err
		}
		return store.Audit(ctx, tx, g.ID, g.WorkerID, "client", "gig.checkout_started", "", "",
			map[string]any{"busha_payment_request_id": pr.ID, "currency": in.Currency, "busha_mode": s.Busha.Mode()})
	})
	if err != nil {
		return nil, err
	}
	return s.PublicGig(ctx, gigID, token)
}

func newToken() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func splitName(n string) (string, string) {
	parts := strings.Fields(n)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], parts[0]
	}
	return parts[0], strings.Join(parts[1:], " ")
}

func def(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
