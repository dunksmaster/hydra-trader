package trader

import (
	"nofx/store"
	"testing"

	"nofx/kernel"
)

func TestInjectHardTakeProfitsClosesWinnersAndDropsHold(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
		RiskControl: store.RiskControlConfig{HardTakeProfitMarginPct: 15},
	}}}
	ctx := &kernel.Context{
		Positions: []kernel.PositionInfo{
			{Symbol: "HYPEUSDT", Side: "long", UnrealizedPnLPct: 28.8},
			{Symbol: "ETHUSDT", Side: "long", UnrealizedPnLPct: 10.2},
		},
	}
	got := at.injectHardTakeProfits([]kernel.Decision{
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

func TestInjectHardTakeProfitsNoOpBelowThreshold(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{
		RiskControl: store.RiskControlConfig{HardTakeProfitMarginPct: 15},
	}}}
	ctx := &kernel.Context{
		Positions: []kernel.PositionInfo{
			{Symbol: "ETHUSDT", Side: "long", UnrealizedPnLPct: 10.2},
		},
	}
	in := []kernel.Decision{{Symbol: "ETHUSDT", Action: "hold"}}
	got := at.injectHardTakeProfits(in, ctx)
	if len(got) != 1 || got[0].Action != "hold" {
		t.Fatalf("got %+v", got)
	}
}

func TestInjectHardTakeProfitsDisabledPerStrategy(t *testing.T) {
	at := &AutoTrader{config: AutoTraderConfig{StrategyConfig: &store.StrategyConfig{}}}
	ctx := &kernel.Context{Positions: []kernel.PositionInfo{
		{Symbol: "BTCUSDT", Side: "long", UnrealizedPnLPct: 99},
	}}
	got := at.injectHardTakeProfits(nil, ctx)
	if len(got) != 0 {
		t.Fatalf("disabled hard TP must not close positions: %+v", got)
	}
}
