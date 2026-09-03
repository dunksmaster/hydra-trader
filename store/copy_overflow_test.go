package store

import "testing"

func TestCopyConfigOverflowJSONRoundTrip(t *testing.T) {
	cfg := DefaultCopyStrategyConfig()
	cfg.OverflowEnabled = true
	cfg.OverflowTraderID = "bigg-1"
	cfg.Normalize()
	if !cfg.OverflowEnabled || cfg.OverflowTraderID != "bigg-1" {
		t.Fatalf("normalize dropped overflow fields: %#v", cfg)
	}
	if len(cfg.OverflowOnSkip) != 3 {
		t.Fatalf("expected default skip list, got %#v", cfg.OverflowOnSkip)
	}
	if cfg.OverflowMaxPositions != 10 {
		t.Fatalf("expected overflow_max_positions=10, got %d", cfg.OverflowMaxPositions)
	}
}

func TestNormalizeOverflowSide(t *testing.T) {
	if normalizeOverflowSide("SHORT") != "short" {
		t.Fatal("short")
	}
	if normalizeOverflowSide("long") != "long" {
		t.Fatal("long")
	}
}
