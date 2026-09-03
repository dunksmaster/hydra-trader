package telegram

import (
	"nofx/events"
	"sync"
	"testing"
	"time"
)

func TestTradeAlertDedupeKeyClose(t *testing.T) {
	key, window := tradeAlertDedupeKey(events.TradeEvent{
		TraderID: "bigg",
		Symbol:   "GRASSUSDT",
		Action:   "close_short",
		OrderID:  "fill-1",
	})
	if key != "bigg:close:GRASSUSDT:close_short" || window != closeTradeAlertDedupeWindow {
		t.Fatalf("got key=%q window=%v", key, window)
	}
}

func TestTradeAlertDedupeKeyOpenUsesOrderID(t *testing.T) {
	key, window := tradeAlertDedupeKey(events.TradeEvent{
		TraderID: "bigg",
		Symbol:   "BTCUSDT",
		Action:   "open_long",
		OrderID:  "ord-99",
	})
	if key != "ord-99:open_long" || window != 2*time.Minute {
		t.Fatalf("got key=%q window=%v", key, window)
	}
}

func TestCopyIssueDedupeWithin30Minutes(t *testing.T) {
	notifierDedupe = sync.Map{}
	key := "trader-1:" + events.AlertCopySkipped + ":margin"
	window := 30 * time.Minute

	notifierDedupe.Store(key, dedupeEntry{at: time.Now().Add(-5 * time.Minute)})
	if v, ok := notifierDedupe.Load(key); ok {
		if entry, ok := v.(dedupeEntry); ok && time.Since(entry.at) < window {
			return // suppressed — expected
		}
	}
	t.Fatal("expected skip alert to be within 30m dedupe window")
}

func TestCopyIssueDedupeAfter30Minutes(t *testing.T) {
	notifierDedupe = sync.Map{}
	key := "trader-1:" + events.AlertCopySkipped + ":margin"
	window := 30 * time.Minute

	notifierDedupe.Store(key, dedupeEntry{at: time.Now().Add(-31 * time.Minute)})
	if v, ok := notifierDedupe.Load(key); ok {
		if entry, ok := v.(dedupeEntry); ok && time.Since(entry.at) < window {
			t.Fatal("expected alert to pass dedupe after 31m")
		}
	}
}

func TestCopyFailureBurstCapsAtThree(t *testing.T) {
	notifierDedupeMu.Lock()
	copyFailureBurst = copyFailureBurstState{}
	now := time.Now()
	for i := 0; i < copyFailureAlertBurstMax; i++ {
		if !allowCopyFailureAlertLocked(now.Add(time.Duration(i) * time.Second)) {
			notifierDedupeMu.Unlock()
			t.Fatalf("alert %d should be allowed", i+1)
		}
	}
	if allowCopyFailureAlertLocked(now.Add(time.Minute)) {
		notifierDedupeMu.Unlock()
		t.Fatal("fourth copy-failure alert must be suppressed")
	}
	notifierDedupeMu.Unlock()
}

func TestCopyFailureBurstResetsAfterWindow(t *testing.T) {
	notifierDedupeMu.Lock()
	now := time.Now()
	copyFailureBurst = copyFailureBurstState{
		windowStart: now.Add(-copyFailureAlertBurstWindow),
		sent:        copyFailureAlertBurstMax,
	}
	allowed := allowCopyFailureAlertLocked(now)
	notifierDedupeMu.Unlock()
	if !allowed {
		t.Fatal("first alert in a new window should be allowed")
	}
}

func TestNormalizeCopyFailureDedupeKeyStripsReason(t *testing.T) {
	key := normalizeCopyFailureDedupeKey(events.SystemAlertEvent{
		TraderID:  "t1",
		DedupeKey: "t1:copy_failed:BTCUSDT:close_short:leader now long on BTCUSDT",
		Message:   "BTCUSDT close_short failed: boom",
	})
	if key != "t1:copy_failed:BTCUSDT:close_short" {
		t.Fatalf("got %q", key)
	}
}

func TestIsTransientCopyFailureMessage(t *testing.T) {
	msg := "BTCUSDT close_short failed: failed to get positions: failed to fetch user state: API error 0: (leader now long on BTCUSDT)"
	if !events.IsTransientCopyFailureText(msg) {
		t.Fatal("expected transient HL copy failure to be suppressed")
	}
	if events.IsTransientCopyFailureText("BTCUSDT close_short failed: insufficient margin") {
		t.Fatal("non-transient failure should not be suppressed")
	}
}

func TestParseCopyFailureMessage(t *testing.T) {
	sym, act := parseCopyFailureMessage("BTCUSDT close_short failed: failed to get positions (leader now long on BTCUSDT)")
	if sym != "BTCUSDT" || act != "close_short" {
		t.Fatalf("got %q %q", sym, act)
	}
}
