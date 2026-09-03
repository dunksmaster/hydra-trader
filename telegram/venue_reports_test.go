package telegram

import "testing"

func TestAggregateHistoriesByVenue(t *testing.T) {
	histories := []TraderHistory{
		{
			Info: TraderInfo{Exchange: "hyperliquid", TraderName: "Alpha"},
			Trades: []ClosedTrade{
				{Symbol: "BTCUSDT", Side: "long", ExitTime: 2000, RealizedPnL: 10},
			},
			Stats: TradingStats{TotalTrades: 1, WinTrades: 1, TotalPnL: 10},
		},
		{
			Info: TraderInfo{Exchange: "hyperliquid", TraderName: "Hyperdash"},
			Trades: []ClosedTrade{
				{Symbol: "ETHUSDT", Side: "short", ExitTime: 3000, RealizedPnL: -5},
			},
			Stats: TradingStats{TotalTrades: 1, LossTrades: 1, TotalPnL: -5},
		},
		{
			Info: TraderInfo{Exchange: "bitget", TraderName: "Crypto BigG"},
			Trades: []ClosedTrade{
				{Symbol: "ASTERUSDT", Side: "long", ExitTime: 4000, RealizedPnL: 2},
			},
			Stats: TradingStats{TotalTrades: 1, WinTrades: 1, TotalPnL: 2},
		},
	}
	venues := aggregateHistoriesByVenue(histories)
	if len(venues) != 2 {
		t.Fatalf("expected 2 venues, got %d", len(venues))
	}
}

func TestFormatVenueHistoryReportCompact(t *testing.T) {
	text := formatVenueHistoryReport([]TraderHistory{
		{
			Info:   TraderInfo{Exchange: "bitget", TraderName: "BigG"},
			Trades: []ClosedTrade{{Symbol: "BTCUSDT", Side: "long", ExitTime: 1000, RealizedPnL: 1}},
			Stats:  TradingStats{TotalTrades: 1, WinTrades: 1, TotalPnL: 1},
		},
	}, "en", false)
	if text == "" || len(text) > 2000 {
		t.Fatalf("unexpected report length: %d", len(text))
	}
}
