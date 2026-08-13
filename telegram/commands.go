package telegram

import (
	"fmt"
	"io"
	"net/http"
	"nofx/events"
	"nofx/store"
	"nofx/telegram/agent"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type quickClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newQuickClient(apiPort int, jwt string) *quickClient {
	return &quickClient{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", apiPort),
		token:   jwt,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *quickClient) get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func handleQuickCommand(bot *tgbotapi.BotAPI, chatID int64, cmd string, st *store.Store, botUserID string, apiPort int) {
	lang := st.TelegramConfig().GetLanguage()
	base := normalizeQuickCommand(cmd)

	switch base {
	case "notify", "njoftimet", "njoftime":
		handleNotifyCommand(bot, chatID, cmd, st, lang, botUserID, apiPort)
		return
	case "use":
		handleUseCommand(bot, chatID, cmd, st, lang, botUserID, apiPort)
		return
	case "traders", "tregtar":
		jwt, err := agent.GenerateBotToken(botUserID)
		if err != nil {
			sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
			return
		}
		sendTradersPicker(bot, chatID, st, lang, newQuickClient(apiPort, jwt))
		return
	case "weblogin", "web":
		handleWebLoginCommand(bot, chatID, st, botUserID)
		return
	}

	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	client := newQuickClient(apiPort, jwt)

	switch base {
	case "balance", "balanca", "balanc":
		portfolios, err := fetchPortfoliosForSelection(st, client)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch balance: "+err.Error())
			return
		}
		sendHTMLMsg(bot, chatID, formatBalanceForPortfolios(portfolios, lang))
	case "positions", "pozicione", "pozicionet", "pos":
		portfolios, err := fetchPortfoliosForSelection(st, client)
		if err != nil {
			sendMsg(bot, chatID, "Could not fetch positions: "+err.Error())
			return
		}
		sendHTMLMsg(bot, chatID, formatPositionsForPortfolios(portfolios, lang))
	default:
		return
	}
}

func normalizeQuickCommand(cmd string) string {
	cmd = strings.TrimSpace(strings.ToLower(cmd))
	cmd = strings.TrimPrefix(cmd, "/")
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return cmd
}

func isQuickCommand(text string) bool {
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	base := normalizeQuickCommand(fields[0])
	switch base {
	case "balance", "balanca", "balanc", "positions", "pozicione", "pozicionet", "pos",
		"traders", "tregtar", "notify", "njoftimet", "njoftime", "use", "weblogin", "web":
		return true
	}
	return false
}

func matchNLQuickIntent(text string) string {
	norm := strings.ToLower(strings.TrimSpace(text))
	norm = strings.TrimSuffix(norm, "?")
	norm = strings.TrimSuffix(norm, ".")
	norm = strings.TrimSpace(norm)
	if norm == "" {
		return ""
	}
	switch {
	case containsAny(norm,
		"web login", "login to dashboard", "open dashboard", "sign in", "weblogin",
	):
		return "weblogin"
	case containsAny(norm,
		"show my positions", "show positions", "my positions", "open positions",
		"what are my positions", "what's my positions", "list my positions",
		"current positions", "pozicionet e mia", "pozicionet", "pozicione",
	):
		return "positions"
	case containsAny(norm,
		"show my balance", "my balance", "what's my balance", "what is my balance",
		"account balance", "show balance", "balanca ime", "balanca", "balanc",
	):
		return "balance"
	case containsAny(norm,
		"list my traders", "show my traders", "my traders", "list traders",
		"tregtarët e mi", "tregtar",
	):
		return "traders"
	case containsAny(norm,
		"notifications", "notify", "njoftime", "njoftimet",
	):
		return "notify"
	}
	return ""
}

func containsAny(s string, phrases ...string) bool {
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func handleNotifyCommand(bot *tgbotapi.BotAPI, chatID int64, text string, st *store.Store, lang string, botUserID string, apiPort int) {
	fields := strings.Fields(strings.TrimSpace(text))
	action := ""
	if len(fields) >= 2 {
		action = strings.ToLower(fields[1])
	}

	switch action {
	case "on", "yes", "1", "po":
		_ = st.TelegramConfig().SetNotifyEnabled(true)
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "off", "no", "0", "jo":
		_ = st.TelegramConfig().SetNotifyEnabled(false)
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "daily", "digest":
		sub := "on"
		if len(fields) >= 3 {
			sub = strings.ToLower(fields[2])
		}
		if sub == "off" {
			_ = st.TelegramConfig().SetDigestEnabled(false)
		} else {
			_ = st.TelegramConfig().SetDigestEnabled(true)
		}
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "swing":
		if len(fields) >= 3 {
			if v, err := strconv.ParseFloat(fields[2], 64); err == nil {
				_ = st.TelegramConfig().SetPnlSwingThreshold(v)
			}
		}
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	case "test", "prove", "provo":
		jwt, err := agent.GenerateBotToken(botUserID)
		if err != nil {
			sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
			return
		}
		sendNotifyTest(bot, chatID, st, lang, newQuickClient(apiPort, jwt))
	default:
		sendHTMLMsg(bot, chatID, notifyStatusMsg(st, lang))
	}
}

func sendNotifyTest(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, lang string, client *quickClient) {
	if lang == "zh" {
		sendHTMLMsg(bot, chatID, "🔔 <b>测试通知</b>\n\n如果你看到这条消息，Telegram 推送正常。")
	} else {
		sendHTMLMsg(bot, chatID, "🔔 <b>Test notification</b>\n\nIf you see this, Telegram push is working.")
	}

	portfolios, err := fetchAllTraderPortfolios(client)
	if err != nil {
		sendMsg(bot, chatID, "Could not load portfolios: "+err.Error())
		return
	}

	title := "📊 Daily snapshot preview (all strategies)"
	if lang == "zh" {
		title = "📊 每日快照预览（全部策略）"
	}
	body := formatMultiTraderSnapshot(portfolios, lang, true)
	sendHTMLMsg(bot, chatID, "<b>"+escapeHTML(title)+"</b>\n\n"+body)

	sample := formatTradeAlertRich(st, events.TradeEvent{
		TraderID: "demo",
		Symbol:   "BTCUSDT",
		Side:     "LONG",
		Action:   "open_long",
		Quantity: 0.01,
		Price:    95000,
		Leverage: 10,
		OrderID:  "test-sample",
	}, lang, nil)
	label := "Example of a real trade alert:"
	if lang == "zh" {
		label = "真实成交通知示例:"
	}
	sendHTMLMsg(bot, chatID, "<b>"+escapeHTML(label)+"</b>\n\n"+sample)
}

func notifyStatusMsg(st *store.Store, lang string) string {
	enabled := st.TelegramConfig().IsNotifyEnabled()
	digestOn := st.TelegramConfig().IsDigestEnabled()
	threshold := st.TelegramConfig().GetPnlSwingThreshold()

	if lang == "zh" {
		if enabled {
			return fmt.Sprintf(
				"🔔 <b>通知已开启</b>\n\n"+
					"每日快照: %s · 波动阈值: %s\n\n"+
					"/notify test — 测试\n/notify off — 关闭全部\n/notify daily off — 仅关闭每日快照\n/notify swing 5 — 设置波动阈值",
				onOff(digestOn), formatMoney(threshold))
		}
		return "🔕 <b>通知已关闭</b>\n\n/notify on — 开启"
	}
	if enabled {
		return fmt.Sprintf(
			"🔔 <b>Notifications ON</b>\n\n"+
				"Daily digest: %s · Swing threshold: %s\n\n"+
				"/notify test — test push\n/notify off — turn off all alerts\n/notify daily off — daily snapshot only (swing stays on)\n/notify swing 5 — set swing threshold",
			onOff(digestOn), formatMoney(threshold))
	}
	return "🔕 <b>Notifications OFF</b>\n\n/notify on — turn on auto alerts"
}

func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

func formatTradersList(st *store.Store, traders []TraderInfo, lang string) string {
	if len(traders) == 0 {
		if lang == "zh" {
			return "🤖 <b>交易员</b>\n\n暂无配置。"
		}
		return "🤖 <b>Traders</b>\n\nNo traders configured."
	}
	sel := st.TelegramConfig().GetSelectedTraderID()
	var sb strings.Builder
	if lang == "zh" {
		sb.WriteString("🤖 <b>选择交易员</b>\n\n")
	} else {
		sb.WriteString("🤖 <b>Choose trader</b>\n\n")
	}
	if sel == store.SelectedTraderAll {
		sb.WriteString("✅ <b>Both</b> — commands show every strategy\n\n")
	} else {
		sb.WriteString("Tap a button or /use 1 · /use 2 · /use all\n\n")
	}
	for i, t := range traders {
		marker := "  "
		if sel == t.TraderID {
			marker = "✅ "
		}
		status := "stopped"
		if t.IsRunning {
			status = "running"
		}
		line := fmt.Sprintf("%s%d. <b>%s</b> — %s", marker, i+1, escapeHTML(t.TraderName), status)
		if t.StrategyName != "" {
			line += fmt.Sprintf("\n   Strategy: <i>%s</i>", escapeHTML(t.StrategyName))
		}
		sb.WriteString(line)
		if i < len(traders)-1 {
			sb.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func sendTradersPicker(bot *tgbotapi.BotAPI, chatID int64, st *store.Store, lang string, client *quickClient) {
	traders, err := fetchMyTraders(client)
	if err != nil {
		sendMsg(bot, chatID, "Could not fetch traders: "+err.Error())
		return
	}
	text := formatTradersList(st, traders, lang)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, t := range traders {
		label := t.TraderName
		if t.StrategyName != "" {
			label = t.TraderName + " · " + t.StrategyName
		}
		if len(label) > 40 {
			label = label[:37] + "..."
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, "use:"+t.TraderID),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("Both (all strategies)", "use:"+store.SelectedTraderAll),
	))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(rows...)
	if _, err := bot.Send(msg); err != nil {
		plain := tgbotapi.NewMessage(chatID, stripHTML(text))
		bot.Send(plain) //nolint:errcheck
	}
}

func handleUseCommand(bot *tgbotapi.BotAPI, chatID int64, cmd string, st *store.Store, lang string, botUserID string, apiPort int) {
	fields := strings.Fields(strings.TrimSpace(cmd))
	if len(fields) < 2 {
		jwt, err := agent.GenerateBotToken(botUserID)
		if err != nil {
			sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
			return
		}
		sendTradersPicker(bot, chatID, st, lang, newQuickClient(apiPort, jwt))
		return
	}
	arg := strings.ToLower(fields[1])
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		sendMsg(bot, chatID, "Could not authenticate. Try /start again.")
		return
	}
	traders, err := fetchMyTraders(newQuickClient(apiPort, jwt))
	if err != nil {
		sendMsg(bot, chatID, "Could not fetch traders: "+err.Error())
		return
	}
	if arg == "all" || arg == "both" {
		_ = st.TelegramConfig().SetSelectedTraderID(store.SelectedTraderAll)
		sendHTMLMsg(bot, chatID, "✅ <b>Both</b> selected — /balanca and /positions show every strategy.")
		return
	}
	idx, err := strconv.Atoi(arg)
	if err != nil || idx < 1 || idx > len(traders) {
		sendMsg(bot, chatID, fmt.Sprintf("Use /use 1 … /use %d or /use all", len(traders)))
		return
	}
	t := traders[idx-1]
	_ = st.TelegramConfig().SetSelectedTraderID(t.TraderID)
	sendHTMLMsg(bot, chatID, fmt.Sprintf("✅ Now showing <b>%s</b>", escapeHTML(t.TraderName))+
		strategySuffix(t.StrategyName))
}

func handleTraderUseCallback(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, st *store.Store, lang string, botUserID string, apiPort int) {
	data := strings.TrimPrefix(query.Data, "use:")
	if data == "" {
		return
	}
	_ = st.TelegramConfig().SetSelectedTraderID(data)
	answer := "Both selected"
	if data != store.SelectedTraderAll {
		answer = "Trader selected"
	}
	callback := tgbotapi.NewCallback(query.ID, answer)
	bot.Request(callback) //nolint:errcheck
	if query.Message == nil {
		return
	}
	chatID := query.Message.Chat.ID
	if data == store.SelectedTraderAll {
		sendHTMLMsg(bot, chatID, "✅ <b>Both</b> selected — /balanca and /positions show every strategy.")
		return
	}
	jwt, err := agent.GenerateBotToken(botUserID)
	if err != nil {
		return
	}
	traders, err := fetchMyTraders(newQuickClient(apiPort, jwt))
	if err != nil {
		return
	}
	for _, t := range traders {
		if t.TraderID == data {
			sendHTMLMsg(bot, chatID, fmt.Sprintf("✅ Now showing <b>%s</b>", escapeHTML(t.TraderName))+
				strategySuffix(t.StrategyName))
			return
		}
	}
}

func strategySuffix(name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf("\nStrategy: <i>%s</i>", escapeHTML(name))
}
