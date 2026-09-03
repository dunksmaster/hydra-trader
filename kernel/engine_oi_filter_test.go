package kernel

import (
	"nofx/market"
	"nofx/store"
	"testing"
)

func TestShouldFetchLegacyOITop(t *testing.T) {
	hyper := &StrategyEngine{config: &store.StrategyConfig{
		CoinSource: store.CoinSourceConfig{SourceType: "hyper_rank"},
		Indicators: store.IndicatorConfig{EnableOIRanking: true},
	}}
	if shouldFetchLegacyOITop(hyper) {
		t.Fatal("hyper_rank must not call NofxOS OI-top")
	}

	off := &StrategyEngine{config: &store.StrategyConfig{
		CoinSource: store.CoinSourceConfig{SourceType: "static"},
		Indicators: store.IndicatorConfig{EnableOIRanking: false},
	}}
	if shouldFetchLegacyOITop(off) {
		t.Fatal("OI ranking off must not call NofxOS OI-top")
	}

	on := &StrategyEngine{config: &store.StrategyConfig{
		CoinSource: store.CoinSourceConfig{SourceType: "ai500"},
		Indicators: store.IndicatorConfig{EnableOIRanking: true},
	}}
	if !shouldFetchLegacyOITop(on) {
		t.Fatal("ai500 + OI ranking should still fetch")
	}
}

func TestShouldSkipLowOpenInterest(t *testing.T) {
	low := &market.Data{
		CurrentPrice: 64000,
		OpenInterest: &market.OIData{Latest: 10}, // 0.64M USD
	}
	if !shouldSkipLowOpenInterest(low, 15) {
		t.Fatal("expected skip for real low OI")
	}

	missing := &market.Data{
		CurrentPrice: 64000,
		OpenInterest: &market.OIData{Latest: 0, Average: 0},
	}
	if shouldSkipLowOpenInterest(missing, 15) {
		t.Fatal("zero OI is a fetch fallback and must not skip")
	}

	nilOI := &market.Data{CurrentPrice: 64000}
	if shouldSkipLowOpenInterest(nilOI, 15) {
		t.Fatal("nil OI must not skip")
	}
}
