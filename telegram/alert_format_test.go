package telegram

import (
	"nofx/events"
	"strings"
	"testing"
)

func TestFormatSystemAlertSafeMode(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Crypto BigG",
		Type:       events.AlertSafeMode,
		Message:    "AI failed 3 consecutive times",
	}, "en")
	if !strings.Contains(text, "Safe mode ON") || !strings.Contains(text, "Crypto BigG") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertWallet(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "NOFX Autopilot",
		Type:       events.AlertWalletEmpty,
		Message:    "AI fee wallet out of funds",
	}, "en")
	if !strings.Contains(text, "AI wallet empty") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertCopySkipped(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Copy Bot",
		Type:       events.AlertCopySkipped,
		Message:    "Leader 0xabc → BTCUSDT open_long skipped (insufficient margin)",
	}, "en")
	if !strings.Contains(text, "Copy skipped") || !strings.Contains(text, "Copy Bot") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertCopyFailed(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Copy Bot",
		Type:       events.AlertCopyFailed,
		Message:    "Leader 0xabc → ETHUSDT open_long failed: margin error",
	}, "en")
	if !strings.Contains(text, "Copy failed") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertCopyOverflow(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "🐉 Leviathan",
		Type:       events.AlertCopyOverflow,
		Message:    "HL skipped BTCUSDT open_short (already_open) → opened on Crypto BigG",
	}, "en")
	if !strings.Contains(text, "Copy overflow") || !strings.Contains(text, "Leviathan") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertCopyLeaderRule(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Alpha 6859",
		Type:       events.AlertCopyLeaderRule,
		Message:    "Leader flipped to long (copy rule) — closing BTCUSDT close_short on Hyperliquid — not SL/TP, not manual",
	}, "en")
	if !strings.Contains(text, "Leader copy rule") || !strings.Contains(text, "Alpha 6859") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertCopyPaused(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Money Printer",
		Type:       events.AlertCopyPaused,
		Message:    "Leader 0xabc → BTCUSDT open_long skipped (L3 PAUSED — no new opens)",
	}, "en")
	if !strings.Contains(text, "paused") || !strings.Contains(text, "Money Printer") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertCopyL2Evicted(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Hyperdash e282",
		Type:       events.AlertCopyL2Evicted,
		Message:    "Closed L2 HYPE Leviathan (+0.40 USD) after 30m review to follow L1 0xe282... on HYPEUSDT",
	}, "en")
	if !strings.Contains(text, "L2 evicted") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertLiquidationRisk(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Crypto BigG",
		Type:       events.AlertLiquidationRisk,
		Message:    "Crypto BigG margin used 85% (available $12.00). Add funds to this wallet.",
	}, "en")
	if !strings.Contains(text, "liquidation") || !strings.Contains(text, "Fund: <b>Bitget — Crypto BigG</b>") {
		t.Fatalf("got %q", text)
	}
}

func TestFormatSystemAlertLiquidationRiskSharedHyperliquidWallet(t *testing.T) {
	text := formatSystemAlert(nil, events.SystemAlertEvent{
		TraderName: "Alpha 6859",
		Type:       events.AlertLiquidationRisk,
		Message:    "Alpha 6859 margin used 97% (available $0.43). Add funds to this wallet.",
	}, "en")
	for _, want := range []string{
		"Bot: <b>Alpha 6859</b>",
		"Fund: <b>Hyperliquid trading wallet</b>",
		"Shared by Leviathan, Grinder, Money Printer, Copy L4, and Alpha 6859.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}

func TestRiskAlertKeyboardUsesShortCallbacks(t *testing.T) {
	kb := riskAlertKeyboard("a-very-long-trader-id", "Alpha 6859", "en")
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("keyboard = %+v", kb.InlineKeyboard)
	}
	for _, button := range kb.InlineKeyboard[0] {
		if button.CallbackData == nil || len(*button.CallbackData) > 64 {
			t.Fatalf("invalid callback: %+v", button)
		}
	}
}
