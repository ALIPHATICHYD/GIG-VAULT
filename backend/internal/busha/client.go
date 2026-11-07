// Package busha is a thin client for the Busha Business API.
//
// Docs: https://docs.busha.io  ·  OpenAPI: https://github.com/bushaHQ/openapi
// Auth: "Authorization: Bearer <secret key>" (server-side only).
// Acting for a customer: "X-BU-PROFILE-ID: <customer id>".
package busha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// API is what the rest of the app depends on, so a mock can stand in when the
// sandbox is unreachable. Every mock response is labelled "mock": true.
type API interface {
	CreatePaymentRequest(ctx context.Context, in PaymentRequestInput) (*PaymentRequest, error)
	GetPaymentRequest(ctx context.Context, id string) (*PaymentRequest, error)
	CreateQuote(ctx context.Context, in QuoteInput) (*Quote, error)
	CreateTransfer(ctx context.Context, quoteID, reference string) (*Transfer, error)
	GetTransfer(ctx context.Context, id string) (*Transfer, error)
	ListBalances(ctx context.Context) ([]Balance, error)
	ListBanks(ctx context.Context) (json.RawMessage, error)
	ResolveBankAccount(ctx context.Context, bankCode, accountNumber string) (*ResolvedAccount, error)
	CreateRecipient(ctx context.Context, in RecipientInput) (*Recipient, error)
	CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, error)
	KYCLink(ctx context.Context, customerID string) (string, error)
	Mode() string
}

// ---- payloads (fields we use; Busha returns more) ----

type Amount struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type PayIn struct {
	Type    string `json:"type"`              // temporary_bank_account | address
	Network string `json:"network,omitempty"` // e.g. TRX for USDT
}

type PayOut struct {
	Type        string `json:"type"`                   // bank_transfer | internal | address
	RecipientID string `json:"recipient_id,omitempty"` // for bank_transfer
	Address     string `json:"address,omitempty"`      // for internal: destination profile id
	Network     string `json:"network,omitempty"`
}

type PaymentRequestInput struct {
	SourceCurrency string `json:"source_currency"` // what the client pays in: NGN or USDT
	TargetCurrency string `json:"target_currency"` // what we hold: USDT
	SourceAmount   string `json:"source_amount,omitempty"`
	QuoteCurrency  string `json:"quote_currency,omitempty"` // price in NGN, pay in USDT
	QuoteAmount    string `json:"quote_amount,omitempty"`
	PayIn          *PayIn `json:"pay_in,omitempty"`
	Reference      string `json:"reference"`
	AdditionalInfo struct {
		Email       string `json:"email"`
		Name        string `json:"name,omitempty"`
		PhoneNumber string `json:"phone_number,omitempty"`
	} `json:"additional_info"`
}

type PaymentRequest struct {
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	Reference    string          `json:"reference"`
	SourceAmount string          `json:"source_amount"`
	TargetAmount string          `json:"target_amount"`
	Rate         json.RawMessage `json:"rate,omitempty"`
	PayIn        json.RawMessage `json:"pay_in,omitempty"`
	Mock         bool            `json:"mock,omitempty"`
}

type QuoteInput struct {
	SourceCurrency string  `json:"source_currency"`
	TargetCurrency string  `json:"target_currency"`
	SourceAmount   string  `json:"source_amount,omitempty"`
	TargetAmount   string  `json:"target_amount,omitempty"`
	QuoteCurrency  string  `json:"quote_currency,omitempty"`
	QuoteAmount    string  `json:"quote_amount,omitempty"`
	Reference      string  `json:"reference,omitempty"`
	PayIn          *PayIn  `json:"pay_in,omitempty"`
	PayOut         *PayOut `json:"pay_out,omitempty"`
}

type Rate struct {
	Rate    string `json:"rate"`
	Side    string `json:"side"`
	Product string `json:"product"`
}

type Quote struct {
	ID             string          `json:"id"`
	ProfileID      string          `json:"profile_id,omitempty"`
	SourceCurrency string          `json:"source_currency"`
	TargetCurrency string          `json:"target_currency"`
	SourceAmount   string          `json:"source_amount"`
	TargetAmount   string          `json:"target_amount"`
	Rate           Rate            `json:"rate"`
	PayIn          json.RawMessage `json:"pay_in,omitempty"`
	PayOut         json.RawMessage `json:"pay_out,omitempty"`
	Status         string          `json:"status"`
	ExpiresAt      time.Time       `json:"expires_at"`
	Mock           bool            `json:"mock,omitempty"`
}

type Transfer struct {
	ID           string          `json:"id"`
	ProfileID    string          `json:"profile_id,omitempty"`
	QuoteID      string          `json:"quote_id"`
	Reference    string          `json:"reference,omitempty"`
	Status       string          `json:"status"`
	SourceAmount string          `json:"source_amount"`
	TargetAmount string          `json:"target_amount"`
	PayIn        json.RawMessage `json:"pay_in,omitempty"`
	Mock         bool            `json:"mock,omitempty"`
}

type Balance struct {
	ID        string `json:"id"`
	Currency  string `json:"currency"`
	Type      string `json:"type"`
	Available Amount `json:"available"`
	Pending   Amount `json:"pending"`
	Total     Amount `json:"total"`
}

type ResolvedAccount struct {
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
	BankCode      string `json:"bank_code"`
	BankName      string `json:"bank_name"`
}

// RecipientInput is the 2025-07-11 recipient schema for a Nigerian bank account.
type RecipientInput struct {
	Type          string `json:"type"` // ngn_bank
	Currency      string `json:"currency"`
	CountryCode   string `json:"country_code"`
	AccountName   string `json:"account_name"`
	AccountNumber string `json:"account_number"`
	BankCode      string `json:"bank_code"`
	BankName      string `json:"bank_name,omitempty"`
	Email         string `json:"email,omitempty"`
}

type Recipient struct {
	ID   string `json:"id"`
	Mock bool   `json:"mock,omitempty"`
}

type Address struct {
	Street       string `json:"street,omitempty"`
	AddressLine1 string `json:"address_line_1,omitempty"`
	City         string `json:"city"`
	State        string `json:"state"`
	PostalCode   string `json:"postal_code,omitempty"`
	Country      string `json:"country,omitempty"`
	CountryID    string `json:"country_id,omitempty"`
}

// CustomerInput creates an individual Busha customer (a profile we can act for
// with X-BU-PROFILE-ID). KYC itself happens on Busha's hosted KYC link.
type CustomerInput struct {
	Email            string  `json:"email"`
	HasAcceptedTerms bool    `json:"has_accepted_terms"`
	Type             string  `json:"type"` // individual
	CountryID        string  `json:"country_id"`
	Phone            string  `json:"phone"`
	BirthDate        string  `json:"birth_date"` // DD-MM-YYYY
	FirstName        string  `json:"first_name"`
	LastName         string  `json:"last_name"`
	Address          Address `json:"address"`
}

type Customer struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Mock   bool   `json:"mock,omitempty"`
}

// Error is a non-2xx response from Busha. RequestID helps Busha support trace it.
type Error struct {
	Status    int
	Body      string
	RequestID string
}

func (e *Error) Error() string {
	return fmt.Sprintf("busha: http %d (request %s): %s", e.Status, e.RequestID, e.Body)
}

// ---- live client ----

type Client struct {
	baseURL   string
	secretKey string
	publicKey string
	profileID string
	http      *http.Client
	log       *slog.Logger
}

func NewClient(baseURL, secretKey, profileID string, log *slog.Logger) *Client {
	return &Client{baseURL: baseURL, secretKey: secretKey, profileID: profileID,
		http: &http.Client{Timeout: 20 * time.Second}, log: log}
}

func (c *Client) WithPublicKey(publicKey string) *Client {
	c.publicKey = publicKey
	return c
}

func (c *Client) Mode() string { return "live" }

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	return c.doWithAuth(ctx, method, path, body, out, false)
}

func (c *Client) doPublic(ctx context.Context, method, path string, body any, out any) error {
	return c.doWithAuth(ctx, method, path, body, out, true)
}

func (c *Client) doWithAuth(ctx context.Context, method, path string, body any, out any, usePublicKey bool) error {
	var payloadBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payloadBytes = b
	}
	// Retry only on network errors and 429/5xx, and only for idempotent GETs,
	// so we never create two quotes or transfers by accident.
	attempts := 1
	if method == http.MethodGet {
		attempts = 3
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i*i) * 300 * time.Millisecond)
		}
		var rdr io.Reader
		if payloadBytes != nil {
			rdr = bytes.NewReader(payloadBytes)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
		if err != nil {
			return err
		}
		if usePublicKey && c.publicKey != "" {
			req.Header.Set("X-BU-PUBLIC-KEY", c.publicKey)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.secretKey)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if c.profileID != "" {
			req.Header.Set("X-BU-PROFILE-ID", c.profileID)
		}
		start := time.Now()
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			c.log.Warn("busha request failed", "method", method, "path", path, "err", err)
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		reqID := resp.Header.Get("X-Request-Id")
		c.log.Info("busha", "method", method, "path", path, "status", resp.StatusCode, "request_id", reqID, "ms", time.Since(start).Milliseconds())
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = &Error{Status: resp.StatusCode, Body: string(raw), RequestID: reqID}
			continue
		}
		if resp.StatusCode >= 300 {
			return &Error{Status: resp.StatusCode, Body: string(raw), RequestID: reqID}
		}
		if out == nil {
			return nil
		}
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("busha: decode envelope: %w", err)
		}
		if raw, ok := out.(*json.RawMessage); ok {
			*raw = env.Data
			return nil
		}
		return json.Unmarshal(env.Data, out)
	}
	return lastErr
}

func (c *Client) CreatePaymentRequest(ctx context.Context, in PaymentRequestInput) (*PaymentRequest, error) {
	var out PaymentRequest
	return &out, c.doPublic(ctx, http.MethodPost, "/v1/payments/requests", in, &out)
}

func (c *Client) GetPaymentRequest(ctx context.Context, id string) (*PaymentRequest, error) {
	var out PaymentRequest
	return &out, c.doPublic(ctx, http.MethodGet, "/v1/payments/requests/"+id, nil, &out)
}

func (c *Client) CreateQuote(ctx context.Context, in QuoteInput) (*Quote, error) {
	var out Quote
	return &out, c.do(ctx, http.MethodPost, "/v1/quotes", in, &out)
}

func (c *Client) CreateTransfer(ctx context.Context, quoteID, reference string) (*Transfer, error) {
	var out Transfer
	return &out, c.do(ctx, http.MethodPost, "/v1/transfers", map[string]string{"quote_id": quoteID, "reference": reference}, &out)
}

func (c *Client) GetTransfer(ctx context.Context, id string) (*Transfer, error) {
	var out Transfer
	return &out, c.do(ctx, http.MethodGet, "/v1/transfers/"+id, nil, &out)
}

func (c *Client) ListBalances(ctx context.Context) ([]Balance, error) {
	var out []Balance
	return out, c.do(ctx, http.MethodGet, "/v1/balances", nil, &out)
}

func (c *Client) ListBanks(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	return out, c.do(ctx, http.MethodGet, "/v1/banks?country_id=NG", nil, &out)
}

func (c *Client) ResolveBankAccount(ctx context.Context, bankCode, accountNumber string) (*ResolvedAccount, error) {
	var out ResolvedAccount
	body := map[string]string{"currency_id": "NGN", "country_id": "NG", "channel": "bank_transfer",
		"bank_code": bankCode, "account_number": accountNumber}
	return &out, c.do(ctx, http.MethodPost, "/v1/recipients/resolve-bank-account", body, &out)
}

func (c *Client) CreateRecipient(ctx context.Context, in RecipientInput) (*Recipient, error) {
	var out Recipient
	return &out, c.do(ctx, http.MethodPost, "/v1/recipients", in, &out)
}

func (c *Client) CreateCustomer(ctx context.Context, in CustomerInput) (*Customer, error) {
	var out Customer
	return &out, c.do(ctx, http.MethodPost, "/v1/customers", in, &out)
}

// KYCLink returns Busha's hosted KYC URL for a customer.
func (c *Client) KYCLink(ctx context.Context, customerID string) (string, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodPost, "/v1/customers/"+customerID+"/kyc-link", map[string]any{}, &out); err != nil {
		return "", err
	}
	for _, k := range []string{"url", "link", "kyc_link"} {
		if v, ok := out[k].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("busha: kyc-link response had no url")
}
