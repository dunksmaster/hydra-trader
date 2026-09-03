package trader

import (
	"nofx/events"
	"nofx/provider/hyperliquid"
	"nofx/store"
	"nofx/trader/types"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestComputeCopyCloseQtyProportional(t *testing.T) {
	cfg := store.DefaultCopyStrategyConfig()
	cfg.SizeMode = "proportional"
	cfg.CopyRatio = 1.0
	fill := hyperliquid.LeaderFill{Size: 1.0, Price: 100, NotionalUSD: 100}
	qty, reason := computeCopyCloseQty(&cfg, fill, 1000, 100, nil, "BTCUSDT", "close_long")
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if qty != 0.1 {
		t.Fatalf("qty = %.4f, want 0.1", qty)
	}
}

type copyCloseMockTrader struct {
	positions []map[string]interface{}
}

func (m *copyCloseMockTrader) GetPositions() ([]map[string]interface{}, error) {
	return m.positions, nil
}

func (m *copyCloseMockTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	return strconv.FormatFloat(quantity, 'f', 4, 64), nil
}

func (m *copyCloseMockTrader) GetBalance() (map[string]interface{}, error) { return nil, nil }
func (m *copyCloseMockTrader) OpenLong(string, float64, int) (map[string]interface{}, error) {
	return nil, nil
}
func (m *copyCloseMockTrader) OpenShort(string, float64, int) (map[string]interface{}, error) {
	return nil, nil
}
func (m *copyCloseMockTrader) CloseLong(string, float64) (map[string]interface{}, error) {
	return nil, nil
}
func (m *copyCloseMockTrader) CloseShort(string, float64) (map[string]interface{}, error) {
	return nil, nil
}
func (m *copyCloseMockTrader) SetLeverage(string, int) error                          { return nil }
func (m *copyCloseMockTrader) SetMarginMode(string, bool) error                       { return nil }
func (m *copyCloseMockTrader) GetMarketPrice(string) (float64, error)                 { return 0, nil }
func (m *copyCloseMockTrader) SetStopLoss(string, string, float64, float64) error     { return nil }
func (m *copyCloseMockTrader) SetTakeProfit(string, string, float64, float64) error   { return nil }
func (m *copyCloseMockTrader) CancelStopLossOrders(string) error                      { return nil }
func (m *copyCloseMockTrader) CancelTakeProfitOrders(string) error                    { return nil }
func (m *copyCloseMockTrader) CancelAllOrders(string) error                           { return nil }
func (m *copyCloseMockTrader) CancelStopOrders(string) error                          { return nil }
func (m *copyCloseMockTrader) GetOrderStatus(string, string) (map[string]interface{}, error) {
	return nil, nil
}
func (m *copyCloseMockTrader) GetClosedPnL(time.Time, int) ([]types.ClosedPnLRecord, error) {
	return nil, nil
}
func (m *copyCloseMockTrader) GetOpenOrders(string) ([]types.OpenOrder, error) { return nil, nil }

func TestComputeCopyCloseQtyCloseAllWhenNearFull(t *testing.T) {
	cfg := store.DefaultCopyStrategyConfig()
	cfg.SizeMode = "proportional"
	cfg.CopyRatio = 1.0
	fill := hyperliquid.LeaderFill{Size: 1000, Price: 0.004, NotionalUSD: 4}
	mock := &copyCloseMockTrader{
		positions: []map[string]interface{}{
			{"symbol": "PUMPUSDT", "side": "long", "positionAmt": 1000.0},
		},
	}
	qty, reason := computeCopyCloseQty(&cfg, fill, 1000, 1000, mock, "PUMPUSDT", "close_long")
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if qty != 0 {
		t.Fatalf("qty = %.4f, want 0 (close all)", qty)
	}
}

func TestComputeCopyCloseQtyDustPartialClosesAll(t *testing.T) {
	cfg := store.DefaultCopyStrategyConfig()
	cfg.SizeMode = "proportional"
	cfg.CopyRatio = 0.01
	fill := hyperliquid.LeaderFill{Size: 100, Price: 0.05, NotionalUSD: 5}
	mock := &copyCloseMockTrader{
		positions: []map[string]interface{}{
			{"symbol": "PUMPUSDT", "side": "long", "positionAmt": 5000.0},
		},
	}
	qty, reason := computeCopyCloseQty(&cfg, fill, 10000, 100, mock, "PUMPUSDT", "close_long")
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if qty != 0 {
		t.Fatalf("dust partial should close all, got qty=%.4f", qty)
	}
}

func TestFlipCopyAction(t *testing.T) {
	if flipCopyAction("open_long") != "open_short" {
		t.Fatal("flip open_long failed")
	}
	if flipCopyAction("close_short") != "close_long" {
		t.Fatal("flip close_short failed")
	}
}

func TestIsCopyFillsModeDefault(t *testing.T) {
	copyCfg := store.DefaultCopyStrategyConfig()
	copyCfg.CopyMode = "fills"
	at := &AutoTrader{
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				StrategyType: "copy_trading",
				CopyConfig:   &copyCfg,
			},
		},
	}
	if !at.IsCopyFillsMode() {
		t.Fatal("expected fills mode")
	}
	at.config.StrategyConfig.CopyConfig.CopyMode = "snapshot"
	if at.IsCopyFillsMode() {
		t.Fatal("expected snapshot mode")
	}
}

func TestRunReentryGuard(t *testing.T) {
	at := &AutoTrader{id: "test"}
	at.isRunning = true
	err := at.Run()
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
}

func TestFilterDriftCopyLegsGrace(t *testing.T) {
	now := time.Unix(1000, 0)
	grace := 90 * time.Second
	opens := []copyLeg{{Symbol: "ETHUSDT", Side: "long", NotionalUSD: 25, Reason: "drift"}}
	closes := []copyLeg{{Symbol: "BTCUSDT", Side: "short", Reason: "drift close"}}

	// First tick: tracked but not executed
	execOpens, execCloses, drift, extra := filterDriftCopyLegs(opens, closes, nil, nil, now, grace)
	if len(execOpens) != 0 || len(execCloses) != 0 {
		t.Fatalf("expected no exec on first tick, got opens=%d closes=%d", len(execOpens), len(execCloses))
	}
	if len(drift) != 1 || len(extra) != 1 {
		t.Fatalf("expected drift tracking maps populated")
	}

	// Before grace: still no exec
	execOpens, execCloses, _, _ = filterDriftCopyLegs(opens, closes, drift, extra, now.Add(60*time.Second), grace)
	if len(execOpens) != 0 || len(execCloses) != 0 {
		t.Fatalf("expected no exec before grace elapsed")
	}

	// After grace: execute
	execOpens, execCloses, _, _ = filterDriftCopyLegs(opens, closes, drift, extra, now.Add(grace), grace)
	if len(execOpens) != 1 || len(execCloses) != 1 {
		t.Fatalf("expected exec after grace, got opens=%d closes=%d", len(execOpens), len(execCloses))
	}
}

func TestRequiredCopyMarginUSD(t *testing.T) {
	m := requiredCopyMarginUSD(50, 10)
	if m < 5.0 || m > 5.2 {
		t.Fatalf("margin for $50 @10x = %.4f, want ~5.05", m)
	}
}

func TestClassifyCopySkipCategory(t *testing.T) {
	if classifyCopySkipCategory("notional 8.00 below minimum 12.00") != "margin" {
		t.Fatal("expected margin category")
	}
	if classifyCopySkipCategory("max_positions=1 reached") != "max_positions" {
		t.Fatal("expected max_positions category")
	}
}

func TestIsCopyWatchOnly(t *testing.T) {
	cfg := store.DefaultCopyStrategyConfig()
	cfg.CopyLayer = 3
	cfg.CopyPaused = true
	cfg.DryRun = true
	if !isCopyWatchOnly(&cfg) {
		t.Fatal("L3 paused dry_run should be watch-only")
	}
	cfg.DryRun = false
	if isCopyWatchOnly(&cfg) {
		t.Fatal("paused L3 without dry_run is not watch-only")
	}
	if !suppressCopyNoiseAlerts(&cfg) {
		t.Fatal("paused L3 should still suppress noise alerts")
	}
}

func TestCopyPausedSkipsOpen(t *testing.T) {
	copyCfg := store.DefaultCopyStrategyConfig()
	copyCfg.CopyLayer = 3
	copyCfg.CopyPaused = true
	at := &AutoTrader{
		id:   "paused-bot",
		name: "Money Printer",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				StrategyType: "copy_trading",
				CopyConfig:   &copyCfg,
			},
		},
	}
	done := make(chan events.SystemAlertEvent, 1)
	events.OnSystemAlert(func(e events.SystemAlertEvent) {
		done <- e
	})
	fill := hyperliquid.LeaderFill{Tid: 1, Symbol: "BTCUSDT", NotionalUSD: 100, Action: "open_long"}
	at.emitCopyPausedSkipAlert(fill, "BTCUSDT", "open_long")
	select {
	case got := <-done:
		if got.Type != events.AlertCopyPaused {
			t.Fatalf("type = %q, want copy_paused", got.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for paused alert")
	}
}

func TestCountAllWalletLegsOpenPath(t *testing.T) {
	positions := []map[string]interface{}{
		{"symbol": "A", "positionAmt": 1.0},
		{"symbol": "B", "positionAmt": 2.0},
		{"symbol": "C", "positionAmt": 3.0},
		{"symbol": "D", "positionAmt": 4.0},
		{"symbol": "E", "positionAmt": 5.0},
	}
	if countAllWalletLegs(positions) != 5 {
		t.Fatalf("expected 5 legs for full wallet")
	}
}

func TestEmitCopySkipAlert(t *testing.T) {
	done := make(chan events.SystemAlertEvent, 1)
	events.OnSystemAlert(func(e events.SystemAlertEvent) {
		done <- e
	})
	copyCfg := store.DefaultCopyStrategyConfig()
	copyCfg.LeaderAddress = "0xabc123def456"
	at := &AutoTrader{
		id:   "trader-1",
		name: "Copy Bot",
		config: AutoTraderConfig{
			StrategyConfig: &store.StrategyConfig{
				StrategyType: "copy_trading",
				CopyConfig:   &copyCfg,
			},
		},
	}
	fill := hyperliquid.LeaderFill{Tid: 999, Symbol: "XYZ:TSLA", NotionalUSD: 500}
	at.emitCopySkipAlert(fill, "XYZ:TSLA", "open_long", "insufficient margin: need $6.00, available $3.00", "margin", 3.0)

	select {
	case got := <-done:
		if got.Type != events.AlertCopySkipped {
			t.Fatalf("type = %q, want copy_skipped", got.Type)
		}
		if !strings.Contains(got.Message, "XYZ:TSLA") || !strings.Contains(got.Message, "insufficient margin") {
			t.Fatalf("message = %q", got.Message)
		}
		if got.DedupeKey != "trader-1:"+events.AlertCopySkipped+":margin" {
			t.Fatalf("dedupe key = %q", got.DedupeKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for alert")
	}
}
