package telegram

import "testing"

func TestShortLeaderAddr(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"0xb7e0b9fbc9479330d70bcc82a7d4325a20e8d1aa", "0xb7e0...d1aa"},
		{"0x6859da14835424957a1e6b397d8026b1d9ff7e1e", "0x6859...7e1e"},
		{"0xabc", "0xabc"},
	}
	for _, tc := range tests {
		if got := shortLeaderAddr(tc.in); got != tc.want {
			t.Errorf("shortLeaderAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCopyLayerFromConfig(t *testing.T) {
	if got := copyLayerFromConfig(map[string]any{"copy_layer": float64(1)}); got != 1 {
		t.Fatalf("layer float64: got %d", got)
	}
	if got := copyLayerFromConfig(map[string]any{"copy_layer": 3}); got != 3 {
		t.Fatalf("layer int: got %d", got)
	}
	if got := copyLayerFromConfig(map[string]any{}); got != 2 {
		t.Fatalf("default layer: got %d", got)
	}
}
