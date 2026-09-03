package telegram

import (
	"fmt"
	"nofx/events"
	"nofx/store"
	"nofx/telegram/agent"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type dedupeEntry struct {
	at time.Time
}

const (
	copyFailureAlertBurstMax    = 5
	copyFailureAlertBurstWindow = 30 * time.Minute
	closeTradeAlertDedupeWindow = 90 * time.Second
)

type copyFailureBurstState struct {
	windowStart time.Time
	sent        int
}

var (
	notifierMu       sync.Mutex
	notifierDedupeMu sync.Mutex
	notifierStore    *store.Store
	notifierBot      *tgbotapi.BotAPI
	notifierAPIPort  int
	notifierDedupe   sync.Map
	copyFailureBurst copyFailureBurstState
)

// InitNotifier wires proactive trade alerts and digest watcher to the bound Telegram chat.
func InitNotifier(st *store.Store, bot *tgbotapi.BotAPI, apiPort int) {
	notifierMu.Lock()
	notifierStore = st
	notifierBot = bot
	notifierAPIPort = apiPort
	notifierMu.Unlock()

	events.OnTrade(handleTradeEvent)
	events.OnSystemAlert(handleSystemAlertEvent)
	StartDigest(st, bot, apiPort)
}

func handleSystemAlertEvent(e events.SystemAlertEvent) {
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

	// Copy mirror duplicates the richer open/close trade alert — suppress it.
	if e.Type == events.AlertCopyMirror {
		return
	}
	key := e.TraderID + ":" + e.Type
	window := 6 * time.Hour
	if e.DedupeKey != "" {
		key = e.DedupeKey
	}
	if e.Type == events.AlertCopyFailed {
		key = normalizeCopyFailureDedupeKey(e)
		if events.IsTransientCopyFailureText(e.Message) {
			return
		}
	}
	switch e.Type {
	case events.AlertSafeMode, events.AlertSafeModeOff:
		window = 2 * time.Minute
	case events.AlertCopySkipped, events.AlertCopyFailed:
		window = 30 * time.Minute
	case events.AlertCopyOverflow:
		window = 2 * time.Minute
	case events.AlertCopyLeaderRule:
		window = 30 * time.Minute
	case events.AlertCopyPaused, events.AlertCopyL2Evicted, events.AlertCopyLossPause:
		window = 5 * time.Minute
	case events.AlertLiquidationRisk:
		window = 15 * time.Minute
	case events.AlertWalletEmpty, events.AlertQuotaExhausted, events.AlertRateLimited:
		window = 30 * time.Minute
	}
	notifierDedupeMu.Lock()
	if v, ok := notifierDedupe.Load(key); ok {
		if entry, ok := v.(dedupeEntry); ok && time.Since(entry.at) < window {
			notifierDedupeMu.Unlock()
			return
		}
	}
	now := time.Now()
	if e.Type == events.AlertCopyFailed && !allowCopyFailureAlertLocked(now) {
		notifierDedupeMu.Unlock()
		return
	}
	notifierDedupe.Store(key, dedupeEntry{at: now})
	if e.Type == events.AlertSafeModeOff {
		notifierDedupe.Delete(e.TraderID + ":" + events.AlertSafeMode)
	}
	notifierDedupeMu.Unlock()

	lang := st.TelegramConfig().GetLanguage()
	text := formatSystemAlert(st, e, lang)
	if e.Type == events.AlertLiquidationRisk && e.TraderID != "" {
		sendHTMLMsgWithKeyboard(bot, chatID, text, riskAlertKeyboard(e.TraderID, e.TraderName, lang))
		return
	}
	sendHTMLMsg(bot, chatID, text)
}

// allowCopyFailureAlertLocked caps noisy operational failures globally. Real
// open/close fills use handleTradeEvent and are never subject to this cap.
// notifierDedupeMu must be held by the caller.
// normalizeCopyFailureDedupeKey collapses reason-specific keys so one BTC
// close_short burst becomes one Telegram incident regardless of emit path.
func normalizeCopyFailureDedupeKey(e events.SystemAlertEvent) string {
	if e.DedupeKey != "" {
		parts := strings.Split(e.DedupeKey, ":")
		if len(parts) >= 4 && parts[1] == events.AlertCopyFailed {
			return strings.Join(parts[:4], ":")
		}
	}
	symbol, action := parseCopyFailureMessage(e.Message)
	if symbol != "" && action != "" {
		return e.TraderID + ":" + events.AlertCopyFailed + ":" + symbol + ":" + action
	}
	return e.TraderID + ":" + events.AlertCopyFailed
}

func parseCopyFailureMessage(msg string) (symbol, action string) {
	fields := strings.Fields(strings.TrimSpace(msg))
	for i, f := range fields {
		low := strings.ToLower(f)
		if low == "open_long" || low == "open_short" || low == "close_long" || low == "close_short" {
			action = low
			if i > 0 {
				symbol = strings.ToUpper(strings.TrimSuffix(fields[i-1], ":"))
			}
			return symbol, action
		}
	}
	return "", ""
}

func allowCopyFailureAlertLocked(now time.Time) bool {
	if copyFailureBurst.windowStart.IsZero() ||
		now.Sub(copyFailureBurst.windowStart) >= copyFailureAlertBurstWindow {
		copyFailureBurst = copyFailureBurstState{windowStart: now}
	}
	if copyFailureBurst.sent >= copyFailureAlertBurstMax {
		return false
	}
	copyFailureBurst.sent++
	return true
}

func handleTradeEvent(e events.TradeEvent) {
	notifierMu.Lock()
	st := notifierStore
	bot := notifierBot
	apiPort := notifierAPIPort
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

	// Bitget (and similar) replay one logical close as many exchange fills — skip partials.
	if e.PartialClose {
		return
	}

	key, window := tradeAlertDedupeKey(e)
	if key != "" && window > 0 {
		notifierDedupeMu.Lock()
		if v, ok := notifierDedupe.Load(key); ok {
			if entry, ok := v.(dedupeEntry); ok && time.Since(entry.at) < window {
				notifierDedupeMu.Unlock()
				return
			}
		}
		notifierDedupe.Store(key, dedupeEntry{at: time.Now()})
		notifierDedupeMu.Unlock()
	}

	lang := st.TelegramConfig().GetLanguage()
	var footer *AccountSnapshot
	if strings.HasPrefix(e.Action, "close_") {
		if snap, err := fetchLiveAccountSnapshotForTrader(st, apiPort, e.TraderID); err == nil {
			footer = &snap
		}
	}

	text := formatTradeAlertRich(st, e, lang, footer)
	if kb, ok := closeKeyboardForTrade(e, lang); ok {
		sendHTMLMsgWithKeyboard(bot, chatID, text, kb)
		return
	}
	sendHTMLMsg(bot, chatID, text)
}

// tradeAlertDedupeKey collapses duplicate close alerts (immediate close + order-sync replay).
func tradeAlertDedupeKey(e events.TradeEvent) (key string, window time.Duration) {
	if strings.HasPrefix(e.Action, "close_") {
		sym := strings.ToUpper(strings.TrimSpace(e.Symbol))
		return e.TraderID + ":close:" + sym + ":" + e.Action, closeTradeAlertDedupeWindow
	}
	if e.OrderID != "" {
		return e.OrderID + ":" + e.Action, 2 * time.Minute
	}
	return "", 0
}

func fetchLiveAccountSnapshotForTrader(st *store.Store, apiPort int, traderID string) (AccountSnapshot, error) {
	userID, err := resolveBotUserID(st)
	if err != nil {
		return AccountSnapshot{}, err
	}
	jwt, err := agent.GenerateBotToken(userID)
	if err != nil {
		return AccountSnapshot{}, err
	}
	client := newQuickClient(apiPort, jwt)
	tp, err := fetchTraderPortfolioByID(st, client, traderID)
	if err != nil {
		return AccountSnapshot{}, err
	}
	if tp.FetchErr != "" {
		return AccountSnapshot{}, fmt.Errorf("%s", tp.FetchErr)
	}
	return tp.Snapshot, nil
}
