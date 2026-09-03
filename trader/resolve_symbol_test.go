package trader

import "testing"

func TestResolveLiveSymbolNormalizesBareTicker(t *testing.T) {
	at := &AutoTrader{}
	if got := at.resolveLiveSymbol("BTC"); got != "BTCUSDT" {
		t.Fatalf("BTC → %q, want BTCUSDT", got)
	}
	if got := at.resolveLiveSymbol("SOLUSDT"); got != "SOLUSDT" {
		t.Fatalf("SOLUSDT → %q", got)
	}
}
