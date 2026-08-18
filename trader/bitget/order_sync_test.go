package bitget

import "testing"

func TestBitgetTPSLBodyUsesTPSLPlanTypes(t *testing.T) {
	body := bitgetTPSLBody("NBISUSDT", "pos_loss", "buy", "264.06", "1", "oid")
	if body["planType"] != "pos_loss" {
		t.Fatalf("planType=%v", body["planType"])
	}
	if body["executePrice"] != "0" {
		t.Fatalf("executePrice=%v, want market 0", body["executePrice"])
	}
	if _, ok := body["side"]; ok {
		t.Fatal("TPSL body must not send place-plan-order side/tradeSide fields")
	}
	if body["holdSide"] != "buy" || body["symbol"] != "NBISUSDT" {
		t.Fatalf("body=%v", body)
	}
}

func TestBitgetTPSLQueryType(t *testing.T) {
	if got := bitgetTPSLQueryType("pos_loss"); got != "profit_loss" {
		t.Fatalf("query type %q", got)
	}
	if !bitgetTPSLMatches("pos_loss", "loss_plan") || bitgetTPSLMatches("pos_profit", "loss_plan") {
		t.Fatal("SL cancel must match pos_loss/loss_plan only")
	}
	if bitgetHoldSide("SHORT") != "sell" || bitgetHoldSide("LONG") != "buy" {
		t.Fatal("one-way holdSide must be buy/sell")
	}
	if bitgetAltHoldSide("buy") != "long" || bitgetAltHoldSide("sell") != "short" {
		t.Fatal("alt holdSide must flip to hedge long/short")
	}
}

func TestConvertSymbolRewritesXYZToUSDT(t *testing.T) {
	var tr BitgetTrader
	if got := tr.convertSymbol("xyz:NBIS"); got != "NBISUSDT" {
		t.Fatalf("xyz:NBIS → %q, want NBISUSDT", got)
	}
	if got := tr.convertSymbol("NVDAUSDT"); got != "NVDAUSDT" {
		t.Fatalf("NVDAUSDT → %q, want NVDAUSDT", got)
	}
}

func TestParseBitgetFillHistoryNullFillList(t *testing.T) {
	fills, err := parseBitgetFillHistory([]byte(`{"fillList":null,"endId":null}`))
	if err != nil {
		t.Fatalf("null fillList should be empty, not an error: %v", err)
	}
	if len(fills) != 0 {
		t.Fatalf("got %d fills, want 0", len(fills))
	}
}

func TestParseBitgetFillHistoryWrappedAndArray(t *testing.T) {
	wrapped, err := parseBitgetFillHistory([]byte(`{"fillList":[{"tradeId":"1","symbol":"BTCUSDT"}],"endId":"9"}`))
	if err != nil {
		t.Fatalf("wrapped: %v", err)
	}
	if len(wrapped) != 1 || wrapped[0].TradeID != "1" {
		t.Fatalf("wrapped = %+v", wrapped)
	}

	direct, err := parseBitgetFillHistory([]byte(`[{"tradeId":"2","symbol":"ETHUSDT"}]`))
	if err != nil {
		t.Fatalf("array: %v", err)
	}
	if len(direct) != 1 || direct[0].TradeID != "2" {
		t.Fatalf("array = %+v", direct)
	}
}
