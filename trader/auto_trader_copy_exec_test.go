package trader

import (
	"errors"
	"testing"
	"time"
)

func TestIsCopyOrderRecoverableError(t *testing.T) {
	if !isCopyOrderRecoverableError(errors.New("Order has invalid price")) {
		t.Fatal("expected invalid price recoverable")
	}
	if isCopyOrderRecoverableError(errors.New("insufficient margin")) {
		t.Fatal("margin should not be recoverable")
	}
}

func TestCopyTransientCircuitBreaker(t *testing.T) {
	at := &AutoTrader{}
	now := time.Now()
	at.markCopyTransientFailure("ETHUSDT", "close_short", now)
	if !at.copyActionCoolingDown("ethusdt", "CLOSE_SHORT", now.Add(time.Second)) {
		t.Fatal("transient failure must cool down the same symbol/action")
	}
	if at.copyActionCoolingDown("ETHUSDT", "close_short", now.Add(copyTransientBackoff+time.Second)) {
		t.Fatal("copy action must resume after transient backoff")
	}
	if at.copyActionCoolingDown("BTCUSDT", "close_short", now.Add(time.Second)) {
		t.Fatal("backoff must not suppress unrelated symbols")
	}
}
