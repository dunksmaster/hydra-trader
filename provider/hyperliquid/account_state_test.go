package hyperliquid

import "testing"

func TestMergeAccountStates(t *testing.T) {
	main := &AccountState{
		Address: "0xabc",
		Equity:  1000,
		Legs: []AccountPosition{
			{Coin: "BTC", Symbol: "BTCUSDT", Side: "long", Size: 0.1, NotionalUSD: 5000},
		},
	}
	xyz := &AccountState{
		Address: "0xabc",
		Equity:  200,
		Legs: []AccountPosition{
			{Coin: "xyz:CRCL", Symbol: "XYZ:CRCL", Side: "short", Size: 10, NotionalUSD: 1000},
			{Coin: "BTC", Symbol: "BTCUSDT", Side: "long", Size: 0.1, NotionalUSD: 5000},
		},
	}
	merged := mergeAccountStates(main, xyz)
	if merged.Equity != 1200 {
		t.Fatalf("equity = %.2f, want 1200", merged.Equity)
	}
	if len(merged.Legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(merged.Legs))
	}
}

func TestParseClearinghouseState(t *testing.T) {
	entry := "42000"
	state := clearinghouseStateResponse{}
	state.MarginSummary.AccountValue = "1500.5"
	state.AssetPositions = []struct {
		Position struct {
			Coin          string  `json:"coin"`
			Szi           string  `json:"szi"`
			EntryPx       *string `json:"entryPx"`
			PositionValue string  `json:"positionValue"`
			Leverage      struct {
				Value int `json:"value"`
			} `json:"leverage"`
		} `json:"position"`
	}{
		{Position: struct {
			Coin          string  `json:"coin"`
			Szi           string  `json:"szi"`
			EntryPx       *string `json:"entryPx"`
			PositionValue string  `json:"positionValue"`
			Leverage      struct {
				Value int `json:"value"`
			} `json:"leverage"`
		}{Coin: "ETH", Szi: "-1.5", EntryPx: &entry, PositionValue: "63000", Leverage: struct {
			Value int `json:"value"`
		}{Value: 5}}},
	}
	out := parseClearinghouseState("0xleader", state)
	if out.Equity != 1500.5 {
		t.Fatalf("equity = %.2f", out.Equity)
	}
	if len(out.Legs) != 1 || out.Legs[0].Side != "short" || out.Legs[0].Size != 1.5 {
		t.Fatalf("unexpected leg: %+v", out.Legs[0])
	}
}
