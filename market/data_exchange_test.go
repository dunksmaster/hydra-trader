package market

import "testing"

func TestNormalizeKlineExchange(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"bitget", "bitget"},
		{"Bitget", "bitget"},
		{"hyperliquid", "hyperliquid"},
		{"", "binance"},
		{"unknown", "binance"},
	}
	for _, tt := range tests {
		if got := NormalizeKlineExchange(tt.in); got != tt.want {
			t.Fatalf("NormalizeKlineExchange(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
