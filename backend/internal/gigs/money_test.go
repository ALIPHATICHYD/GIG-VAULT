package gigs

import "testing"

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

func TestUSDTToKoboRoundsToWholeNaira(t *testing.T) {
	// 500,000 NGN settled as 361.93 USDT; 20% upfront is 72.386 USDT.
	rate := 500000.0 / 361.93
	if got := usdtToKobo(72386000, rate); got != 10000000 {
		t.Fatalf("got %d kobo, want 10000000 (N100,000)", got)
	}
	if got := usdtToKobo(0, rate); got != 0 {
		t.Fatalf("zero: got %d", got)
	}
}
