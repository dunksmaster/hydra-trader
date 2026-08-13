package telegram

import (
	"fmt"
	"math"
	"nofx/logger"
	"nofx/store"
	"nofx/telegram/agent"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type digestState struct {
	mu               sync.Mutex
	lastDailyDate    string
	lastSwingUPnL    float64
	swingInitialized bool
}

var digest digestState

// StartDigest runs daily snapshot and uPnL swing polling in the background.
func StartDigest(st *store.Store, bot *tgbotapi.BotAPI, apiPort int) {
	go runDigestLoop(st, bot, apiPort)
}

func runDigestLoop(st *store.Store, bot *tgbotapi.BotAPI, apiPort int) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		if !shouldRunDigest(st) {
			<-ticker.C
			continue
		}

		lang := st.TelegramConfig().GetLanguage()
		snap, positions, err := fetchLivePortfolio(st, apiPort)
		if err != nil {
			logger.Infof("Telegram digest: skip (fetch error): %v", err)
			<-ticker.C
			continue
		}

		chatID, _ := st.TelegramConfig().GetBoundChatID()
		now := time.Now().UTC()
		today := now.Format("2006-01-02")

		digest.mu.Lock()

		if st.TelegramConfig().IsDigestEnabled() && digest.lastDailyDate != today && now.Hour() >= 9 {
			digest.lastDailyDate = today
			digest.mu.Unlock()
			title := "☀️ <b>Daily snapshot</b>"
			if lang == "zh" {
				title = "☀️ <b>每日快照</b>"
			}
			body := formatRichPositions(snap, positions, lang)
			sendHTMLMsg(bot, chatID, title+"\n\n"+body)
			digest.mu.Lock()
		}

		if st.TelegramConfig().IsDigestEnabled() {
			threshold := st.TelegramConfig().GetPnlSwingThreshold()
			cur := snap.UnrealizedProfit
			if !digest.swingInitialized {
				digest.lastSwingUPnL = cur
				digest.swingInitialized = true
			} else if math.Abs(cur-digest.lastSwingUPnL) >= threshold {
				delta := cur - digest.lastSwingUPnL
				digest.lastSwingUPnL = cur
				digest.mu.Unlock()
				title := fmt.Sprintf("⚡ <b>Portfolio move</b> (%s)", formatMoney(delta))
				if lang == "zh" {
					title = fmt.Sprintf("⚡ <b>账户波动</b> (%s)", formatMoney(delta))
				}
				body := formatRichPositions(snap, positions, lang)
				sendHTMLMsg(bot, chatID, title+"\n\n"+body)
				<-ticker.C
				continue
			}
		}

		digest.mu.Unlock()
		<-ticker.C
	}
}

func shouldRunDigest(st *store.Store) bool {
	if !st.TelegramConfig().IsNotifyEnabled() {
		return false
	}
	chatID, err := st.TelegramConfig().GetBoundChatID()
	return err == nil && chatID != 0
}

func fetchLivePortfolio(st *store.Store, apiPort int) (AccountSnapshot, []map[string]any, error) {
	userID, err := resolveBotUserID(st)
	if err != nil {
		return AccountSnapshot{}, nil, err
	}
	jwt, err := agent.GenerateBotToken(userID)
	if err != nil {
		return AccountSnapshot{}, nil, err
	}
	client := newQuickClient(apiPort, jwt)
	return fetchAccountAndPositions(client)
}
