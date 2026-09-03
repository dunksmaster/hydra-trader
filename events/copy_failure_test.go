package events

import "testing"

func TestIsTransientCopyFailureTextLeaderFlip(t *testing.T) {
	msg := "BTCUSDT close_short failed: failed to get positions: failed to fetch user state: API error 0: (leader now long on BTCUSDT)"
	if !IsTransientCopyFailureText(msg) {
		t.Fatal("leader flip HL state error must be transient")
	}
}

func TestIsTransientCopyFailureTextRealFailure(t *testing.T) {
	if IsTransientCopyFailureText("BTCUSDT open_long failed: insufficient margin") {
		t.Fatal("margin failure must not be transient")
	}
}
