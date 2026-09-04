package trader

import (
	"nofx/kernel"
	"nofx/store"
	"sync/atomic"
	"testing"
)

func TestShouldOverflowCategory(t *testing.T) {
	if shouldOverflowCategory(nil, "already_open") {
		t.Fatal("nil config should not overflow")
	}
	cfg := &store.CopyStrategyConfig{OverflowEnabled: true, OverflowTraderID: "bigg"}
	if !shouldOverflowCategory(cfg, "already_open") || !shouldOverflowCategory(cfg, "max_positions") || !shouldOverflowCategory(cfg, "margin") {
		t.Fatal("enabled overflow should accept already_open, max_positions, and margin")
	}
	if shouldOverflowCategory(cfg, "blocked") {
		t.Fatal("blocked must not overflow")
	}
	cfg.OverflowOnSkip = []string{"already_open"}
	if shouldOverflowCategory(cfg, "max_positions") {
		t.Fatal("max_positions not in allow-list")
	}
	cfg.OverflowEnabled = false
	if shouldOverflowCategory(cfg, "already_open") {
		t.Fatal("disabled overflow should not fire")
	}
	cfg.OverflowEnabled = true
	cfg.OverflowParallel = true
	if !shouldOverflowCategory(cfg, "parallel") {
		t.Fatal("parallel overflow should fire when enabled")
	}
	cfg.OverflowParallel = false
	if shouldOverflowCategory(cfg, "parallel") {
		t.Fatal("parallel overflow should not fire when disabled")
	}
}

func TestCopyConfigNormalizeOverflowDefaults(t *testing.T) {
	cfg := store.CopyStrategyConfig{OverflowEnabled: true, OverflowTraderID: "bigg"}
	cfg.Normalize()
	if len(cfg.OverflowOnSkip) != 3 {
		t.Fatalf("default overflow_on_skip = %#v", cfg.OverflowOnSkip)
	}
	if cfg.OverflowMaxPositions != 10 {
		t.Fatalf("default overflow_max_positions = %d", cfg.OverflowMaxPositions)
	}
}

func TestCopyMaxPositionsAllowsTen(t *testing.T) {
	cfg := store.CopyStrategyConfig{MaxPositions: 10}
	cfg.Normalize()
	if cfg.MaxPositions != 10 {
		t.Fatalf("max_positions clamped to %d, want 10", cfg.MaxPositions)
	}
	cfg.MaxPositions = 25
	cfg.Normalize()
	if cfg.MaxPositions != store.CopyMaxPositions {
		t.Fatalf("max_positions = %d, want %d", cfg.MaxPositions, store.CopyMaxPositions)
	}
}

func TestBlockAIOnOverflowLeg(t *testing.T) {
	ai := &AutoTrader{
		id:                "bigg",
		name:              "Crypto BigG",
		overflowOpenSides: map[string]string{"BTCUSDT": "short"},
	}
	if err := ai.blockAIOnOverflowLeg("BTCUSDT", "close_short"); err == nil {
		t.Fatal("AI close of overflow short should be blocked")
	}
	if err := ai.blockAIOnOverflowLeg("BTCUSDT", "open_long"); err == nil {
		t.Fatal("AI flip of overflow short should be blocked")
	}
	if err := ai.blockAIOnOverflowLeg("ETHUSDT", "close_long"); err != nil {
		t.Fatalf("unrelated symbol should pass: %v", err)
	}

	atomic.StoreInt32(&ai.overflowExecDepth, 1)
	if err := ai.blockAIOnOverflowLeg("BTCUSDT", "close_short"); err != nil {
		t.Fatalf("overflow exec must bypass AI protect: %v", err)
	}

	copyBot := &AutoTrader{
		id: "copy-1",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				StrategyType: "copy_trading",
				CopyConfig:   &store.CopyStrategyConfig{LeaderAddress: "0xabc"},
			},
		},
		overflowOpenSides: map[string]string{"BTCUSDT": "short"},
	}
	if err := copyBot.blockAIOnOverflowLeg("BTCUSDT", "close_short"); err != nil {
		t.Fatalf("copy strategy must not self-block: %v", err)
	}
}

func TestCountAIOwnedPositionsExcludesOverflow(t *testing.T) {
	ai := &AutoTrader{
		id:                "bigg",
		overflowOpenSides: map[string]string{"BTCUSDT": "short"},
	}
	positions := []map[string]interface{}{
		{"symbol": "BTCUSDT", "side": "short"},
		{"symbol": "ETHUSDT", "side": "long"},
	}
	if n := ai.countAIOwnedPositions(positions); n != 1 {
		t.Fatalf("AI-owned count = %d, want 1", n)
	}
}

func TestOverflowNotionalUSD(t *testing.T) {
	if overflowNotionalUSD(nil) != 50 {
		t.Fatal("default overflow notional should be 50")
	}
	cfg := &store.CopyStrategyConfig{NotionalUSD: 50, OverflowNotionalUSD: 0}
	if overflowNotionalUSD(cfg) != 50 {
		t.Fatal("should fall back to copy notional")
	}
	cfg.OverflowNotionalUSD = 75
	if overflowNotionalUSD(cfg) != 75 {
		t.Fatal("explicit overflow notional should win")
	}
}

func TestFollowerHasAnyPosition(t *testing.T) {
	positions := []map[string]interface{}{{"symbol": "BTCUSDT", "side": "long"}}
	if !followerHasAnyPosition(positions, "BTCUSDT") {
		t.Fatal("expected BTC position")
	}
	if followerHasAnyPosition(positions, "ETHUSDT") {
		t.Fatal("ETH should be flat")
	}
}

func TestInjectHardExitsSkipsOverflowLegs(t *testing.T) {
	at := &AutoTrader{
		id: "bigg",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				RiskControl: store.RiskControlConfig{
					HardStopLossMarginPct: 10,
				},
			},
		},
		overflowOpenSides: map[string]string{"BTCUSDT": "short"},
	}
	got := at.injectHardExits(nil, &kernel.Context{
		Positions: []kernel.PositionInfo{{Symbol: "BTCUSDT", Side: "short", UnrealizedPnLPct: -40}},
	})
	if len(got) != 0 {
		t.Fatalf("overflow short must not get a hard exit, got %#v", got)
	}
}

func TestOverflowMaxPositions(t *testing.T) {
	if overflowMaxPositions(nil) != 10 {
		t.Fatal("default 10")
	}
	if overflowMaxPositions(&store.CopyStrategyConfig{OverflowMaxPositions: 6}) != 6 {
		t.Fatal("explicit max")
	}
	positions := make([]map[string]interface{}, 10)
	if len(positions) < overflowMaxPositions(&store.CopyStrategyConfig{OverflowMaxPositions: 10}) {
		t.Fatal("10th position should be at cap")
	}
	if len(positions) >= overflowMaxPositions(&store.CopyStrategyConfig{OverflowMaxPositions: 10}) {
		// at cap — 11th would skip
	} else {
		t.Fatal("expected at cap")
	}
}

func TestLiquidationRiskReason(t *testing.T) {
	if got := liquidationRiskReason(100, 50, nil); got != "" {
		t.Fatalf("50 percent used should be quiet, got %q", got)
	}
	if got := liquidationRiskReason(100, 15, nil); got == "" {
		t.Fatal("85% used should alert")
	}
	if got := liquidationRiskReason(100, 90, []map[string]interface{}{
		{"symbol": "BTCUSDT", "markPrice": 100.0, "liquidationPrice": 94.0},
	}); got == "" {
		t.Fatal("mark within 8% of liq should alert")
	}
}

func TestOverflowSideFromAction(t *testing.T) {
	if overflowSideFromAction("open_short") != "short" {
		t.Fatal("open_short")
	}
	if overflowSideFromAction("close_long") != "long" {
		t.Fatal("close_long")
	}
}

func TestOverflowCloseQtyUsesTrackedLeg(t *testing.T) {
	row := &store.CopyOverflowLeg{Quantity: 120}
	cfg := &store.CopyStrategyConfig{OverflowNotionalUSD: 50}
	if got := overflowCloseQty(row, 9362, 1.0, cfg); got != 120 {
		t.Fatalf("tracked leg qty = %v, want 120", got)
	}
}

func TestOverflowCloseQtyCapsLegacyFullMirror(t *testing.T) {
	row := &store.CopyOverflowLeg{Quantity: 0}
	cfg := &store.CopyStrategyConfig{NotionalUSD: 50, OverflowNotionalUSD: 50}
	got := overflowCloseQty(row, 9362, 100.0, cfg)
	want := 50 / 100.0 * 1.15
	if got <= 0 || got >= 9362 || got > want*1.01 {
		t.Fatalf("legacy cap qty = %v, want ~%.4f not full mirror", got, want)
	}
}

func TestOverflowCloseQtyFlat(t *testing.T) {
	if got := overflowCloseQty(&store.CopyOverflowLeg{Quantity: 10}, 0, 1, nil); got != 0 {
		t.Fatalf("flat held = %v, want 0", got)
	}
}

type overflowFillStub struct {
	positions []map[string]interface{}
	calls     int
}

func (s *overflowFillStub) GetPositions() ([]map[string]interface{}, error) {
	s.calls++
	return s.positions, nil
}

func TestWaitOverflowFillConfirm(t *testing.T) {
	stub := &overflowFillStub{
		positions: []map[string]interface{}{{"symbol": "ASTERUSDT", "side": "long", "positionAmt": 120.0}},
	}
	held, ok := waitOverflowFillConfirm(stub, "ASTERUSDT", "long")
	if !ok || held != 120 {
		t.Fatalf("expected confirmed fill held=120, got ok=%v held=%v calls=%d", ok, held, stub.calls)
	}

	flat := &overflowFillStub{positions: nil}
	held, ok = waitOverflowFillConfirm(flat, "ASTERUSDT", "long")
	if ok || held != 0 {
		t.Fatalf("flat exchange should not confirm, got ok=%v held=%v", ok, held)
	}
	if flat.calls < 2 {
		t.Fatalf("expected polling retries, calls=%d", flat.calls)
	}
}

func TestOverflowExchangeHeld(t *testing.T) {
	stub := &overflowFillStub{
		positions: []map[string]interface{}{
			{"symbol": "ASTERUSDT", "side": "long", "positionAmt": 50.0, "markPrice": 1.2},
		},
	}
	held, mark := overflowExchangeHeld(stub, "ASTERUSDT", "long")
	if held != 50 || mark != 1.2 {
		t.Fatalf("held=%v mark=%v", held, mark)
	}
	held, _ = overflowExchangeHeld(stub, "ASTERUSDT", "short")
	if held != 0 {
		t.Fatalf("wrong side should be flat, held=%v", held)
	}
}
