package bitget

import (
	"fmt"
	"testing"
)

func TestIsBitgetClassicBlocked(t *testing.T) {
	if !isBitgetClassicBlocked(fmt.Errorf("Bitget API error: code=40085, msg=This API is not available for Unified Account")) {
		t.Fatal("40085 must switch to UTA v3")
	}
	if !isBitgetClassicBlocked(fmt.Errorf("unified account please use v3 api")) {
		t.Fatal("unified-account wording must switch to UTA v3")
	}
	if isBitgetClassicBlocked(fmt.Errorf("Bitget API error: code=40001, msg=invalid sign")) {
		t.Fatal("auth errors must not be treated as UTA")
	}
	if isBitgetClassicBlocked(nil) {
		t.Fatal("nil error is not classic-blocked")
	}
}

func TestUTAOrderAction(t *testing.T) {
	tests := []struct {
		name, side, tradeSide, holdSide, posSide, want string
		execPnl                                       float64
		wantOK                                        bool
	}{
		{"open long", "buy", "open", "", "", "open_long", 0, true},
		{"open short", "sell", "open", "", "", "open_short", 0, true},
		{"close long", "sell", "close", "", "", "close_long", 0, true},
		{"close short", "buy", "close", "", "", "close_short", 0, true},
		{"legacy open long", "buy", "buy_single", "long", "", "open_long", 0, true},
		{"legacy close short", "buy", "buy_single", "short", "", "close_short", 0, true},
		{"legacy open short", "sell", "sell_single", "short", "", "open_short", 0, true},
		{"legacy close long", "sell", "sell_single", "long", "", "close_long", 0, true},
		{"posSide open long", "buy", "buy_single", "", "long", "open_long", 0, true},
		{"one-way open long", "buy", "buy_single", "", "", "open_long", 0, true},
		{"one-way open short", "sell", "sell_single", "", "", "open_short", 0, true},
		{"one-way close long", "sell", "sell_single", "", "", "close_long", -1.5, true},
		{"one-way close short", "buy", "buy_single", "", "", "close_short", 2.3, true},
		{"empty side", "", "buy_single", "", "", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := utaOrderAction(tt.side, tt.tradeSide, tt.holdSide, tt.posSide, tt.execPnl)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("utaOrderAction(%q, %q, %q, %q, %v) = (%q, %v), want (%q, %v)",
					tt.side, tt.tradeSide, tt.holdSide, tt.posSide, tt.execPnl, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestParseUTAFillsWrappedList(t *testing.T) {
	raw := []byte(`{"list":[{"execId":"9","orderId":"8","symbol":"BTCUSDT","side":"buy","tradeSide":"open","holdSide":"long","execPrice":"100","execQty":"0.01","execPnl":"0","createdTime":"1","feeDetail":[{"feeCoin":"USDT","fee":"0.02"}]}],"cursor":"x"}`)
	fills, err := parseUTAFills(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(fills) != 1 || fills[0].ExecId != "9" || fills[0].HoldSide != "long" || fills[0].FeeDetail[0].Fee != "0.02" {
		t.Fatalf("fills=%+v", fills)
	}
}

func TestUTATPSLBodySendsBothPrices(t *testing.T) {
	body := utaTPSLBody("HYPEUSDT", "LONG", "58.08", "64.67", "1.67", "oid")
	if body["stopLoss"] != "58.08" || body["takeProfit"] != "64.67" {
		t.Fatalf("UTA TPSL must send both prices, got %v", body)
	}
	if body["type"] != "tpsl" || body["posSide"] != "long" {
		t.Fatalf("body=%v", body)
	}
	if body["tpslMode"] != "partial" || body["qty"] != "1.67" {
		t.Fatalf("partial qty missing: %v", body)
	}
}

func TestParseUTAStrategyOrdersArray(t *testing.T) {
	raw := []byte(`[{"orderId":"1","symbol":"ETHUSDT","qty":"2","posSide":"long","takeProfit":"4000","stopLoss":"3000"}]`)
	orders, err := parseUTAStrategyOrders(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(orders) != 1 || orders[0].TakeProfit != "4000" || orders[0].StopLoss != "3000" {
		t.Fatalf("orders=%+v", orders)
	}
}

func TestUnmarshalUTAListPositions(t *testing.T) {
	raw := []byte(`{"list":[{"symbol":"SOLUSDT","posSide":"long","total":"1.5","avgPrice":"140"}]}`)
	var positions []struct {
		Symbol   string `json:"symbol"`
		PosSide  string `json:"posSide"`
		Total    string `json:"total"`
		AvgPrice string `json:"avgPrice"`
	}
	if err := unmarshalUTAList(raw, &positions); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(positions) != 1 || positions[0].Symbol != "SOLUSDT" || positions[0].Total != "1.5" {
		t.Fatalf("positions=%+v", positions)
	}
}

func TestUnmarshalUTAListNullAndEmpty(t *testing.T) {
	var fills []utaFill
	if err := unmarshalUTAList([]byte(`{"list":null,"cursor":null}`), &fills); err != nil {
		t.Fatalf("null list should be empty: %v", err)
	}
	if len(fills) != 0 {
		t.Fatalf("got %d fills", len(fills))
	}
	if err := unmarshalUTAList([]byte(`{"list":[]}`), &fills); err != nil {
		t.Fatalf("empty list: %v", err)
	}
}

func TestIsBitgetUTAManageDenied(t *testing.T) {
	if !isBitgetUTAManageDenied(fmt.Errorf("Bitget API error: code=40014, msg=Incorrect permissions, need UTA manage read")) {
		t.Fatal("40014 must be manage-denied")
	}
	if isBitgetUTAManageDenied(fmt.Errorf("Bitget API error: code=40085, msg=use v3")) {
		t.Fatal("40085 is classic-blocked, not manage-denied")
	}
}

func TestBitgetTraderStartsClassicUntilDetected(t *testing.T) {
	var tr BitgetTrader
	if tr.useUTA() {
		t.Fatal("zero trader must not assume UTA")
	}
	tr.markUTA()
	if !tr.useUTA() {
		t.Fatal("markUTA must enable v3 routing")
	}
}

func TestUTAMarginModeIsRememberedPerSymbol(t *testing.T) {
	var tr BitgetTrader
	tr.rememberUTAMarginMode("BTCUSDT", "isolated")
	if got := tr.utaMarginMode("BTCUSDT"); got != "isolated" {
		t.Fatalf("BTC margin mode = %q, want isolated", got)
	}
	if got := tr.utaMarginMode("ETHUSDT"); got != "crossed" {
		t.Fatalf("unset margin mode = %q, want crossed", got)
	}
	tr.rememberUTAMarginMode("BTCUSDT", "crossed")
	if got := tr.utaMarginMode("BTCUSDT"); got != "crossed" {
		t.Fatalf("BTC margin mode = %q, want crossed", got)
	}
}
