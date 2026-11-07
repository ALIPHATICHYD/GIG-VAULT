package busha

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
)

// SignatureHeader carries base64(HMAC-SHA256(raw body, webhook secret)).
const SignatureHeader = "X-BU-SIGNATURE"

func Sign(body []byte, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// VerifySignature compares in constant time.
func VerifySignature(body []byte, signature, secret string) bool {
	got, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hmac.Equal(got, m.Sum(nil))
}

// Event is the webhook envelope: {"business_id", "event", "data": {...}}.
type Event struct {
	BusinessID string          `json:"business_id"`
	Event      string          `json:"event"`
	Data       json.RawMessage `json:"data"`
}

// EventData holds the fields we read across event types.
type EventData struct {
	ID           string `json:"id"`
	CustomerID   string `json:"customer_id"`
	Reference    string `json:"reference"`
	Status       string `json:"status"`
	QuoteID      string `json:"quote_id"`
	TargetAmount string `json:"target_amount"`
}
