package auth

import (
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	s := Signer{Secret: []byte("k")}
	tok := s.Issue("USR_1", Client, time.Hour)
	got, err := s.Parse(tok)
	if err != nil || got.UserID != "USR_1" || got.Role != Client {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := (Signer{Secret: []byte("other")}).Parse(tok); err == nil {
		t.Fatal("token accepted with wrong secret")
	}
	if _, err := s.Parse(s.Issue("USR_1", Client, -time.Second)); err == nil {
		t.Fatal("expired token accepted")
	}
	// Changing the role must break the signature.
	forged := "USR_1.creator" + tok[len("USR_1.client"):]
	if _, err := s.Parse(forged); err == nil {
		t.Fatal("forged role accepted")
	}
}
