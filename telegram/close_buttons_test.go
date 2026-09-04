package telegram

import (
	"nofx/events"
	"strings"
	"testing"
	"time"
)

func TestMintCloseTokenRoundTrip(t *testing.T) {
	data := mintCloseToken("trader-abc", "ZECUSDT", "LONG")
	if !strings.HasPrefix(data, closeTokenPrefix) {
		t.Fatalf("token %q missing prefix", data)
	}
	if len(data) > 64 {
		t.Fatalf("callback data %d bytes exceeds Telegram 64-byte limit", len(data))
	}
	p, ok := lookupCloseToken(data)
	if !ok {
		t.Fatal("lookup failed")
	}
	if p.TraderID != "trader-abc" || p.Symbol != "ZECUSDT" || p.Side != "long" {
		t.Fatalf("payload = %+v", p)
	}
}

func TestLookupCloseTokenExpiredOrMissing(t *testing.T) {
	if _, ok := lookupCloseToken("cl:missing"); ok {
		t.Fatal("missing token should fail")
	}
	id := newCloseTokenID()
	closeTokens.Store(id, closeTokenPayload{TraderID: "x", Symbol: "BTC", Side: "long", At: time.Now().Add(-7 * time.Hour)})
	if _, ok := lookupCloseToken(closeTokenPrefix + id); ok {
		t.Fatal("expired token should fail")
	}
}

func TestCloseButtonKeyboardUsesShortCallback(t *testing.T) {
	kb := closeButtonKeyboard("long-trader-id-that-would-overflow-telegram-callback-limit", "HYPEUSDT", "short", "en")
	if len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("keyboard = %+v", kb.InlineKeyboard)
	}
	btn := kb.InlineKeyboard[0][0]
	if btn.Text != "Close HYPE SHORT" {
		t.Fatalf("label = %q", btn.Text)
	}
	if btn.CallbackData == nil || !strings.HasPrefix(*btn.CallbackData, closeTokenPrefix) {
		t.Fatalf("callback = %v", btn.CallbackData)
	}
	if len(*btn.CallbackData) > 64 {
		t.Fatalf("callback too long: %d", len(*btn.CallbackData))
	}
}

func TestCloseKeyboardForTradeOpenOnly(t *testing.T) {
	_, ok := closeKeyboardForTrade(events.TradeEvent{Action: "close_long", TraderID: "t", Symbol: "BTC"}, "en")
	if ok {
		t.Fatal("close alerts must not get a Close button")
	}
	kb, ok := closeKeyboardForTrade(events.TradeEvent{Action: "open_short", TraderID: "t", Symbol: "SOLUSDT", Side: "SHORT"}, "en")
	if !ok {
		t.Fatal("open alerts should get a Close button")
	}
	if kb.InlineKeyboard[0][0].Text != "Close SOL SHORT" {
		t.Fatalf("label = %q", kb.InlineKeyboard[0][0].Text)
	}
}

func TestIsQuickCommandOrderAlias(t *testing.T) {
	if !isQuickCommand("/order") || !isQuickCommand("/orders") {
		t.Fatal("/orders should be a quick command")
	}
	if !isQuickCommand("/close_ab12cd34") {
		t.Fatal("/close_<token> should be a quick command")
	}
	token := mintCloseToken("trader-abc", "BTCUSDT", "long")
	if !isQuickCommand("/"+strings.TrimPrefix(token, closeTokenPrefix)) && !isQuickCommand(closeCommand(strings.TrimPrefix(token, closeTokenPrefix))) {
		// isQuickCommand expects /close_<id>
		id := strings.TrimPrefix(token, closeTokenPrefix)
		if !isQuickCommand("/close_" + id) {
			t.Fatalf("/close_%s should be quick command", id)
		}
	}
}
