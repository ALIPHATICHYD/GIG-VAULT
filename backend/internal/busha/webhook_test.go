package busha

import "testing"

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
