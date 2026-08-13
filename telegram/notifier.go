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

var (
	notifierMu      sync.Mutex
	notifierStore   *store.Store
	notifierBot     *tgbotapi.BotAPI
	notifierAPIPort int
	notifierDedupe  sync.Map
)

// InitNotifier wires proactive trade alerts and digest watcher to the bound Telegram chat.
func InitNotifier(st *store.Store, bot *tgbotapi.BotAPI, apiPort int) {
	notifierMu.Lock()
	notifierStore = st
	notifierBot = bot
	notifierAPIPort = apiPort
	notifierMu.Unlock()

	events.OnTrade(handleTradeEvent)
	StartDigest(st, bot, apiPort)
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

	key := e.OrderID + ":" + e.Action
	if e.OrderID != "" {
		if v, ok := notifierDedupe.Load(key); ok {
			if entry, ok := v.(dedupeEntry); ok && time.Since(entry.at) < 2*time.Minute {
				return
			}
		}
		notifierDedupe.Store(key, dedupeEntry{at: time.Now()})
	}

	lang := st.TelegramConfig().GetLanguage()
	var footer *AccountSnapshot
	if strings.HasPrefix(e.Action, "close_") {
		if snap, err := fetchLiveAccountSnapshot(st, apiPort); err == nil {
			footer = &snap
		}
	}

	text := formatTradeAlertRich(st, e, lang, footer)
	sendHTMLMsg(bot, chatID, text)
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

func fetchLiveAccountSnapshot(st *store.Store, apiPort int) (AccountSnapshot, error) {
	userID, err := resolveBotUserID(st)
	if err != nil {
		return AccountSnapshot{}, err
	}
	jwt, err := agent.GenerateBotToken(userID)
	if err != nil {
		return AccountSnapshot{}, err
	}
	client := newQuickClient(apiPort, jwt)
	return fetchAccountSnapshot(client)
}

func resolveBotUserID(st *store.Store) (string, error) {
	users, err := st.User().GetAll()
	if err == nil && len(users) > 0 {
		return users[0].ID, nil
	}
	traders, err := st.Trader().ListAll()
	if err == nil && len(traders) > 0 && traders[0].UserID != "" {
		return traders[0].UserID, nil
	}
	return "", fmt.Errorf("no user found")
}
