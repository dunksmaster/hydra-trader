package trader

import (
	"nofx/events"
	"strings"
	"testing"
	"time"
)

func TestLeaderRuleReasonText(t *testing.T) {
	if got := leaderRuleReasonText("leader closed leg"); got != "Leader closed position" {
		t.Fatalf("closed = %q", got)
	}
	if got := leaderRuleReasonText("leader now long on BTCUSDT"); got != "Leader flipped to long" {
		t.Fatalf("flip long = %q", got)
	}
}

func TestCopyLeaderRuleDedupe(t *testing.T) {
	copyLeaderRuleAt = map[string]time.Time{}
	at := &AutoTrader{id: "t1", name: "Alpha 6859"}
	if !shouldEmitCopyLeaderRuleAlert(at.id, "BTCUSDT", "close_short") {
		t.Fatal("first alert should pass")
	}
	if shouldEmitCopyLeaderRuleAlert(at.id, "BTCUSDT", "close_short") {
		t.Fatal("duplicate within window should block")
	}
	if !shouldEmitCopyLeaderRuleAlert(at.id, "ETHUSDT", "close_short") {
		t.Fatal("different symbol should pass")
	}
	key := copyLeaderRuleDedupeKey("t1", "btcusdt", "CLOSE_SHORT")
	if !strings.Contains(key, events.AlertCopyLeaderRule) {
		t.Fatalf("dedupe key = %q", key)
	}
	copyLeaderRuleAt[key] = time.Now().Add(-copyLeaderRuleDedupeWindow - time.Second)
	if !shouldEmitCopyLeaderRuleAlert(at.id, "BTCUSDT", "close_short") {
		t.Fatal("alert should pass after window expires")
	}
}

func TestEmitCopyLeaderRuleAlertMessage(t *testing.T) {
	copyLeaderRuleAt = map[string]time.Time{}
	done := make(chan events.SystemAlertEvent, 1)
	events.OnSystemAlert(func(e events.SystemAlertEvent) {
		done <- e
	})
	at := &AutoTrader{id: "t1", name: "Alpha 6859"}
	at.emitCopyLeaderRuleAlert("BTCUSDT", "close_short", "Hyperliquid", "leader now long on BTCUSDT")
	select {
	case got := <-done:
		if got.Type != events.AlertCopyLeaderRule {
			t.Fatalf("type = %q", got.Type)
		}
		if got.TraderName != "Alpha 6859" {
			t.Fatalf("trader = %q", got.TraderName)
		}
		for _, want := range []string{"Leader flipped to long", "BTCUSDT", "close_short", "Hyperliquid", "not SL/TP"} {
			if !strings.Contains(got.Message, want) {
				t.Fatalf("missing %q in %q", want, got.Message)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for alert")
	}
}

func TestEmitCopyLeaderOverflowCloseAlert(t *testing.T) {
	copyLeaderRuleAt = map[string]time.Time{}
	done := make(chan events.SystemAlertEvent, 1)
	events.OnSystemAlert(func(e events.SystemAlertEvent) {
		done <- e
	})
	at := &AutoTrader{id: "t1", name: "Hyperdash b7e0"}
	at.emitCopyLeaderOverflowCloseAlert("ASTERUSDT", "long", "Crypto BigG")
	select {
	case got := <-done:
		if got.Type != events.AlertCopyLeaderRule {
			t.Fatalf("type = %q", got.Type)
		}
		for _, want := range []string{"Leader flat", "ASTERUSDT", "overflow long", "Crypto BigG", "copy rule"} {
			if !strings.Contains(got.Message, want) {
				t.Fatalf("missing %q in %q", want, got.Message)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for alert")
	}
}
