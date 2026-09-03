package bitget

import "testing"

func TestReconcileBitgetOrderActionKeepsOpen(t *testing.T) {
	got := reconcileBitgetOrderAction("t1", nil, "BTCUSDT", "sell", "open_short")
	if got != "open_short" {
		t.Fatalf("action = %q, want open_short", got)
	}
}

func TestReconcileBitgetOrderActionKeepsClose(t *testing.T) {
	got := reconcileBitgetOrderAction("t1", nil, "BTCUSDT", "sell", "close_long")
	if got != "close_long" {
		t.Fatalf("action = %q, want close_long", got)
	}
}
