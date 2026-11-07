package busha

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSignatureRoundTrip(t *testing.T) {
	body := []byte(`{"event":"payment_request.completed","data":{"reference":"GIG_1"}}`)
	sig := Sign(body, "secret")
	if !VerifySignature(body, sig, "secret") {
		t.Fatal("valid signature rejected")
	}
	if VerifySignature(body, sig, "wrong") {
		t.Fatal("wrong secret accepted")
	}
	if VerifySignature(append(body, ' '), sig, "secret") {
		t.Fatal("tampered body accepted")
	}
	if VerifySignature(body, "not-base64!!", "secret") {
		t.Fatal("garbage signature accepted")
	}
}

func TestWebhookPayloadsFromTestdata(t *testing.T) {
	cases := []struct {
		file      string
		wantEvent string
		wantID    string
		wantRef   string
	}{
		{"payment_request_completed.json", "payment_request.completed", "PAYR_dGK3Xv7kCswk", "GIG_demo001"},
		{"deposit_success.json", "deposit.success", "TRF_LJB2GQb55Cs98LbpqgRGx", "GIG_demo002"},
		{"transfer_completed.json", "transfer.completed", "TRF_0OF0pBoXllc0", "PAY_cashout001"},
		{"transfer_funds_delivered.json", "transfer.funds_delivered", "TRF_BuIVXa89aFC3", "PAY_cashout001"},
		{"transfer_failed.json", "transfer.failed", "TRF_99FailedExample", "PAY_cashout002"},
		{"customer_verification_active.json", "customer.verification.active", "CUS_Ikdb49NLsnlYU", ""},
		{"customer_verification_rejected.json", "customer.verification.rejected", "CUS_Ikdb49NLsnlYU", ""},
	}
	secret := "whsec_test_sandbox_secret"
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", tc.file))
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			sig := Sign(raw, secret)
			if !VerifySignature(raw, sig, secret) {
				t.Fatalf("signature check failed for %s", tc.file)
			}
			var ev Event
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Fatalf("unmarshal Event: %v", err)
			}
			if ev.Event != tc.wantEvent {
				t.Fatalf("got event %q, want %q", ev.Event, tc.wantEvent)
			}
			var d EventData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				t.Fatalf("unmarshal EventData: %v", err)
			}
			if d.ID != tc.wantID {
				t.Fatalf("got id %q, want %q", d.ID, tc.wantID)
			}
			if d.Reference != tc.wantRef {
				t.Fatalf("got reference %q, want %q", d.Reference, tc.wantRef)
			}
		})
	}
}

func TestClientAuthHeadersAndPayloads(t *testing.T) {
	var lastAuth, lastPubKey, lastProfile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		lastPubKey = r.Header.Get("X-BU-PUBLIC-KEY")
		lastProfile = r.Header.Get("X-BU-PROFILE-ID")
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":"OBJ_1","status":"pending","source_amount":"32.851512","target_amount":"32.851512"}}`))
	}))
	defer srv.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewClient(srv.URL, "sec_live_123", "", log).WithPublicKey("pub_live_456")

	// Payment Requests must use X-BU-PUBLIC-KEY.
	_, err := c.CreatePaymentRequest(context.Background(), PaymentRequestInput{
		QuoteCurrency:  "NGN",
		QuoteAmount:    "50000",
		SourceCurrency: "USDT",
		TargetCurrency: "USDT",
		Reference:      "GIG_1",
	})
	if err != nil {
		t.Fatalf("CreatePaymentRequest: %v", err)
	}
	if lastPubKey != "pub_live_456" || lastAuth != "" {
		t.Fatalf("expected X-BU-PUBLIC-KEY=pub_live_456 and empty Authorization, got pub=%q auth=%q", lastPubKey, lastAuth)
	}
	if lastProfile != "" {
		t.Fatalf("expected empty X-BU-PROFILE-ID when not configured, got %q", lastProfile)
	}

	// Quotes and Transfers must use Authorization: Bearer <secretKey>.
	_, err = c.CreateQuote(context.Background(), QuoteInput{
		QuoteCurrency:  "NGN",
		QuoteAmount:    "50000",
		SourceCurrency: "USDT",
		TargetCurrency: "USDT",
		PayIn:          &PayIn{Type: "address", Network: "TRX"},
		PayOut:         &PayOut{Type: "balance"},
	})
	if err != nil {
		t.Fatalf("CreateQuote: %v", err)
	}
	if lastAuth != "Bearer sec_live_123" || lastPubKey != "" {
		t.Fatalf("expected Authorization='Bearer sec_live_123' and empty X-BU-PUBLIC-KEY, got auth=%q pub=%q", lastAuth, lastPubKey)
	}
}
