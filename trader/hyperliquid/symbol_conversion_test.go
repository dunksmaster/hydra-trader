package hyperliquid

import "testing"

func TestConvertSymbolToHyperliquidXYZAliases(t *testing.T) {
	cases := map[string]string{
		"SAMSUNG-USDC":  "xyz:SMSN",
		"SK-HYNIX-USDC": "xyz:SKHX",
		"TSLAUSDT":      "xyz:TSLA",
		"xyz:SMSN":      "xyz:SMSN",
		"HYPEUSDT":      "HYPE",
	}
	for input, want := range cases {
		if got := convertSymbolToHyperliquid(input); got != want {
			t.Fatalf("convertSymbolToHyperliquid(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSameHyperliquidSymbolMatchesBareTicker(t *testing.T) {
	if !sameHyperliquidSymbol("BTCUSDT", "BTC") {
		t.Fatal("BTCUSDT must match AI shorthand BTC")
	}
	if !sameHyperliquidSymbol("SOLUSDT", "SOL", "SOLUSDT") {
		t.Fatal("SOLUSDT must match SOL")
	}
	if sameHyperliquidSymbol("BTCUSDT", "ETH") {
		t.Fatal("BTCUSDT must not match ETH")
	}
}
