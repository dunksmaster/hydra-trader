package trader

import (
	"nofx/kernel"
	"testing"
)

func TestApplyDefaultProtectivePricesFillsZeros(t *testing.T) {
	d := &kernel.Decision{Symbol: "xyz:NBIS", Action: "open_long"}
	applyDefaultProtectivePrices(d, 271.23)
	if d.StopLoss <= 0 || d.StopLoss >= 271.23 {
		t.Fatalf("long stop = %v, want below entry", d.StopLoss)
	}
	if d.TakeProfit <= 271.23 {
		t.Fatalf("long take = %v, want above entry", d.TakeProfit)
	}
}

func TestApplyDefaultProtectivePricesShort(t *testing.T) {
	d := &kernel.Decision{Symbol: "xyz:CXMT", Action: "open_short"}
	applyDefaultProtectivePrices(d, 7.965)
	if d.StopLoss <= 7.965 {
		t.Fatalf("short stop = %v, want above entry", d.StopLoss)
	}
	if d.TakeProfit <= 0 || d.TakeProfit >= 7.965 {
		t.Fatalf("short take = %v, want below entry", d.TakeProfit)
	}
}

func TestPlaceProtectiveOrdersRejectsZeroStop(t *testing.T) {
	at := &AutoTrader{}
	if err := at.placeProtectiveOrders("xyz:NBIS", "LONG", 0.79, 0, 0); err == nil {
		t.Fatal("expected refusal to send stop_loss=0")
	}
}

func TestExampleStopTakeProfitNeverZero(t *testing.T) {
	// kernel helper is tested via prompt; this guards the trader default percents.
	if defaultOpenStopPct <= 0 || defaultOpenTakePct <= 0 {
		t.Fatal("default protective percents must be positive")
	}
}

func TestCurrentAccountEquityPrefersLiveEquity(t *testing.T) {
	balance := map[string]interface{}{
		"total_equity":       54.0,
		"totalWalletBalance": 61.0,
	}
	if got := currentAccountEquity(balance, 40); got != 54 {
		t.Fatalf("equity = %.2f, want current total equity 54", got)
	}
}

func TestCurrentAccountEquityFallbacks(t *testing.T) {
	if got := currentAccountEquity(map[string]interface{}{"totalWalletBalance": 61.0}, 40); got != 61 {
		t.Fatalf("wallet fallback = %.2f, want 61", got)
	}
	if got := currentAccountEquity(map[string]interface{}{}, 40); got != 40 {
		t.Fatalf("available fallback = %.2f, want 40", got)
	}
}
