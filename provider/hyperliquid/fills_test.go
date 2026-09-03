package hyperliquid

import "testing"

func TestParseFillAction(t *testing.T) {
	tests := []struct {
		name      string
		dir       string
		side      string
		closedPnl float64
		want      string
	}{
		{"Open Long", "Open Long", "B", 0, "open_long"},
		{"Open Short", "Open Short", "A", 0, "open_short"},
		{"Close Long", "Close Long", "A", 0, "close_long"},
		{"Close Short", "Close Short", "B", 0, "close_short"},
		{"fallback close long", "", "SELL", 12.5, "close_long"},
		{"fallback close short", "", "BUY", -3, "close_short"},
		{"fallback open long", "", "B", 0, "open_long"},
		{"fallback open short", "", "S", 0, "open_short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFillAction(tt.dir, tt.side, tt.closedPnl)
			if got != tt.want {
				t.Fatalf("ParseFillAction() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseLeaderFill(t *testing.T) {
	fill, err := ParseLeaderFill("BTC", "Open Long", "B", "50000", "0.01", "0", "0xabc", 123, 1_700_000_000_000)
	if err != nil {
		t.Fatalf("ParseLeaderFill: %v", err)
	}
	if fill.Symbol != "BTCUSDT" || fill.Action != "open_long" || fill.NotionalUSD != 500 {
		t.Fatalf("unexpected fill: %+v", fill)
	}

	xyz, err := ParseLeaderFill("xyz:CRCL", "Open Long", "B", "100", "2", "0", "0xdef", 456, 1_700_000_000_001)
	if err != nil {
		t.Fatalf("ParseLeaderFill xyz: %v", err)
	}
	if xyz.Symbol != "XYZ:CRCL" {
		t.Fatalf("xyz symbol = %q", xyz.Symbol)
	}
}
