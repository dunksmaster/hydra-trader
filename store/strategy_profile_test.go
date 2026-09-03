package store

import (
	"testing"
)

func TestCopyStrategyConfigLayerDefaults(t *testing.T) {
	cfg := DefaultCopyStrategyConfig()
	cfg.Normalize()
	if cfg.CopyLayer != 2 {
		t.Fatalf("default layer = %d, want 2", cfg.CopyLayer)
	}
	if cfg.CopyPaused {
		t.Fatal("default should not be paused")
	}
	cfg.CopyLayer = 3
	cfg.Normalize()
	if !cfg.CopyPaused {
		t.Fatal("layer 3 should force copy_paused")
	}
}

func TestCopyLayerFieldsRoundTrip(t *testing.T) {
	cfg := DefaultCopyStrategyConfig()
	cfg.CopyLayer = 1
	cfg.CopyPaused = false
	st := &Strategy{Config: `{}`}
	if err := st.SetConfig(&StrategyConfig{
		StrategyType: "copy_trading",
		CopyConfig:   &cfg,
	}); err != nil {
		t.Fatal(err)
	}
	parsed, err := st.ParseConfig()
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CopyConfig.CopyLayer != 1 {
		t.Fatalf("layer = %d", parsed.CopyConfig.CopyLayer)
	}
}
