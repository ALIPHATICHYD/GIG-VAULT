package gigs

import (
	"fmt"
	"math/big"
	"strings"
)

// ParseDecimal converts "123.45" into integer minor units with the given decimals.
func ParseDecimal(s string, decimals int) (int64, error) {
	s = strings.TrimSpace(s)
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	r.Mul(r, scale)
	// round half up
	n := new(big.Int).Quo(r.Num(), r.Denom())
	rem := new(big.Rat).Sub(r, new(big.Rat).SetInt(n))
	if rem.Cmp(big.NewRat(1, 2)) >= 0 {
		n.Add(n, big.NewInt(1))
	}
	if !n.IsInt64() {
		return 0, fmt.Errorf("amount out of range")
	}
	return n.Int64(), nil
}

// FormatMinor renders minor units back to a decimal string.
func FormatMinor(v int64, decimals int) string {
	r := new(big.Rat).SetFrac(big.NewInt(v), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil))
	return r.FloatString(decimals)
}

// bps returns amount * bps / 10000, rounded down (the remainder stays in escrow).
func bps(amount int64, b int) int64 { return amount * int64(b) / 10000 }

// usdtToKobo converts USDT micro-units to NGN kobo at rate (NGN per USDT).
func usdtToKobo(micro int64, rate float64) int64 {
	if rate <= 0 {
		return 0
	}
	return int64(float64(micro) / 1e6 * rate * 100)
}
