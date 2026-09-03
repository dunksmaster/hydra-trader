package telegram

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"nofx/events"
	"nofx/logger"
	"nofx/store"
	"nofx/telegram/agent"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	closeTokenPrefix   = "cl:"
	closeCommandPrefix = "close_"
	closeTokenTTL      = 6 * time.Hour
)

type closeTokenPayload struct {
	TraderID string
	Symbol   string
	Side     string
	At       time.Time
}

var closeTokens sync.Map

func mintCloseTokenID(traderID, symbol, side string) string {
	id := newCloseTokenID()
	closeTokens.Store(id, closeTokenPayload{
		TraderID: traderID,
		Symbol:   symbol,
		Side:     strings.ToLower(strings.TrimSpace(side)),
		At:       time.Now(),
	})
	return id
}

func mintCloseToken(traderID, symbol, side string) string {
	return closeTokenPrefix + mintCloseTokenID(traderID, symbol, side)
}

func newCloseTokenID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	id := fmt.Sprintf("%x", time.Now().UnixNano())
	if len(id) > 16 {
		return id[len(id)-16:]
	}
	return id
}

func lookupCloseToken(data string) (closeTokenPayload, bool) {
	id := parseCloseTokenID(data)
	if id == "" {
		return closeTokenPayload{}, false
	}
	v, ok := closeTokens.Load(id)
	if !ok {
		return closeTokenPayload{}, false
	}
	p, ok := v.(closeTokenPayload)
	if !ok || time.Since(p.At) > closeTokenTTL {
		closeTokens.Delete(id)
		return closeTokenPayload{}, false
	}
	return p, true
}

func parseCloseTokenID(data string) string {
	data = strings.ToLower(strings.TrimSpace(data))
	if fields := strings.Fields(data); len(fields) > 0 {
		data = fields[0]
	}
	if i := strings.IndexByte(data, '@'); i >= 0 {
		data = data[:i]
	}
	data = strings.TrimPrefix(data, "/")
	switch {
	case strings.HasPrefix(data, closeTokenPrefix):
		data = strings.TrimPrefix(data, closeTokenPrefix)
	case strings.HasPrefix(data, closeCommandPrefix):
		data = strings.TrimPrefix(data, closeCommandPrefix)
	default:
		return ""
	}
	if data == "" || len(data) > 24 {
		return ""
	}
	for _, r := range data {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return ""
		}
	}
	return data
}

func closeCommand(tokenID string) string {
	return "/" + closeCommandPrefix + tokenID
}

func closeButtonKeyboard(traderID, symbol, side, lang string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(closeButtonRow(traderID, symbol, side, "", "", lang))
}

func closeButtonRow(traderID, symbol, side, traderName, pnlLabel, lang string) []tgbotapi.InlineKeyboardButton {
	tokenID := mintCloseTokenID(traderID, symbol, side)
	return closeButtonRowWithToken(symbol, side, traderName, pnlLabel, lang, tokenID)
}

func closeButtonRowWithToken(symbol, side, traderName, pnlLabel, lang, tokenID string) []tgbotapi.InlineKeyboardButton {
	label := "Close " + displaySymbol(symbol) + " " + strings.ToUpper(side)
	if lang == "zh" {
		label = "平仓 " + displaySymbol(symbol) + " " + strings.ToUpper(side)
	}
	if strings.HasPrefix(strings.ToLower(symbol), "xyz:") {
		label = "Close " + symbol + " " + strings.ToUpper(side)
	}
	if traderName != "" {
		label += " (" + traderName + ")"
	}
	if pnlLabel != "" {
		label += " · " + pnlLabel
	}
	if len([]rune(label)) > 64 {
		label = "Close " + displaySymbol(symbol)
		if strings.HasPrefix(strings.ToLower(symbol), "xyz:") {
			label = "Close " + symbol
		}
		if pnlLabel != "" {
			label += " · " + pnlLabel
		}
	}
	return tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData(label, closeTokenPrefix+tokenID),
	)
}

func sendHTMLMsgWithKeyboard(bot *tgbotapi.BotAPI, chatID int64, html string, kb tgbotapi.InlineKeyboardMarkup) {
	html = strings.TrimSpace(html)
	if html == "" {
		logger.Warnf("Telegram: refusing to send empty HTML+keyboard message to chat %d", chatID)
		return
	}
	msg := tgbotapi.NewMessage(chatID, html)
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = kb
	if _, err := bot.Send(msg); err != nil {
		logger.Warnf("Telegram HTML+keyboard send failed (chat %d): %v — falling back to plain text", chatID, err)
		plain := tgbotapi.NewMessage(chatID, stripHTML(html))
		plain.ReplyMarkup = kb
		if _, plainErr := bot.Send(plain); plainErr != nil {
			logger.Errorf("Telegram plain+keyboard send failed (chat %d): %v", chatID, plainErr)
			sendHTMLMsg(bot, chatID, html)
		}
	}
}

func sendPositionCards(bot *tgbotapi.BotAPI, chatID int64, portfolios []TraderPortfolio, lang string) {
	groups := groupPositionsByVenue(portfolios)
	total := countVenuePositions(groups)
	if total == 0 && len(portfolios) == 0 {
		if lang == "zh" {
			sendMsg(bot, chatID, "📈 暂无交易员。")
		} else {
			sendMsg(bot, chatID, "📈 No traders configured.")
		}
		return
	}
	sendHTMLMsg(bot, chatID, formatPositionsHeader(total, lang))

	sentCards := 0
	for _, g := range groups {
		if len(g.Positions) == 0 {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("🤖 <b>%s</b>", escapeHTML(g.Label)))
			if len(g.FetchErrs) > 0 {
				sb.WriteString("\n└ ⚠️ ")
				sb.WriteString(escapeHTML(g.FetchErrs[0]))
			} else if total == 0 {
				if lang == "zh" {
					sb.WriteString("\n└ 暂无持仓。")
				} else {
					sb.WriteString("\n└ No open positions.")
				}
			}
			if sb.Len() > 0 && (len(g.FetchErrs) > 0 || total == 0) {
				sendHTMLMsg(bot, chatID, sb.String())
			}
			continue
		}
		for _, vp := range g.Positions {
			pnl := formatPnLMoney(posFloat(vp.Raw, "unrealized_pnl", "unRealizedProfit")) +
				" (" + formatPct(posFloat(vp.Raw, "unrealized_pnl_pct")) + ")"
			body := fmt.Sprintf("🤖 <b>%s</b>\n", escapeHTML(g.Label)) +
				formatPositionBlock(vp.Raw, lang, "")
			if vp.CloseTraderID == "" {
				sendHTMLMsg(bot, chatID, body)
				sentCards++
				time.Sleep(80 * time.Millisecond)
				continue
			}
			tokenID := mintCloseTokenID(vp.CloseTraderID, vp.Symbol, vp.Side)
			body += "\nClose: " + closeCommand(tokenID)
			kb := tgbotapi.NewInlineKeyboardMarkup(
				closeButtonRowWithToken(vp.Symbol, vp.Side, "", pnl, lang, tokenID),
			)
			sendHTMLMsgWithKeyboard(bot, chatID, body, kb)
			sentCards++
			time.Sleep(80 * time.Millisecond)
		}
	}
	if sentCards == 0 && total == 0 {
		if lang == "zh" {
			sendMsg(bot, chatID, "📈 暂无持仓。")
		} else {
			sendMsg(bot, chatID, "📈 No open positions.")
		}
	}
}

func sendOrdersCards(bot *tgbotapi.BotAPI, chatID int64, portfolios []TraderPortfolio, rows map[string][]map[string]any, lang string) {
	body := formatOrdersReport(portfolios, rows, lang)
	if strings.TrimSpace(body) == "" {
		if lang == "zh" {
			sendMsg(bot, chatID, "📋 暂无订单。")
		} else {
			sendMsg(bot, chatID, "📋 No orders yet.")
		}
		return
	}
	kb := buildPositionCloseKeyboard(portfolios, lang)
	if len(kb.InlineKeyboard) == 0 {
		sendHTMLMsg(bot, chatID, body)
		return
	}
	if len(body) > 3500 {
		sendHTMLMsg(bot, chatID, body)
		sendHTMLMsgWithKeyboard(bot, chatID, "Close open positions:", kb)
		return
	}
	sendHTMLMsgWithKeyboard(bot, chatID, body, kb)
}

func buildPositionCloseKeyboard(portfolios []TraderPortfolio, lang string) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, 8)
	for _, g := range groupPositionsByVenue(portfolios) {
		for _, vp := range g.Positions {
			if vp.CloseTraderID == "" {
				continue
			}
			if len(rows) >= 8 {
				return tgbotapi.NewInlineKeyboardMarkup(rows...)
			}
			pnl := formatPnLMoney(posFloat(vp.Raw, "unrealized_pnl", "unRealizedProfit")) +
				" (" + formatPct(posFloat(vp.Raw, "unrealized_pnl_pct")) + ")"
			tokenID := mintCloseTokenID(vp.CloseTraderID, vp.Symbol, vp.Side)
			rows = append(rows, closeButtonRowWithToken(vp.Symbol, vp.Side, "", pnl, lang, tokenID))
		}
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func closeKeyboardForTrade(e events.TradeEvent, lang string) (tgbotapi.InlineKeyboardMarkup, bool) {
	if !strings.HasPrefix(e.Action, "open_") || e.TraderID == "" || e.Symbol == "" {
		return tgbotapi.InlineKeyboardMarkup{}, false
	}
	side := e.Side
	if side == "" {
		if strings.Contains(e.Action, "short") {
			side = "short"
		} else {
			side = "long"
		}
	}
	return closeButtonKeyboard(e.TraderID, e.Symbol, side, lang), true
}

func handleCloseTokenCommand(bot *tgbotapi.BotAPI, chatID int64, cmd, botUserID string, apiPort int) {
	payload, ok := lookupCloseToken(cmd)
	if !ok {
		sendMsg(bot, chatID, "Close link expired — resend /positions.")
		return
	}
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Resend /positions and try again.")
		return
	}
	sym := displaySymbol(payload.Symbol)
	if strings.HasPrefix(strings.ToLower(payload.Symbol), "xyz:") {
		sym = payload.Symbol
	}
	if err := closePositionAPI(newQuickClient(apiPort, jwt), payload.TraderID, payload.Symbol, payload.Side); err != nil {
		sendHTMLMsg(bot, chatID, fmt.Sprintf("❌ %s %s — %s",
			escapeHTML(sym), strings.ToUpper(payload.Side), escapeHTML(err.Error())))
		return
	}
	sendHTMLMsg(bot, chatID, fmt.Sprintf("✅ <b>%s %s</b> closed",
		escapeHTML(sym), strings.ToUpper(payload.Side)))
}

func handleCloseCallback(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, st *store.Store, lang, botUserID string, apiPort int) {
	payload, ok := lookupCloseToken(query.Data)
	if !ok {
		cb := tgbotapi.NewCallback(query.ID, "Expired — send /positions")
		bot.Request(cb) //nolint:errcheck
		return
	}
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		cb := tgbotapi.NewCallback(query.ID, "Auth failed")
		bot.Request(cb) //nolint:errcheck
		return
	}
	err = closePositionAPI(newQuickClient(apiPort, jwt), payload.TraderID, payload.Symbol, payload.Side)
	sym := displaySymbol(payload.Symbol)
	if strings.HasPrefix(strings.ToLower(payload.Symbol), "xyz:") {
		sym = payload.Symbol
	}
	if err != nil {
		cb := tgbotapi.NewCallback(query.ID, "Close failed")
		bot.Request(cb) //nolint:errcheck
		if query.Message != nil {
			sendHTMLMsg(bot, query.Message.Chat.ID, fmt.Sprintf("❌ %s %s — %s",
				escapeHTML(sym), strings.ToUpper(payload.Side), escapeHTML(err.Error())))
		}
		return
	}
	cb := tgbotapi.NewCallback(query.ID, "Closed")
	bot.Request(cb) //nolint:errcheck
	if query.Message == nil {
		return
	}
	text := fmt.Sprintf("✅ <b>%s %s</b> closed", escapeHTML(sym), strings.ToUpper(payload.Side))
	edit := tgbotapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, text)
	edit.ParseMode = "HTML"
	empty := tgbotapi.InlineKeyboardMarkup{}
	edit.ReplyMarkup = &empty
	if _, err := bot.Send(edit); err != nil {
		sendHTMLMsg(bot, query.Message.Chat.ID, text)
	}
	_ = st
	_ = lang
}
