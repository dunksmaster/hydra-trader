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
	mu            sync.Mutex
	lastDailyDate string
	lastSwingUPnL map[string]float64
	swingInit     map[string]bool
}

var digest digestState

func init() {
	digest.lastSwingUPnL = make(map[string]float64)
	digest.swingInit = make(map[string]bool)
}

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
		portfolios, err := fetchAllTraderPortfoliosDigest(st, apiPort)
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
			title := fmt.Sprintf("☀️ <b>Daily snapshot</b>  %s UTC", now.Format("15:04"))
			if lang == "zh" {
				title = fmt.Sprintf("☀️ <b>每日快照</b>  %s UTC", now.Format("15:04"))
			}
			body := formatMultiTraderSnapshot(portfolios, lang, true)
			sendHTMLMsg(bot, chatID, title+"\n\n"+body)
			digest.mu.Lock()
		}

		threshold := st.TelegramConfig().GetPnlSwingThreshold()
		swingSent := false
		for _, tp := range portfolios {
			if !tp.Info.IsRunning || tp.FetchErr != "" {
				continue
			}
			cur := tp.Snapshot.UnrealizedProfit
			id := tp.Info.TraderID
			if !digest.swingInit[id] {
				digest.lastSwingUPnL[id] = cur
				digest.swingInit[id] = true
				continue
			}
			if math.Abs(cur-digest.lastSwingUPnL[id]) >= threshold {
				delta := cur - digest.lastSwingUPnL[id]
				digest.lastSwingUPnL[id] = cur
				digest.mu.Unlock()
				title := fmt.Sprintf("⚡ <b>%s move</b> (%s)", escapeHTML(tp.Info.TraderName), formatMoney(delta))
				if lang == "zh" {
					title = fmt.Sprintf("⚡ <b>%s 波动</b> (%s)", escapeHTML(tp.Info.TraderName), formatMoney(delta))
				}
				body := formatTraderPortfolioSection(tp, lang, true)
				sendHTMLMsg(bot, chatID, title+"\n\n"+body)
				swingSent = true
				digest.mu.Lock()
			}
		}

		digest.mu.Unlock()
		if swingSent {
			<-ticker.C
			continue
		}
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

func fetchAllTraderPortfoliosDigest(st *store.Store, apiPort int) ([]TraderPortfolio, error) {
	userID, err := resolveBotUserID(st)
	if err != nil {
		return nil, err
	}
	jwt, err := agent.GenerateBotToken(userID)
	if err != nil {
		return nil, err
	}
	return fetchAllTraderPortfolios(newQuickClient(apiPort, jwt))
}
