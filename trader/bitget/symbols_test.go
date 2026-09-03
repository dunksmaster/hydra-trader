package bitget

import (
	"nofx/kernel"
	"testing"
	"time"
)

func TestNativeUSDTPerpSymbol(t *testing.T) {
	cases := map[string]string{
		"xyz:NVDA":  "NVDAUSDT",
		"xyz:SKHX":  "SKHXUSDT",
		"NVDAUSDT":  "NVDAUSDT",
		"MU":        "MUUSDT",
		"xyz:MU":    "MUUSDT",
		"xyz:SP500": "SP500USDT",
	}
	for in, want := range cases {
		if got := NativeUSDTPerpSymbol(in); got != want {
			t.Fatalf("NativeUSDTPerpSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContractCatalogResolveByBaseCoin(t *testing.T) {
	catalog := &contractCatalog{
		bySymbol: map[string]contractEntry{
			"NVDAUSDT": {Symbol: "NVDAUSDT", Base: "NVDA", IsRWA: true},
			"MUUSDT":   {Symbol: "MUUSDT", Base: "MU", IsRWA: true},
		},
		byBase: map[string]string{
			"NVDA": "NVDAUSDT",
			"MU":   "MUUSDT",
		},
	}

	got, ok := catalog.resolve("xyz:NVDA")
	if !ok || got != "NVDAUSDT" {
		t.Fatalf("resolve xyz:NVDA = %q ok=%v", got, ok)
	}

	got, ok = catalog.resolve("xyz:SKHX")
	if ok {
		t.Fatalf("expected SKHX missing on Bitget, got %q", got)
	}
}

func TestFilterCandidateCoinsUsesCatalog(t *testing.T) {
	tr := &BitgetTrader{
		contractCatalog: &contractCatalog{
			bySymbol: map[string]contractEntry{
				"NVDAUSDT": {Symbol: "NVDAUSDT", Base: "NVDA"},
				"MUUSDT":   {Symbol: "MUUSDT", Base: "MU"},
			},
			byBase: map[string]string{
				"NVDA": "NVDAUSDT",
				"MU":   "MUUSDT",
			},
		},
		tradableCatalogTime: time.Now(),
	}
	in := []kernel.CandidateCoin{
		{Symbol: "xyz:SKHX", Sources: []string{"hyper_rank"}},
		{Symbol: "xyz:NVDA", Sources: []string{"hyper_rank"}},
		{Symbol: "xyz:MU", Sources: []string{"hyper_rank"}},
	}
	out := tr.FilterCandidateCoins(in)
	if len(out) != 2 {
		t.Fatalf("filtered len=%d, want 2", len(out))
	}
	if out[0].Symbol != "NVDAUSDT" || out[1].Symbol != "MUUSDT" {
		t.Fatalf("filtered = %+v", out)
	}
}
