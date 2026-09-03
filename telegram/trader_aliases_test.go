package telegram

import "testing"

func TestCopyTraderDisplayNameMochi(t *testing.T) {
	got := copyTraderDisplayName("machibigbrother")
	if got != "machibigbrother (mochi)" {
		t.Fatalf("got %q", got)
	}
}

func TestTokenMatchesMochiAlias(t *testing.T) {
	if !tokenMatchesCopyTraderName("machibigbrother", "mochi") {
		t.Fatal("mochi should match machibigbrother")
	}
	if tokenMatchesCopyTraderName("hyperdash b7e0", "mochi") {
		t.Fatal("mochi should not match hyperdash")
	}
}
