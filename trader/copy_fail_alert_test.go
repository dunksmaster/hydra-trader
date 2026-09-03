package trader

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestShouldEmitCopyFailAlertSuppressesTransient(t *testing.T) {
	copyFailAlertMu.Lock()
	copyFailBurst = copyFailBurstState{}
	copyFailIncidentAt = map[string]time.Time{}
	copyFailAlertMu.Unlock()

	err := errors.New("failed to close short position for BTCUSDT: failed to get positions: failed to fetch user state: API error 0: (leader now long on BTCUSDT)")
	if shouldEmitCopyFailAlert("t1", "BTCUSDT", "close_short", err, "leader now long on BTCUSDT") {
		t.Fatal("transient HL state error must not emit Telegram alert")
	}
}

func TestShouldEmitCopyFailAlertCapsBurst(t *testing.T) {
	copyFailAlertMu.Lock()
	copyFailBurst = copyFailBurstState{}
	copyFailIncidentAt = map[string]time.Time{}
	copyFailAlertMu.Unlock()

	err := errors.New("insufficient margin")
	allowed := 0
	for i := 0; i < 10; i++ {
		sym := fmt.Sprintf("SYM%dUSDT", i)
		if shouldEmitCopyFailAlert("t1", sym, "open_long", err) {
			allowed++
		}
	}
	if allowed != copyFailTelegramBurstMax {
		t.Fatalf("expected %d alerts allowed, got %d", copyFailTelegramBurstMax, allowed)
	}
}

func TestShouldEmitCopyFailAlertOnePerSymbolAction(t *testing.T) {
	copyFailAlertMu.Lock()
	copyFailBurst = copyFailBurstState{}
	copyFailIncidentAt = map[string]time.Time{}
	copyFailAlertMu.Unlock()

	err := errors.New("exchange rejected")
	if !shouldEmitCopyFailAlert("t1", "BTCUSDT", "close_short", err) {
		t.Fatal("first alert should pass")
	}
	if shouldEmitCopyFailAlert("t1", "BTCUSDT", "close_short", err) {
		t.Fatal("duplicate symbol/action should be suppressed within incident window")
	}
	if !shouldEmitCopyFailAlert("t1", "ETHUSDT", "close_short", err) {
		t.Fatal("different symbol should still be allowed within burst budget")
	}
}

func TestCopyFailDedupeKeyStable(t *testing.T) {
	key := copyFailDedupeKey("abc", "btcusdt", "CLOSE_SHORT")
	if key != "abc:copy_failed:BTCUSDT:close_short" {
		t.Fatalf("unexpected key %q", key)
	}
}
