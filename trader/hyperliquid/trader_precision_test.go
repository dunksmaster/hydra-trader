package hyperliquid

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestWireSafeHyperliquidPerpPricePumpLike(t *testing.T) {
	raw := 0.0034567 * aggressiveSellPriceFactor
	safe := WireSafeHyperliquidPerpPrice(raw, 0)
	if !IsValidHyperliquidPerpPrice(safe, 0) {
		t.Fatalf("wire-safe price %v failed validation", safe)
	}
	wire := PriceWireString(safe, 0)
	if parts := strings.Split(wire, "."); len(parts) == 2 && len(parts[1]) > 6 {
		t.Fatalf("wire price %q has >6 decimal places", wire)
	}
}

func TestWireSafeHyperliquidPerpPriceIsIdempotent(t *testing.T) {
	cases := []struct {
		sz    int
		price float64
	}{
		{0, 0.003422133},
		{1, 0.03422133},
		{4, 3.422133},
		{5, 34.22133},
	}
	for _, tc := range cases {
		p := FormatHyperliquidPerpPrice(tc.price, tc.sz)
		safe := WireSafeHyperliquidPerpPrice(p, tc.sz)
		if !IsValidHyperliquidPerpPrice(safe, tc.sz) {
			t.Fatalf("szDecimals=%d safe=%v invalid", tc.sz, safe)
		}
		if WireSafeHyperliquidPerpPrice(safe, tc.sz) != safe {
			t.Fatalf("szDecimals=%d not idempotent: %v", tc.sz, safe)
		}
	}
}

func TestFormatHyperliquidPerpPricePumpLike(t *testing.T) {
	// PUMP-style low price: szDecimals=0 → max 6 decimal places.
	raw := 0.0034567 * aggressiveSellPriceFactor
	got := FormatHyperliquidPerpPrice(raw, 0)
	gotStr := strconv.FormatFloat(got, 'f', -1, 64)
	if parts := strings.Split(gotStr, "."); len(parts) == 2 && len(parts[1]) > 6 {
		t.Fatalf("price %s has >6 decimal places", gotStr)
	}
	if got <= 0 || got >= raw {
		t.Fatalf("rounded price %v should be positive and below raw %v", got, raw)
	}
}

func TestFormatHyperliquidPerpPriceBTCLike(t *testing.T) {
	// BTC szDecimals=5 → max 1 decimal place on price.
	got := FormatHyperliquidPerpPrice(98765.4321, 5)
	gotStr := strconv.FormatFloat(got, 'f', -1, 64)
	if parts := strings.Split(gotStr, "."); len(parts) == 2 && len(parts[1]) > 1 {
		t.Fatalf("price %s has >1 decimal place for szDecimals=5", gotStr)
	}
	if math.Abs(got-98765) > 0.5 {
		t.Fatalf("got %v, want ~98765", got)
	}
}

func TestFormatHyperliquidPerpPriceSigFigs(t *testing.T) {
	got := FormatHyperliquidPerpPrice(1234.56789, 2)
	// szDecimals=2 → max 4 decimals; 5 sig figs on 1234.6 → 1234.6
	if math.Abs(got-1234.6) > 0.01 {
		t.Fatalf("got %v, want 1234.6", got)
	}
}

func TestRoundToSzDecimalsFloor(t *testing.T) {
	ht := &HyperliquidTrader{}
	ht.metaMutex.Lock()
	ht.meta = nil
	ht.metaMutex.Unlock()

	got := ht.roundToSzDecimalsFloor("TEST", 1.23456)
	if got != 1.2345 {
		t.Fatalf("floor qty = %v, want 1.2345 (default szDecimals=4)", got)
	}
}
