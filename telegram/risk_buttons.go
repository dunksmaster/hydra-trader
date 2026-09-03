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

const (
	riskPositionsPrefix = "riskp:"
	riskRefreshPrefix   = "riskr:"
	riskTokenTTL        = 6 * time.Hour
)

type riskTokenPayload struct {
	TraderID   string
	TraderName string
	At         time.Time
}

var riskTokens sync.Map

func riskAlertKeyboard(traderID, traderName, lang string) tgbotapi.InlineKeyboardMarkup {
	id := newCloseTokenID()
	riskTokens.Store(id, riskTokenPayload{TraderID: traderID, TraderName: traderName, At: time.Now()})
	positionsLabel, refreshLabel := "Open positions", "Refresh"
	if lang == "zh" {
		positionsLabel, refreshLabel = "查看持仓", "刷新"
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(positionsLabel, riskPositionsPrefix+id),
			tgbotapi.NewInlineKeyboardButtonData(refreshLabel, riskRefreshPrefix+id),
		),
	)
}

func lookupRiskToken(data, prefix string) (riskTokenPayload, bool) {
	id := strings.TrimPrefix(data, prefix)
	v, ok := riskTokens.Load(id)
	if !ok {
		return riskTokenPayload{}, false
	}
	p, ok := v.(riskTokenPayload)
	if !ok || time.Since(p.At) > riskTokenTTL {
		riskTokens.Delete(id)
		return riskTokenPayload{}, false
	}
	return p, true
}

func handleRiskCallback(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, st *store.Store, lang, botUserID string, apiPort int) {
	prefix := riskPositionsPrefix
	if strings.HasPrefix(query.Data, riskRefreshPrefix) {
		prefix = riskRefreshPrefix
	}
	payload, ok := lookupRiskToken(query.Data, prefix)
	if !ok {
		bot.Request(tgbotapi.NewCallback(query.ID, "Expired — wait for a new alert")) //nolint:errcheck
		return
	}
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		bot.Request(tgbotapi.NewCallback(query.ID, "Authentication failed")) //nolint:errcheck
		return
	}
	client := newQuickClient(apiPort, jwt)
	tp, err := fetchTraderPortfolioByID(st, client, payload.TraderID)
	if err != nil || tp.FetchErr != "" {
		if err == nil {
			err = fmt.Errorf("%s", tp.FetchErr)
		}
		bot.Request(tgbotapi.NewCallback(query.ID, "Refresh failed")) //nolint:errcheck
		if query.Message != nil {
			sendHTMLMsg(bot, query.Message.Chat.ID, "Could not refresh risk: "+escapeHTML(err.Error()))
		}
		return
	}
	if strings.HasPrefix(query.Data, riskPositionsPrefix) {
		bot.Request(tgbotapi.NewCallback(query.ID, "Positions updated")) //nolint:errcheck
		if query.Message != nil {
			sendPositionCards(bot, query.Message.Chat.ID, []TraderPortfolio{tp}, lang)
		}
		return
	}

	bot.Request(tgbotapi.NewCallback(query.ID, "Risk refreshed")) //nolint:errcheck
	if query.Message == nil {
		return
	}
	reason := liquidationRiskReasonForPortfolio(tp)
	var text string
	if reason == "" {
		wallet, _ := liquidationFundingDestination(payload.TraderName, lang)
		text = fmt.Sprintf("✅ <b>Liquidation risk cleared</b>\n\n┣ Bot: <b>%s</b>\n┣ Wallet: <b>%s</b>\n┗ Margin is below the alert threshold.",
			escapeHTML(payload.TraderName), escapeHTML(wallet))
	} else {
		text = formatSystemAlert(st, events.SystemAlertEvent{
			TraderID: payload.TraderID, TraderName: payload.TraderName,
			Type: events.AlertLiquidationRisk, Message: payload.TraderName + " " + reason + ". Add funds to this wallet.",
		}, lang)
	}
	edit := tgbotapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, text)
	edit.ParseMode = "HTML"
	kb := riskAlertKeyboard(payload.TraderID, payload.TraderName, lang)
	edit.ReplyMarkup = &kb
	if _, err := bot.Send(edit); err != nil {
		sendHTMLMsgWithKeyboard(bot, query.Message.Chat.ID, text, kb)
	}
}

func liquidationRiskReasonForPortfolio(tp TraderPortfolio) string {
	pct := tp.Snapshot.MarginUsedPct
	if pct <= 0 && tp.Snapshot.TotalEquity > 0 {
		used := tp.Snapshot.TotalEquity - tp.Snapshot.AvailableBalance
		if used < 0 {
			used = 0
		}
		pct = used / tp.Snapshot.TotalEquity * 100
	}
	if pct >= 80 {
		return fmt.Sprintf("margin used %.0f%% (available $%.2f)", pct, tp.Snapshot.AvailableBalance)
	}
	for _, pos := range tp.Positions {
		mark := posFloat(pos, "markPrice", "mark_price")
		liq := posFloat(pos, "liquidationPrice", "liquidation_price")
		if mark <= 0 || liq <= 0 {
			continue
		}
		dist := (mark - liq) / mark
		if dist < 0 {
			dist = -dist
		}
		if dist*100 <= 8 {
			return fmt.Sprintf("%s within %.0f%% of liquidation", posString(pos, "symbol"), dist*100)
		}
	}
	return ""
}
