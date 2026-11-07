package busha

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func loadEnvForTest() {
	for _, p := range []string{"../../../.env.local", "../../.env.local", "../../.env", ".env.local", ".env"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if _, set := os.LookupEnv(k); !set && v != "" {
				_ = os.Setenv(k, v)
			}
		}
	}
}

// TestLiveSandbox exercises every Busha API call against the live sandbox when
// BUSHA_SECRET_KEY is set in the environment or .env.local (skipped otherwise).
func TestLiveSandbox(t *testing.T) {
	loadEnvForTest()
	sec := os.Getenv("BUSHA_SECRET_KEY")
	if sec == "" {
		t.Skip("BUSHA_SECRET_KEY not set; skipping live sandbox smoke test")
	}
	baseURL := os.Getenv("BUSHA_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.sandbox.busha.so"
	}
	pub := os.Getenv("BUSHA_PUBLIC_KEY")

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c := NewClient(baseURL, sec, "", log).WithPublicKey(pub)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("Balances", func(t *testing.T) {
		bals, err := c.ListBalances(ctx)
		if err != nil {
			t.Errorf("ListBalances: %v", err)
			return
		}
		for _, b := range bals {
			t.Logf("Balance: currency=%s available=%s total=%s", b.Currency, b.Available.Amount, b.Total.Amount)
		}
	})

	t.Run("Banks", func(t *testing.T) {
		banks, err := c.ListBanks(ctx)
		if err != nil {
			t.Errorf("ListBanks: %v", err)
			return
		}
		t.Logf("Banks response bytes: %d", len(banks))
	})

	var recipientID string
	t.Run("ResolveBankAccount_And_CreateRecipient", func(t *testing.T) {
		acct, err := c.ResolveBankAccount(ctx, "000013", "0123456789")
		if err != nil {
			t.Errorf("ResolveBankAccount: %v", err)
			return
		}
		t.Logf("Resolved account: %+v", acct)

		rcp, err := c.CreateRecipient(ctx, RecipientInput{
			Type:          "ngn_bank",
			Currency:      "NGN",
			CountryCode:   "NG",
			AccountName:   acct.AccountName,
			AccountNumber: acct.AccountNumber,
			BankCode:      acct.BankCode,
			BankName:      acct.BankName,
			Email:         "chidinma@example.com",
		})
		if err != nil {
			t.Errorf("CreateRecipient: %v", err)
			return
		}
		recipientID = rcp.ID
		t.Logf("Created recipient: %+v", rcp)
	})

	var discoveredProfileID string
	t.Run("DepositQuoteAndTransfer_USDT", func(t *testing.T) {
		q, err := c.CreateQuote(ctx, QuoteInput{
			QuoteCurrency:  "NGN",
			QuoteAmount:    "50000",
			SourceCurrency: "USDT",
			TargetCurrency: "USDT",
			PayIn:          &PayIn{Type: "address", Network: "TRX"},
			PayOut:         &PayOut{Type: "balance"},
		})
		if err != nil {
			t.Errorf("CreateQuote (USDT deposit with quote_currency NGN): %v", err)
			return
		}
		discoveredProfileID = q.ProfileID
		t.Logf("Quote created: id=%s profile_id=%s source_amount=%s target_amount=%s", q.ID, q.ProfileID, q.SourceAmount, q.TargetAmount)

		trf, err := c.CreateTransfer(ctx, q.ID, "GIG_smoke_usdt_"+q.ID)
		if err != nil {
			t.Errorf("CreateTransfer: %v", err)
			return
		}
		if discoveredProfileID == "" {
			discoveredProfileID = trf.ProfileID
		}
		t.Logf("Transfer created: id=%s profile_id=%s status=%s pay_in=%s", trf.ID, trf.ProfileID, trf.Status, string(trf.PayIn))

		got, err := c.GetTransfer(ctx, trf.ID)
		if err != nil {
			t.Errorf("GetTransfer: %v", err)
			return
		}
		t.Logf("GetTransfer: id=%s status=%s", got.ID, got.Status)
	})

	t.Run("DepositQuoteAndTransfer_NGN", func(t *testing.T) {
		q, err := c.CreateQuote(ctx, QuoteInput{
			SourceCurrency: "NGN",
			TargetCurrency: "USDT",
			SourceAmount:   "50000",
			PayIn:          &PayIn{Type: "temporary_bank_account"},
			PayOut:         &PayOut{Type: "balance"},
		})
		if err != nil {
			t.Logf("CreateQuote (NGN deposit): %v", err)
			return
		}
		t.Logf("NGN Deposit Quote: id=%s source_amount=%s target_amount=%s pay_in=%s", q.ID, q.SourceAmount, q.TargetAmount, string(q.PayIn))

		trf, err := c.CreateTransfer(ctx, q.ID, "GIG_smoke_ngn_"+q.ID)
		if err != nil {
			t.Logf("CreateTransfer (NGN deposit): %v", err)
			return
		}
		t.Logf("NGN Deposit Transfer: id=%s status=%s pay_in=%s", trf.ID, trf.Status, string(trf.PayIn))
	})

	if recipientID != "" {
		t.Run("CashoutQuoteAndTransfer_USDT_to_NGN", func(t *testing.T) {
			payoutRef := "PAY_smoke_" + time.Now().Format("150405")
			q, err := c.CreateQuote(ctx, QuoteInput{
				SourceCurrency: "USDT",
				TargetCurrency: "NGN",
				TargetAmount:   "5000",
				Reference:      payoutRef,
				PayOut:         &PayOut{Type: "bank_transfer", RecipientID: recipientID},
			})
			if err != nil {
				t.Errorf("CreateQuote (Cashout USDT->NGN): %v", err)
				return
			}
			t.Logf("Cashout Quote: id=%s source_amount=%s target_amount=%s rate=%s", q.ID, q.SourceAmount, q.TargetAmount, q.Rate.Rate)

			trf, err := c.CreateTransfer(ctx, q.ID, payoutRef)
			if err != nil {
				t.Errorf("CreateTransfer (Cashout USDT->NGN): %v", err)
				return
			}
			t.Logf("Cashout Transfer: id=%s status=%s source=%s target=%s", trf.ID, trf.Status, trf.SourceAmount, trf.TargetAmount)
		})
	}

	t.Run("CreateCustomer_And_KYCLink", func(t *testing.T) {
		if discoveredProfileID != "" {
			c.profileID = discoveredProfileID
		}
		email := fmt.Sprintf("creator.%d@gigvault.test", time.Now().UnixNano())
		cus, err := c.CreateCustomer(ctx, CustomerInput{
			Email:            email,
			HasAcceptedTerms: true,
			Type:             "individual",
			CountryID:        "NG",
			Phone:            "+2348012345678",
			BirthDate:        "15-05-1998",
			FirstName:        "Chidinma",
			LastName:         "Okafor",
			Address: Address{
				Street:     "12 Admiralty Way",
				City:       "Lagos",
				State:      "Lagos",
				PostalCode: "106104",
				Country:    "NG",
			},
		})
		if err != nil {
			t.Logf("CreateCustomer (profile=%q): %v", c.profileID, err)
			return
		}
		t.Logf("Created customer: %+v", cus)

		link, err := c.KYCLink(ctx, cus.ID)
		if err != nil {
			t.Logf("KYCLink error: %v", err)
			return
		}
		t.Logf("KYC link: %s", link)
	})

	if pub != "" {
		t.Run("PaymentRequest", func(t *testing.T) {
			for _, curr := range []struct{ src, net string }{
				{"NGN", ""},
				{"BTC", "BTC"},
				{"USDT", "TRX"},
			} {
				in := PaymentRequestInput{
					QuoteCurrency:  "NGN",
					QuoteAmount:    "50000",
					SourceCurrency: curr.src,
					TargetCurrency: curr.src,
					Reference:      fmt.Sprintf("GIG_pr_%s_%d", curr.src, time.Now().UnixNano()),
				}
				if curr.net != "" {
					in.PayIn = &PayIn{Type: "address", Network: curr.net}
				} else {
					in.PayIn = &PayIn{Type: "temporary_bank_account"}
				}
				in.AdditionalInfo.Email = "john@busha.so"
				in.AdditionalInfo.PhoneNumber = "+234 8123456789"
				pr, err := c.CreatePaymentRequest(ctx, in)
				if err != nil {
					t.Logf("CreatePaymentRequest (%s): %v", curr.src, err)
					continue
				}
				t.Logf("PaymentRequest (%s) created: id=%s source_amount=%s pay_in=%s", curr.src, pr.ID, pr.SourceAmount, string(pr.PayIn))
				got, err := c.GetPaymentRequest(ctx, pr.ID)
				if err != nil {
					t.Errorf("GetPaymentRequest: %v", err)
					return
				}
				t.Logf("GetPaymentRequest: id=%s status=%s", got.ID, got.Status)
				return
			}
		})
	}
}
