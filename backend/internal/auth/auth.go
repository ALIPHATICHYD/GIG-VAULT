// Package auth issues and checks session tokens for creators and clients.
//
// Tokens are "<user id>.<role>.<expiry unix>.<HMAC-SHA256 signature>", signed
// with AUTH_SECRET. Small, stateless and enough for a hackathon; swap for
// JWT/OAuth later without touching handlers.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	Creator Role = "creator"
	Client  Role = "client"
)

type Session struct {
	UserID string
	Role   Role
}

type Signer struct{ Secret []byte }

func (s Signer) Issue(userID string, role Role, ttl time.Duration) string {
	payload := fmt.Sprintf("%s.%s.%d", userID, role, time.Now().Add(ttl).Unix())
	return payload + "." + s.sign(payload)
}

func (s Signer) Parse(token string) (Session, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 {
		return Session{}, errors.New("malformed token")
	}
	payload := strings.Join(parts[:3], ".")
	if !hmac.Equal([]byte(parts[3]), []byte(s.sign(payload))) {
		return Session{}, errors.New("bad signature")
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return Session{}, errors.New("token expired")
	}
	return Session{UserID: parts[0], Role: Role(parts[1])}, nil
}

func (s Signer) sign(payload string) string {
	m := hmac.New(sha256.New, s.Secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

type ctxKey struct{}

func FromContext(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(ctxKey{}).(Session)
	return s, ok
}

// Require returns middleware that only lets a valid session with the given role through.
// The onFail callback writes the error response.
func (s Signer) Require(role Role, onFail func(w http.ResponseWriter, status int, code, msg string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				onFail(w, http.StatusUnauthorized, "unauthenticated", "log in first")
				return
			}
			sess, err := s.Parse(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				onFail(w, http.StatusUnauthorized, "unauthenticated", "your session has expired, log in again")
				return
			}
			if role != "" && sess.Role != role {
				onFail(w, http.StatusForbidden, "wrong_role", "this screen is for "+string(role)+"s")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
		})
	}
}
