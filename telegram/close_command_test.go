package telegram

import (
	"strings"
	"testing"
)

func TestFormatCloseHelpSimpleInstructions(t *testing.T) {
	portfolios := []TraderPortfolio{
		{
			Info: TraderInfo{TraderID: "t1", TraderName: "Alpha 6859", Exchange: "hyperliquid", IsRunning: true},
			Positions: []map[string]any{
				{"symbol": "HYPEUSDT", "side": "long"},
			},
		},
	}

	text := formatCloseHelp(portfolios, "en")

	for _, bad := range []string{
		"take profit", "cut)", "🟢 Close", "🔴 Close", "unknow", "PUMP long hl",
		"refresh the list", "/pnl — refresh",
	} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(bad)) {
			t.Fatalf("close help must not mention %q, got:\n%s", bad, text)
		}
	}

	for _, want := range []string{
		"/positions", "Tap the", "Close</b> button", "/close_", "Open now:",
		"HYPE", "LONG", "/close all",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("close help missing %q, got:\n%s", want, text)
		}
	}
}

func TestFormatCloseAmbiguousUsesTokenNotVenue(t *testing.T) {
	matches := []closeTarget{
		{TraderID: "t1", Symbol: "HYPEUSDT", Side: "long", Exchange: "unknown"},
		{TraderID: "t2", Symbol: "HYPEUSDT", Side: "short", Exchange: "hyperliquid"},
	}

	text := formatCloseAmbiguous(matches, "en")

	if strings.Contains(text, "unknow") || strings.Contains(text, " hl") {
		t.Fatalf("ambiguous close must not use venue alias, got:\n%s", text)
	}
	if strings.Count(text, "/close_") < 2 {
		t.Fatalf("expected /close_ token commands, got:\n%s", text)
	}
}
