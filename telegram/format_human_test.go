package telegram

import (
	"nofx/events"
	"strings"
	"testing"
)

func TestFormatMoneyHumanReadable(t *testing.T) {
	if got := formatMoney(93.6347); got != "$93.63" {
		t.Fatalf("equity = %q, want $93.63", got)
	}
	if got := formatMoney(-0.158790); got != "-$0.16" {
		t.Fatalf("small loss = %q, want -$0.16", got)
	}
	if got := formatPnLMoney(-0.158790); got != "-$0.16" {
		t.Fatalf("pnl money = %q, want -$0.16", got)
	}
}

func TestFormatPortfolioFooterOneLine(t *testing.T) {
	text := formatPortfolioFooterOneLine(AccountSnapshot{
		TotalEquity:      93.6347,
		UnrealizedProfit: 1.8062,
		PositionCount:    2,
	})
	if !strings.Contains(text, "$93.63") || !strings.Contains(text, "$1.81") {
		t.Fatalf("footer = %q", text)
	}
	if !strings.Contains(text, "Equity") || !strings.Contains(text, "uPnL") {
		t.Fatalf("footer should use Equity/uPnL labels, got %q", text)
	}
}

func TestSymbolMatchesQuery(t *testing.T) {
	if !symbolMatchesQuery("BTCUSDT", "BTC") {
		t.Fatal("BTC should match BTCUSDT")
	}
	if !symbolMatchesQuery("xyz:TSLA", "TSLA") {
		t.Fatal("TSLA should match xyz:TSLA")
	}
	if symbolMatchesQuery("ETHUSDT", "BTC") {
		t.Fatal("BTC should not match ETH")
	}
}

func TestFormatTradeAlertRealizedPnL(t *testing.T) {
	text := formatTradeAlertRich(nil, events.TradeEvent{
		Action:      "close_long",
		Symbol:      "HYPEUSDT",
		Side:        "LONG",
		Quantity:    1,
		Price:       59,
		RealizedPnL: -0.158790,
	}, "en", nil)
	if strings.Contains(text, "0.158790") {
		t.Fatalf("realized pnl should be rounded, got %q", text)
	}
	if !strings.Contains(text, "-$0.16") {
		t.Fatalf("expected -$0.16, got %q", text)
	}
}
