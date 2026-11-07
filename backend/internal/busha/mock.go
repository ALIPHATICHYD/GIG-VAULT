package busha

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"sync"
	"time"
)

// Mock is an offline stand-in for the Busha API. It is only for when the
// sandbox is unreachable; every object it returns has "mock": true and the
// UI shows a MOCK badge, so we never pass it off as the real thing.
type Mock struct {
	RateNGN int64 // NGN per USDT
	mu      sync.Mutex
	n       int
	quotes  map[string]*Quote
}

func NewMock(rateNGN int64) *Mock { return &Mock{RateNGN: rateNGN, quotes: map[string]*Quote{}} }

func (m *Mock) Mode() string { return "mock" }

func (m *Mock) id(prefix string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	return fmt.Sprintf("%s_mock%06d", prefix, m.n)
}

func (m *Mock) convert(src, dst, amount string) string {
	a, ok := new(big.Rat).SetString(amount)
	if !ok {
		return "0"
	}
	r := new(big.Rat).SetInt64(m.RateNGN)
	switch {
	case src == "NGN" && dst == "USDT":
		return new(big.Rat).Quo(a, r).FloatString(6)
	case src == "USDT" && dst == "NGN":
		return new(big.Rat).Mul(a, r).FloatString(2)
	default:
		return a.FloatString(6)
	}
}

func (m *Mock) CreatePaymentRequest(_ context.Context, in PaymentRequestInput) (*PaymentRequest, error) {
	payIn := map[string]any{"type": "temporary_bank_account", "bank_name": "Busha Sandbox Bank (mock)",
		"account_number": "99" + strconv.FormatInt(time.Now().UnixNano()%100000000, 10), "account_name": "GigVault Escrow",
		"expires_at": time.Now().Add(30 * time.Minute)}
	if in.SourceCurrency == "USDT" {
		payIn = map[string]any{"type": "address", "network": "TRX", "address": "TMockAddressForDemoOnly000000000"}
	}
	pi, _ := json.Marshal(payIn)
	return &PaymentRequest{ID: m.id("PAYR"), Status: "pending", Reference: in.Reference,
		SourceAmount: in.SourceAmount, TargetAmount: m.convert(in.SourceCurrency, in.TargetCurrency, in.SourceAmount),
		PayIn: pi, Mock: true}, nil
}

func (m *Mock) CreateQuote(_ context.Context, in QuoteInput) (*Quote, error) {
	q := &Quote{ID: m.id("QUO"), SourceCurrency: in.SourceCurrency, TargetCurrency: in.TargetCurrency,
		Rate:   Rate{Rate: strconv.FormatInt(m.RateNGN, 10), Side: "sell", Product: "USDTNGN"},
		Status: "pending", ExpiresAt: time.Now().Add(3 * time.Minute), Mock: true}
	if in.SourceAmount != "" {
		q.SourceAmount = in.SourceAmount
		q.TargetAmount = m.convert(in.SourceCurrency, in.TargetCurrency, in.SourceAmount)
	} else {
		q.TargetAmount = in.TargetAmount
		q.SourceAmount = m.convert(in.TargetCurrency, in.SourceCurrency, in.TargetAmount)
	}
	m.mu.Lock()
	m.quotes[q.ID] = q
	m.mu.Unlock()
	return q, nil
}

func (m *Mock) CreateTransfer(_ context.Context, quoteID string) (*Transfer, error) {
	m.mu.Lock()
	q, ok := m.quotes[quoteID]
	m.mu.Unlock()
	if !ok {
		return nil, &Error{Status: 404, Body: `{"message":"quote not found"}`}
	}
	if time.Now().After(q.ExpiresAt) {
		return nil, &Error{Status: 422, Body: `{"message":"quote expired"}`}
	}
	return &Transfer{ID: m.id("TRF"), QuoteID: quoteID, Status: "processing",
		SourceAmount: q.SourceAmount, TargetAmount: q.TargetAmount, Mock: true}, nil
}

func (m *Mock) GetTransfer(_ context.Context, id string) (*Transfer, error) {
	return &Transfer{ID: id, Status: "completed", Mock: true}, nil
}

func (m *Mock) ListBalances(context.Context) ([]Balance, error) {
	return []Balance{{ID: "BAL_mock_usdt", Currency: "USDT", Type: "crypto",
		Available: Amount{"0", "USDT"}, Total: Amount{"0", "USDT"}}}, nil
}

func (m *Mock) ListBanks(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[{"name":"Access Bank","code":"000014"},{"name":"GTBank","code":"000013"},{"name":"Opay","code":"100004"}]`), nil
}

func (m *Mock) ResolveBankAccount(_ context.Context, bankCode, acct string) (*ResolvedAccount, error) {
	return &ResolvedAccount{AccountName: "CHIDINMA OKAFOR (mock)", AccountNumber: acct, BankCode: bankCode, BankName: "Mock Bank"}, nil
}

func (m *Mock) CreateRecipient(context.Context, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(fmt.Sprintf(`{"id":"%s","mock":true}`, m.id("RCP"))), nil
}
