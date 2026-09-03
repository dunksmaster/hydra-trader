package telegram

import (
	"strings"
	"testing"
	"time"
)

func TestFormatPositionsKeepsMixedSidesAccurate(t *testing.T) {
	portfolios := []TraderPortfolio{
		{
			Info: TraderInfo{TraderID: "bigg-1", TraderName: "Crypto BigG", Exchange: "bitget", IsRunning: true},
			Snapshot: AccountSnapshot{
				TotalEquity:      139.12,
				InitialBalance:   61.76,
				TotalPnL:         77.36,
				UnrealizedProfit: -0.81,
				PositionCount:    2,
				TraderName:       "Crypto BigG",
			},
			Stats: TradingStats{
				TotalTrades: 15,
				WinTrades:   7,
				LossTrades:  8,
				WinRate:     46.7,
				TotalPnL:    -16.34,
				TotalFee:    4.51,
			},
			Positions: []map[string]any{
				{
					"symbol": "HYPEUSDT", "side": "long", "quantity": 1.67,
					"entry_price": 59.721, "mark_price": 59.19,
					"unrealized_pnl": -0.88, "unrealized_pnl_pct": -4.46,
					"leverage": 5.0, "margin_used": 19.77,
				},
				{
					"symbol": "BTCUSDT", "side": "short", "quantity": 0.0022,
					"entry_price": 64204.9, "mark_price": 64174.0,
					"unrealized_pnl": 0.07, "unrealized_pnl_pct": 0.24,
					"leverage": 5.0, "margin_used": 28.24,
				},
			},
		},
		{
			Info: TraderInfo{TraderID: "autopilot-1", TraderName: "NOFX Autopilot", Exchange: "hyperliquid", IsRunning: true},
			Snapshot: AccountSnapshot{
				TotalEquity:      47.24,
				InitialBalance:   24.45,
				UnrealizedProfit: 0.54,
				PositionCount:    1,
				TraderName:       "NOFX Autopilot",
			},
			Stats: TradingStats{
				TotalTrades: 37,
				WinTrades:   19,
				LossTrades:  16,
				TotalPnL:    -12.02,
				TotalFee:    9.24,
			},
			Positions: []map[string]any{
				{
					"symbol": "BTCUSDT", "side": "long", "quantity": 0.00098,
					"entry_price": 63618.0, "mark_price": 64174.0,
					"unrealized_pnl": 0.54, "unrealized_pnl_pct": 8.66,
					"leverage": 10.0, "margin_used": 6.29,
				},
			},
		},
	}

	text := formatPositionsForPortfolios(portfolios, "en")

	if strings.Contains(text, "|") {
		t.Fatalf("telegram positions must not use markdown tables, got:\n%s", text)
	}
	if strings.Contains(text, "Closed trades:") {
		t.Fatalf("/positions must not include portfolio block, got:\n%s", text)
	}
	if !strings.Contains(text, "/pnl") {
		t.Fatalf("expected hint to use /pnl, got:\n%s", text)
	}
	if !strings.Contains(text, "<b>HYPE LONG</b>") || !strings.Contains(text, "└ 🔴") {
		t.Fatalf("expected compact HYPE list row with pnl dot, got:\n%s", text)
	}
	if !strings.Contains(text, "<b>BTC SHORT</b>") {
		t.Fatalf("expected SHORT label, got:\n%s", text)
	}
	if strings.Contains(text, "├ 📊") {
		t.Fatalf("/positions must not use multi-line position cards, got:\n%s", text)
	}
	if !strings.Contains(text, "────────────") || !strings.Contains(text, " • PnL ") {
		t.Fatalf("expected venue divider and bullet-separated rows, got:\n%s", text)
	}
	if !strings.Contains(text, "Bitget · Crypto BigG") {
		t.Fatalf("expected Bitget venue header, got:\n%s", text)
	}
	if !strings.Contains(text, "Hyperliquid · NOFX Autopilot") {
		t.Fatalf("expected Hyperliquid venue header, got:\n%s", text)
	}
	if !strings.Contains(text, "<code>/close_") {
		t.Fatalf("expected inline close token command for Bitget, got:\n%s", text)
	}
}

func TestFormatPositionsDedupesSharedHyperliquidWallet(t *testing.T) {
	pump := map[string]any{
		"symbol": "PUMPUSDT", "side": "long", "quantity": 100.0,
		"entry_price": 0.05, "mark_price": 0.045,
		"unrealized_pnl": -0.66, "unrealized_pnl_pct": -13.46,
		"leverage": 5.0, "margin_used": 4.93,
		"update_time": time.Now().UnixMilli(),
	}
	portfolios := []TraderPortfolio{
		{Info: TraderInfo{TraderID: "a", TraderName: "Leviathan", Exchange: "hyperliquid", IsRunning: true}, Positions: []map[string]any{pump}},
		{Info: TraderInfo{TraderID: "b", TraderName: "NOFX Autopilot", Exchange: "hyperliquid", IsRunning: true}, Positions: []map[string]any{pump}},
		{Info: TraderInfo{TraderID: "c", TraderName: "Copy L4", Exchange: "hyperliquid", IsRunning: true}, Positions: []map[string]any{pump}},
	}

	text := formatPositionsForPortfolios(portfolios, "en")
	if strings.Count(text, "<b>PUMP LONG</b>") != 1 {
		t.Fatalf("expected one PUMP row, got:\n%s", text)
	}
	if !strings.Contains(text, "shared wallet (3 bots)") {
		t.Fatalf("expected shared wallet header, got:\n%s", text)
	}
	if strings.Count(text, "<code>/close_") != 1 {
		t.Fatalf("expected one close token command, got:\n%s", text)
	}
	if !strings.Contains(text, "(1 total)") {
		t.Fatalf("expected total count 1, got:\n%s", text)
	}
}

func TestFindCloseTargetsDedupesSharedWallet(t *testing.T) {
	pump := map[string]any{"symbol": "PUMPUSDT", "side": "long"}
	portfolios := []TraderPortfolio{
		{Info: TraderInfo{TraderID: "a", TraderName: "Leviathan", Exchange: "hyperliquid", IsRunning: true}, Positions: []map[string]any{pump}},
		{Info: TraderInfo{TraderID: "b", TraderName: "Copy L4", Exchange: "hyperliquid", IsRunning: true}, Positions: []map[string]any{pump}},
		{Info: TraderInfo{TraderID: "c", TraderName: "Crypto BigG", Exchange: "bitget", IsRunning: true}, Positions: []map[string]any{
			{"symbol": "PUMPUSDT", "side": "long"},
		}},
	}

	all := findCloseTargets(portfolios, "PUMP", "", "")
	if len(all) != 2 {
		t.Fatalf("expected 2 wallet-level targets (hl + bg), got %d: %+v", len(all), all)
	}

	hlOnly := findCloseTargets(portfolios, "PUMP", "long", "hl")
	if len(hlOnly) != 1 || hlOnly[0].TraderID != "a" {
		t.Fatalf("expected one hl target via trader a, got %+v", hlOnly)
	}
}

func TestFormatPnLReportAllBots(t *testing.T) {
	text := formatPnLReport([]TraderPortfolio{
		{
			Info: TraderInfo{TraderName: "Crypto BigG"},
			Snapshot: AccountSnapshot{
				TotalEquity: 138, InitialBalance: 62, TotalPnL: 76,
				UnrealizedProfit: -1.5, TraderName: "Crypto BigG",
			},
			Stats: TradingStats{TotalTrades: 15, TotalPnL: -16.34, TotalFee: 4.51, WinRate: 46.7, WinTrades: 7, LossTrades: 8},
		},
	}, "en")
	if !strings.Contains(text, "Closed trades:") || !strings.Contains(text, "-$16.34") {
		t.Fatalf("pnl report missing closed stats: %s", text)
	}
	if !strings.Contains(text, "Start balance:") {
		t.Fatalf("pnl report missing start balance: %s", text)
	}
}

func TestMatchKeyboardShortcut(t *testing.T) {
	if got := matchKeyboardShortcut("Pozicionet"); got != "positions" {
		t.Fatalf("keyboard Pozicionet -> positions, got %q", got)
	}
	if got := matchKeyboardShortcut("Balanca"); got != "balance" {
		t.Fatalf("keyboard Balanca -> balance, got %q", got)
	}
}

func TestFormatTradeHistoryMarksLosses(t *testing.T) {
	exit := time.Date(2026, 8, 18, 16, 43, 0, 0, time.UTC).UnixMilli()
	text := formatTradeHistoryReport([]TraderHistory{
		{
			Info: TraderInfo{TraderName: "Crypto BigG", IsRunning: true},
			Stats: TradingStats{
				TotalTrades: 2, WinTrades: 1, LossTrades: 1,
				TotalPnL: -0.75, TotalFee: 0.34,
			},
			Trades: []ClosedTrade{
				{Symbol: "HYPEUSDT", Side: "long", EntryPrice: 59.72, ExitPrice: 59.40, RealizedPnL: -0.78, Fee: 0.17, ExitTime: exit},
				{Symbol: "SOLUSDT", Side: "long", EntryPrice: 75.86, ExitPrice: 76.01, RealizedPnL: 0.26, Fee: 0.17, ExitTime: exit},
			},
		},
	}, "en", false)

	if !strings.Contains(text, "Crypto BigG") || !strings.Contains(text, "Bitget") {
		t.Fatalf("missing bot header: %s", text)
	}
	if !strings.Contains(text, "HYPE") || !strings.Contains(text, "SOL") {
		t.Fatalf("missing symbols: %s", text)
	}
	if !strings.Contains(text, "net") {
		t.Fatalf("expected net pnl lines: %s", text)
	}
}

func TestFormatTradeHistoryLossesOnly(t *testing.T) {
	text := formatTradeHistoryReport([]TraderHistory{
		{
			Info: TraderInfo{TraderName: "Crypto BigG"},
			Trades: []ClosedTrade{
				{Symbol: "NVDAUSDT", Side: "long", RealizedPnL: -10, Fee: 0.58},
				{Symbol: "HYPEUSDT", Side: "long", RealizedPnL: 0.90, Fee: 0.17},
			},
		},
	}, "en", true)
	if strings.Contains(text, "HYPE") {
		t.Fatalf("losses-only must hide winners: %s", text)
	}
	if !strings.Contains(text, "NVDA") {
		t.Fatalf("losses-only must show losers: %s", text)
	}
}

func TestHistoryLossesOnlyFlag(t *testing.T) {
	if !historyLossesOnly("/history losses") {
		t.Fatal("expected losses flag")
	}
	if historyLossesOnly("/history") {
		t.Fatal("plain history should not filter")
	}
}

func TestMatchNLQuickIntentTradeHistory(t *testing.T) {
	if got := matchNLQuickIntent("show my losing trades"); got != "history losses" {
		t.Fatalf("losing trades -> history losses, got %q", got)
	}
	if got := matchNLQuickIntent("show my trade history"); got != "history" {
		t.Fatalf("trade history -> history, got %q", got)
	}
	if got := matchKeyboardShortcut("Orders"); got != "orders" {
		t.Fatalf("keyboard Orders -> orders, got %q", got)
	}
}

func TestMatchNLQuickIntentOpenOrders(t *testing.T) {
	if got := matchNLQuickIntent("why there are 9 order"); got != "" {
		t.Fatalf("should not steal unrelated order questions, got %q", got)
	}
	if got := matchNLQuickIntent("show my open orders"); got != "orders" {
		t.Fatalf("open orders should map to orders, got %q", got)
	}
	if got := matchNLQuickIntent("how much have i lost"); got != "pnl" {
		t.Fatalf("loss question should map to pnl, got %q", got)
	}
}
