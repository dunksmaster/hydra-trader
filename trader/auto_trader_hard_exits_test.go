package trader

import (
	"strings"
	"testing"

	"nofx/kernel"
	"nofx/store"
)

func hardExitTrader(takeProfit, stopLoss float64) *AutoTrader {
	return &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
		RiskControl: store.RiskControlConfig{
			HardTakeProfitMarginPct: takeProfit,
			HardStopLossMarginPct:   stopLoss,
		},
	}}}
}

func TestInjectHardExitsClosesWinnersAndDropsHold(t *testing.T) {
	at := hardExitTrader(15, 0)
	ctx := &kernel.Context{
		Positions: []kernel.PositionInfo{
			{Symbol: "HYPEUSDT", Side: "long", UnrealizedPnLPct: 28.8},
			{Symbol: "ETHUSDT", Side: "long", UnrealizedPnLPct: 10.2},
		},
	}
	got := at.injectHardExits([]kernel.Decision{
		{Symbol: "HYPEUSDT", Action: "hold"},
		{Symbol: "ETHUSDT", Action: "hold"},
		{Symbol: "BTCUSDT", Action: "open_long"},
	}, ctx)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(got), got)
	}
	if got[0].Action != "close_long" || got[0].Symbol != "HYPEUSDT" {
		t.Fatalf("first should be hard TP close HYPE, got %+v", got[0])
	}
	var ethHold, btcOpen bool
	for _, d := range got {
		if d.Symbol == "ETHUSDT" && d.Action == "hold" {
			ethHold = true
		}
		if d.Symbol == "BTCUSDT" && d.Action == "open_long" {
			btcOpen = true
		}
	}
	if !ethHold || !btcOpen {
		t.Fatalf("ETH should stay hold and BTC open should remain: %+v", got)
	}
}

func TestInjectHardExitsNoOpBelowThresholds(t *testing.T) {
	at := hardExitTrader(15, 7.5)
	ctx := &kernel.Context{
		Positions: []kernel.PositionInfo{
			{Symbol: "ETHUSDT", Side: "long", UnrealizedPnLPct: 10.2},
			{Symbol: "SOLUSDT", Side: "short", UnrealizedPnLPct: -7.4},
		},
	}
	in := []kernel.Decision{{Symbol: "ETHUSDT", Action: "hold"}, {Symbol: "SOLUSDT", Action: "hold"}}
	got := at.injectHardExits(in, ctx)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	for _, d := range got {
		if d.Action != "hold" {
			t.Fatalf("nothing should be forced between the thresholds: %+v", got)
		}
	}
}

func TestInjectHardExitsDisabledPerStrategy(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{}}}
	ctx := &kernel.Context{Positions: []kernel.PositionInfo{
		{Symbol: "BTCUSDT", Side: "long", UnrealizedPnLPct: 99},
		{Symbol: "ETHUSDT", Side: "long", UnrealizedPnLPct: -99},
	}}
	got := at.injectHardExits(nil, ctx)
	if len(got) != 0 {
		t.Fatalf("disabled hard exits must not close positions: %+v", got)
	}
}

// The loss cut is the fix for average loss growing past average win: the profit
// lock caps winners, so losers must be capped too.
func TestInjectHardExitsCutsLosersOnBothSides(t *testing.T) {
	at := hardExitTrader(15, 7.5)
	ctx := &kernel.Context{Positions: []kernel.PositionInfo{
		{Symbol: "HYPEUSDT", Side: "long", UnrealizedPnLPct: -12.0},
		{Symbol: "BTCUSDT", Side: "short", UnrealizedPnLPct: -8.1},
	}}
	got := at.injectHardExits([]kernel.Decision{
		{Symbol: "HYPEUSDT", Action: "hold"},
		{Symbol: "BTCUSDT", Action: "hold"},
	}, ctx)
	if len(got) != 2 {
		t.Fatalf("both losers must be force-closed, got %+v", got)
	}
	actions := map[string]string{}
	for _, d := range got {
		actions[d.Symbol] = d.Action
		if d.Confidence != 100 {
			t.Fatalf("forced exits must carry full confidence: %+v", d)
		}
		if !strings.Contains(d.Reasoning, "stop-loss") {
			t.Fatalf("reasoning should name the stop-loss: %q", d.Reasoning)
		}
	}
	if actions["HYPEUSDT"] != "close_long" {
		t.Fatalf("long loser must close_long, got %q", actions["HYPEUSDT"])
	}
	if actions["BTCUSDT"] != "close_short" {
		t.Fatalf("short loser must close_short, got %q", actions["BTCUSDT"])
	}
}

// A strategy may run the loss cut without the profit lock, and vice versa.
func TestInjectHardExitsStopLossWorksWithoutTakeProfit(t *testing.T) {
	at := hardExitTrader(0, 6)
	ctx := &kernel.Context{Positions: []kernel.PositionInfo{
		{Symbol: "ETHUSDT", Side: "long", UnrealizedPnLPct: -9.0},
		{Symbol: "SOLUSDT", Side: "long", UnrealizedPnLPct: 500},
	}}
	got := at.injectHardExits(nil, ctx)
	if len(got) != 1 || got[0].Symbol != "ETHUSDT" || got[0].Action != "close_long" {
		t.Fatalf("only the loser should close when the profit lock is off: %+v", got)
	}
}

func TestInjectHardExitsIgnoresPositionlessContext(t *testing.T) {
	at := hardExitTrader(15, 7.5)
	in := []kernel.Decision{{Symbol: "BTCUSDT", Action: "open_long"}}
	got := at.injectHardExits(in, &kernel.Context{})
	if len(got) != 1 || got[0].Action != "open_long" {
		t.Fatalf("decisions must pass through untouched: %+v", got)
	}
}
