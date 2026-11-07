package gigs

import (
	"encoding/json"
	"testing"

	"github.com/aliphatichyd/gig-vault/backend/internal/busha"
)

func TestParseDecimal(t *testing.T) {
	cases := []struct {
		in   string
		dec  int
		want int64
	}{{"120000", 2, 12000000}, {"75.123456", 6, 75123456}, {"0.0000005", 6, 1}, {"1.005", 2, 101}}
	for _, c := range cases {
		got, err := ParseDecimal(c.in, c.dec)
		if err != nil || got != c.want {
			t.Errorf("ParseDecimal(%q,%d)=%d,%v want %d", c.in, c.dec, got, err, c.want)
		}
	}
	if _, err := ParseDecimal("abc", 2); err == nil {
		t.Error("expected error for abc")
	}
}

func TestBpsLeavesRemainderInEscrow(t *testing.T) {
	if got := bps(75_000_001, 2500); got != 18_750_000 {
		t.Fatalf("got %d", got)
	}
}

func TestNormalizePayInFlattensRecipientDetails(t *testing.T) {
	raw := json.RawMessage(`{"expires_at":"2026-11-07T12:55:11Z","recipient_details":{"account_name":"BDL  Michael Scott","account_number":"0325259470","bank_code":"090614","bank_name":"Aella Microfinance Bank"},"type":"temporary_bank_account"}`)
	norm := normalizePayIn(raw)
	var m map[string]any
	if err := json.Unmarshal(norm, &m); err != nil {
		t.Fatalf("unmarshal normalized pay_in: %v", err)
	}
	if m["account_number"] != "0325259470" || m["bank_name"] != "Aella Microfinance Bank" || m["account_name"] != "BDL  Michael Scott" {
		t.Fatalf("expected flattened bank details, got %+v", m)
	}
}

func TestIsExpiredQuote(t *testing.T) {
	err := &busha.Error{Status: 422, Body: `{"error":{"name":"bad_request","message":"Quote has expired"}}`}
	if !isExpiredQuote(err) {
		t.Fatal("expected isExpiredQuote=true for expired quote error")
	}
	other := &busha.Error{Status: 400, Body: `{"error":{"name":"bad_request","message":"Insufficient balance"}}`}
	if isExpiredQuote(other) {
		t.Fatal("expected isExpiredQuote=false for non-expiry error")
	}
}
