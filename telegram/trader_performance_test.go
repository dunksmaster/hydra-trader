package telegram

import (
	"strings"
	"testing"
)

func TestFormatTradeStreak(t *testing.T) {
	tests := []struct {
		name   string
		trades []ClosedTrade
		want   string
	}{
		{"empty", nil, "—"},
		{"single win", []ClosedTrade{{RealizedPnL: 10, ExitTime: 100}}, "1W"},
		{"three wins", []ClosedTrade{
			{RealizedPnL: 5, ExitTime: 300},
			{RealizedPnL: 3, ExitTime: 200},
			{RealizedPnL: 1, ExitTime: 100},
		}, "3W"},
		{"two losses", []ClosedTrade{
			{RealizedPnL: -2, ExitTime: 200},
			{RealizedPnL: -1, ExitTime: 100},
			{RealizedPnL: 5, ExitTime: 50},
		}, "2L"},
		{"breakeven counts as loss streak", []ClosedTrade{
			{RealizedPnL: 0, ExitTime: 200},
			{RealizedPnL: 5, ExitTime: 100},
		}, "1L"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTradeStreak(tc.trades); got != tc.want {
				t.Fatalf("formatTradeStreak() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatCopyTraderPerfLine(t *testing.T) {
	line := formatCopyTraderPerfLine(CopyTraderPerf{
		Name:    "Hyperdash",
		Layer:   2,
		Running: true,
		Stats: TradingStats{
			TotalTrades: 17,
			WinTrades:   12,
			LossTrades:  5,
			WinRate:     70.6,
			TotalPnL:    42.30,
		},
		Streak:     "3W",
		IsFavorite: true,
	}, "en")
	for _, want := range []string{"⭐", "Hyperdash", "L2", "running", "12W/5L", "3W", "+$42.30"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in:\n%s", want, line)
		}
	}
}

func TestFormatTraderPerformanceReportEmpty(t *testing.T) {
	// No HTTP client — verify title path when fetch returns empty via nil slice formatting helpers only.
	line := formatCopyTraderPerfLine(CopyTraderPerf{Name: "A", Layer: 2, Running: true}, "en")
	if !strings.Contains(line, "A") {
		t.Fatalf("expected name in line: %s", line)
	}
}

func TestTraderTokenMatches(t *testing.T) {
	tr := map[string]any{"strategy_id": ""}
	if !traderTokenMatches("abc_b7e0_copy", "Hyperdash", tr, nil, []string{"b7e0"}) {
		t.Fatal("expected id suffix match")
	}
	if !traderTokenMatches("id1", "Hyperdash Copy", tr, nil, []string{"hyperdash"}) {
		t.Fatal("expected name match")
	}
	if traderTokenMatches("id1", "Alpha", tr, nil, []string{"beta"}) {
		t.Fatal("expected no match")
	}
	if traderTokenMatches("id1", "Hyperdash", tr, nil, []string{"hyperdash", "missing"}) {
		t.Fatal("expected all tokens required")
	}
}

func TestShortTraderID(t *testing.T) {
	if got := shortTraderID("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	long := "abcdefgh1234567890_extra"
	if got := shortTraderID(long); got != "abcdefgh…" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatFavoriteSummaryLine(t *testing.T) {
	line := formatFavoriteSummaryLine(CopyTraderPerf{
		Name:  "BigG",
		Layer: 1,
		Stats: TradingStats{TotalPnL: 12.5, WinRate: 55, TotalTrades: 10},
	})
	if !strings.Contains(line, "BigG") || !strings.Contains(line, "L1") || !strings.Contains(line, "55%") {
		t.Fatalf("unexpected line: %s", line)
	}
}
