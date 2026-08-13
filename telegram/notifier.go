package telegram

import (
	"fmt"
	"math"
	"nofx/events"
	"nofx/store"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type dedupeEntry struct {
	at time.Time
}

var (
	notifierMu     sync.Mutex
	notifierStore  *store.Store
	notifierBot    *tgbotapi.BotAPI
	notifierDedupe sync.Map
)

// InitNotifier wires proactive trade alerts to the bound Telegram chat.
func InitNotifier(st *store.Store, bot *tgbotapi.BotAPI) {
	notifierMu.Lock()
	notifierStore = st
	notifierBot = bot
	notifierMu.Unlock()

	events.OnTrade(handleTradeEvent)
}

func handleTradeEvent(e events.TradeEvent) {
	notifierMu.Lock()
	st := notifierStore
	bot := notifierBot
	notifierMu.Unlock()
	if st == nil || bot == nil {
		return
	}

	if !st.TelegramConfig().IsNotifyEnabled() {
		return
	}

	chatID, err := st.TelegramConfig().GetBoundChatID()
	if err != nil || chatID == 0 {
		return
	}

	key := e.OrderID + ":" + e.Action
	if e.OrderID != "" {
		if v, ok := notifierDedupe.Load(key); ok {
			if entry, ok := v.(dedupeEntry); ok && time.Since(entry.at) < 2*time.Minute {
				return
			}
		}
		notifierDedupe.Store(key, dedupeEntry{at: time.Now()})
	}

	text := formatTradeAlert(st, e)
	sendMarkdownMsg(bot, chatID, text)
}

func formatTradeAlert(st *store.Store, e events.TradeEvent) string {
	traderName := lookupTraderName(st, e.TraderID)
	symbol := strings.TrimSuffix(strings.ToUpper(e.Symbol), "USDT")
	if symbol == "" {
		symbol = e.Symbol
	}

	actionLabel := tradeActionLabel(e)
	qty := formatQty(e.Quantity)
	price := formatPrice(e.Price)

	lines := []string{actionLabel, fmt.Sprintf("%s %s · %s @ %s", e.Side, symbol, qty, price)}
	if traderName != "" {
		lines = append(lines, fmt.Sprintf("_Trader:_ %s", traderName))
	}
	if e.PartialClose {
		lines[0] = "📉 *Partial close*"
	}
	if strings.HasPrefix(e.Action, "close_") && !e.PartialClose {
		pnl := e.RealizedPnL
		sign := "+"
		if pnl < 0 {
			sign = ""
		}
		lines = append(lines, fmt.Sprintf("*PnL:* %s$%.2f", sign, pnl))
	}
	return strings.Join(lines, "\n")
}

func tradeActionLabel(e events.TradeEvent) string {
	switch e.Action {
	case "open_long":
		return "📈 *Position opened* (LONG)"
	case "open_short":
		return "📉 *Position opened* (SHORT)"
	case "close_long", "close_short":
		return "✅ *Position closed*"
	default:
		if strings.HasPrefix(e.Action, "open_") {
			return "📈 *Position opened*"
		}
		return "✅ *Trade update*"
	}
}

func lookupTraderName(st *store.Store, traderID string) string {
	if traderID == "" {
		return ""
	}
	traders, err := st.Trader().ListAll()
	if err != nil {
		return ""
	}
	for _, t := range traders {
		if t.ID == traderID {
			return t.Name
		}
	}
	if len(traderID) >= 8 {
		return traderID[:8]
	}
	return traderID
}

func formatQty(q float64) string {
	if q == 0 {
		return "0"
	}
	if q >= 1 {
		return fmt.Sprintf("%.4f", q)
	}
	return fmt.Sprintf("%.6f", q)
}

func formatPrice(p float64) string {
	if p == 0 {
		return "$0"
	}
	if p >= 100 {
		return fmt.Sprintf("$%.2f", p)
	}
	if p >= 1 {
		return fmt.Sprintf("$%.4f", p)
	}
	decimals := int(math.Ceil(-math.Log10(p))) + 2
	if decimals < 2 {
		decimals = 2
	}
	if decimals > 8 {
		decimals = 8
	}
	return fmt.Sprintf("$%.*f", decimals, p)
}
